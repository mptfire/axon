package user

import (
	"regexp"
	"strings"
	"sync"

	"heckel.io/ntfy/v2/db"
)

// accessCache is an in-memory index over the entire user_access table.
//
// exact[username][escapedTopic] returns the matching entry in O(1) for the common
// case where the requested topic appears verbatim in some rule. The key is the
// stored form of the topic (i.e. with \_ escapes), so Lookup escapes incoming
// topics through escapeUnderscore before probing.
//
// pattern[username] is the linear-scan list of %-bearing rules for that user.
// Walked per request; trivially small in practice. Wildcards are NOT u_everyone-
// only -- any user can create them.
type accessCache struct {
	exact   map[string]map[string]aclEntry
	pattern map[string][]aclEntry
	mu      sync.RWMutex // Protect exact and pattern
}

// aclEntry mirrors one user_access row in the in-memory cache.
//
// length is the length of the original stored value (topic for exact rows,
// SQL LIKE pattern for wildcard rows). It is only used by better() to
// implement the "longer pattern beats shorter" tie-break from the original
// SQL ORDER BY. The string itself is intentionally not stored on the entry:
// the exact map already keys on it, and surfacing it would invite misuse
// (wildcard "topics" are actually SQL patterns like "up%").
//
// pattern is the pre-compiled regex equivalent of the stored LIKE pattern.
// For exact-match entries (no % in the stored value) pattern is nil and the
// entry is reachable only through accessCache.exact[username][topic].
type aclEntry struct {
	length  int            // len() of the original stored topic/pattern
	pattern *regexp.Regexp // nil for exact entries
	read    bool
	write   bool
}

func newAccessCache() *accessCache {
	return &accessCache{
		exact:   make(map[string]map[string]aclEntry),
		pattern: make(map[string][]aclEntry),
	}
}

