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

// aclEntry mirrors one user_access row. length feeds better()'s "longer
// pattern wins" tie-break; the stored topic/pattern string itself is not kept
// on the entry (the exact map already keys on it; surfacing wildcard "topics"
// like "up%" alongside real ones would invite misuse). pattern is the
// compiled regex form of the LIKE pattern; nil for exact entries.
type aclEntry struct {
	length  int
	pattern *regexp.Regexp
	read    bool
	write   bool
}

func newAccessCache() *accessCache {
	return &accessCache{
		exact:   make(map[string]map[string]aclEntry),
		pattern: make(map[string][]aclEntry),
	}
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

// reload scans (user_name, topic, read, write) rows and merges them into the
// cache. With no usernames the cache is replaced wholesale; otherwise the
// query is invoked with those usernames as positional args and only the
// listed users' slices are touched (a username absent from the result drops
// them from both maps). Runs against the primary so a reload after a
// mutation sees the just-written rows.
func (c *accessCache) reload(d *db.DB, query string, usernames ...string) error {
	args := make([]any, len(usernames))
	for i, u := range usernames {
		args[i] = u
	}
	rows, err := d.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	exacts := make(map[string]map[string]aclEntry)
	patterns := make(map[string][]aclEntry)
	for rows.Next() {
		var u, topic string
		var read, write bool
		if err := rows.Scan(&u, &topic, &read, &write); err != nil {
			return err
		}
		entry, isPattern, err := toACLEntry(topic, read, write)
		if err != nil {
			return err
		}
		if isPattern {
			patterns[u] = append(patterns[u], entry)
		} else {
			if exacts[u] == nil {
				exacts[u] = make(map[string]aclEntry)
			}
			exacts[u][topic] = entry
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(usernames) == 0 {
		c.exact = exacts
		c.pattern = patterns
		return nil
	}
	for _, u := range usernames {
		if e, ok := exacts[u]; ok {
			c.exact[u] = e
		} else {
			delete(c.exact, u)
		}
		if p, ok := patterns[u]; ok {
			c.pattern[u] = p
		} else {
			delete(c.pattern, u)
		}
	}
	return nil
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

// toACLEntry builds an aclEntry from one user_access row's values. The
// isWildcard return tells the caller which storage slot the entry belongs in:
// the per-user wildcard slice if true, the per-user exact map if false.
// Wildcards have their LIKE pattern pre-compiled into entry.pattern; exact
// entries leave entry.pattern nil.
func toACLEntry(topic string, read, write bool) (entry aclEntry, isWildcard bool, err error) {
	entry = aclEntry{length: len(topic), read: read, write: write}
	if !strings.Contains(topic, "%") {
		return entry, false, nil
	}
	pattern, err := compileLikeToRegex(topic)
	if err != nil {
		return entry, true, err
	}
	entry.pattern = pattern
	return entry, true, nil
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