// reload runs the bulk-load query against the primary and swaps in freshly-built
// exact and pattern maps under the write lock. The primary is used (not
// ReadOnly) so a reload immediately after an ACL mutation sees the freshly-
// written rows without replica lag.
func (c *accessCache) reload(d *db.DB, query string) error {
	rows, err := d.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	exacts := make(map[string]map[string]aclEntry)
	patterns := make(map[string][]aclEntry)
	for rows.Next() {
		var username, topic string
		var read, write bool
		if err := rows.Scan(&username, &topic, &read, &write); err != nil {
			return err
		}
		entry, isWildcard, err := newACLEntry(topic, read, write)
		if err != nil {
			return err
		}
		if isWildcard {
			patterns[username] = append(patterns[username], entry)
		} else {
			if exacts[username] == nil {
				exacts[username] = make(map[string]aclEntry)
			}
			exacts[username][topic] = entry
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.exact = exacts
	c.pattern = patterns
	c.mu.Unlock()
	return nil
}

// reloadUser refreshes just one user's rules from the database and swaps them
// into the cache. Used as a cheaper, owner-cascade-safe alternative to the
// full bulk reload after a mutation that affects a known set of users. The
// caller is expected to invoke reloadUser for every user whose row set may
// have changed -- typically the targeted user plus Everyone, since most
// reservation flows touch both.
//
// The query must return (topic, read, write) for every row whose user_id
// matches the given username. An empty result set is treated as "this user
// has no rules": the user's entries are removed from both maps so the inner
// maps don't grow unbounded under churn.
func (c *accessCache) reloadUser(d *db.DB, query, username string) error {
	rows, err := d.Query(query, username)
	if err != nil {
		return err
	}
	defer rows.Close()
	exact := make(map[string]aclEntry)
	var pattern []aclEntry
	for rows.Next() {
		var topic string
		var read, write bool
		if err := rows.Scan(&topic, &read, &write); err != nil {
			return err
		}
		entry, isWildcard, err := newACLEntry(topic, read, write)
		if err != nil {
			return err
		}
		if isWildcard {
			pattern = append(pattern, entry)
		} else {
			exact[topic] = entry
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if len(exact) == 0 {
		delete(c.exact, username)
	} else {
		c.exact[username] = exact
	}
	if len(pattern) == 0 {
		delete(c.pattern, username)
	} else {
		c.pattern[username] = pattern
	}
	c.mu.Unlock()
	return nil
}

// newACLEntry builds an aclEntry from one user_access row's values. The
// isWildcard return tells the caller which storage slot the entry belongs in:
// the per-user wildcard slice if true, the per-user exact map if false.
// Wildcards have their LIKE pattern pre-compiled into entry.pattern; exact
// entries leave entry.pattern nil.
func newACLEntry(topic string, read, write bool) (entry aclEntry, isWildcard bool, err error) {
	entry = aclEntry{length: len(topic), read: read, write: write}
	if !strings.Contains(topic, "%") {
		return entry, false, nil
	}
	re, err := compileLikeToRegex(topic)
	if err != nil {
		return entry, true, err
	}
	entry.pattern = re
	return entry, true, nil
}

// Lookup returns the effective (read, write, found) permission for the given
// (username, topic), preserving the priority ordering of the original SQL query:
//  1. specific user beats Everyone
//  2. longer pattern beats shorter (more specific wins)
//  3. write beats read at equal length (write is "stronger")
func (c *accessCache) Lookup(usernameOrEveryone, topic string) (read, write, found bool) {
	escapedTopic := escapeUnderscore(topic)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if usernameOrEveryone != Everyone {
		if entry, ok := c.pickBestNoLock(usernameOrEveryone, topic, escapedTopic); ok {
			return entry.read, entry.write, true
		}
	}
	if entry, ok := c.pickBestNoLock(Everyone, topic, escapedTopic); ok {
		return entry.read, entry.write, true
	}
	return false, false, false
}

// pickBestNoLock returns the highest-priority entry for a single user. When
// more than one of that user's rules matches the requested topic, the winner
// is chosen by:
//
//  1. longer stored pattern beats shorter (a more specific rule wins over a
//     more general one)
//  2. at equal length, write beats read (a stronger permission wins the tie)
//
// Exact and wildcard rules are ranked together under the same criteria, so
// an exact "foo" (length 3) beats a wildcard "f%" (length 2), but a wildcard
// "foo%" (length 4) beats an exact "foo" (length 3).
func (c *accessCache) pickBestNoLock(username, topic, escapedTopic string) (*aclEntry, bool) {
	var best aclEntry
	var found bool
	if exact, exists := c.exact[username]; exists {
		if entry, exists := exact[escapedTopic]; exists {
			best, found = entry, true
		}
	}
	for _, pattern := range c.pattern[username] {
		if !pattern.pattern.MatchString(topic) {
			continue
		} else if !found || better(pattern, best) {
			best, found = pattern, true
		}
	}
	return &best, found
}

// better implements the (length DESC, write DESC) tie-break used by the original
// query's ORDER BY for entries owned by the same user.
func better(a, b aclEntry) bool {
	if a.length != b.length {
		return a.length > b.length
	} else if a.write != b.write {
		return a.write
	}
	return false
}

// compileLikeToRegex converts a stored ntfy LIKE pattern into an equivalent Go
// regexp. In ntfy's stored form, % is the only wildcard (translated from *) and
// \_ is a literal underscore; no other backslashes occur. Topics themselves are
// restricted to [A-Za-z0-9_-] (see AllowedTopic), so neither % nor stray
// backslashes appear in user-supplied input.
func compileLikeToRegex(pattern string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	i := 0
	for i < len(pattern) {
		switch {
		case pattern[i] == '\\' && i+1 < len(pattern) && pattern[i+1] == '_':
			sb.WriteString(regexp.QuoteMeta("_"))
			i += 2
		case pattern[i] == '%':
			sb.WriteString(".*")
			i++
		default:
			sb.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}
