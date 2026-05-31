package user

import (
	"regexp"
	"strings"
	"sync"

	"heckel.io/ntfy/v2/db"
)

// aclCache is an in-memory index over the entire user_access table.
//
// exact[username][escapedTopic] returns the matching entry in O(1) for the common
// case where the requested topic appears verbatim in some rule. The key is the
// stored form of the topic (i.e. with \_ escapes), so Lookup escapes incoming
// topics through escapeUnderscore before probing.
//
// wildcard[username] is the linear-scan list of %-bearing rules for that user.
// Walked per request; trivially small in practice. Wildcards are NOT u_everyone-
// only -- any user can create them.
type aclCache struct {
	exact    map[string]map[string]aclEntry
	wildcard map[string][]aclEntry
	mu       sync.RWMutex // Protect exact and wildcard
}

// aclEntry mirrors one user_access row in the in-memory cache.
//
// topic is the raw stored value: it may contain \_ escapes (for literal underscores)
// and % wildcards (translated from user-supplied *). For exact-match entries (no %)
// matcher is nil and the entry is keyed by topic in aclCache.exact. For wildcard
// entries (with %) matcher is the pre-compiled regex equivalent of the LIKE pattern.
type aclEntry struct {
	topic   string
	read    bool
	write   bool
	matcher *regexp.Regexp //  Nil for exact entries
}

func newAccessCache() *aclCache {
	return &aclCache{
		exact:    make(map[string]map[string]aclEntry),
		wildcard: make(map[string][]aclEntry),
	}
}

// reload runs the bulk-load query against the primary and swaps in freshly-built
// exact and wildcard maps under the write lock. The primary is used (not
// ReadOnly) so a reload immediately after an ACL mutation sees the freshly-
// written rows without replica lag.
func (c *aclCache) reload(d *db.DB, query string) error {
	rows, err := d.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	exact := make(map[string]map[string]aclEntry)
	wildcards := make(map[string][]aclEntry)
	for rows.Next() {
		var username string
		var entry aclEntry
		if err := rows.Scan(&username, &entry.topic, &entry.read, &entry.write); err != nil {
			return err
		}
		if strings.Contains(entry.topic, "%") {
			re, err := compileLikeToRegex(entry.topic)
			if err != nil {
				return err
			}
			entry.matcher = re
			wildcards[username] = append(wildcards[username], entry)
		} else {
			if exact[username] == nil {
				exact[username] = make(map[string]aclEntry)
			}
			exact[username][entry.topic] = entry
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.exact = exact
	c.wildcard = wildcards
	c.mu.Unlock()
	return nil
}

// Lookup returns the effective (read, write, found) permission for the given
// (username, topic), preserving the priority ordering of the original SQL query:
//  1. specific user beats Everyone
//  2. longer pattern beats shorter (more specific wins)
//  3. write beats read at equal length (write is "stronger")
func (c *aclCache) Lookup(usernameOrEveryone, topic string) (read, write, found bool) {
	if c == nil {
		return false, false, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	// Pre-compute the escaped form once: exact-match keys in the cache are
	// stored as toSQLWildcard would emit them (literal _ -> \_), so the
	// incoming topic must be escaped the same way before map lookup.
	escaped := escapeUnderscore(topic)

	// Specific user takes priority over Everyone. Skip the first lookup when
	// the request is already anonymous to avoid scanning the same map twice.
	if usernameOrEveryone != Everyone {
		if e, ok := c.pickBestLocked(usernameOrEveryone, topic, escaped); ok {
			return e.read, e.write, true
		}
	}
	if e, ok := c.pickBestLocked(Everyone, topic, escaped); ok {
		return e.read, e.write, true
	}
	return false, false, false
}

// pickBestLocked returns the highest-priority entry for a single user, combining
// the exact-match O(1) probe with a linear scan over the (usually empty or tiny)
// wildcard list. Caller must hold c.mu (RLock is sufficient).
func (c *aclCache) pickBestLocked(username, topic, escaped string) (aclEntry, bool) {
	var best aclEntry
	var found bool
	if m, ok := c.exact[username]; ok {
		if e, ok := m[escaped]; ok {
			best, found = e, true
		}
	}
	for _, w := range c.wildcard[username] {
		if !w.matcher.MatchString(topic) {
			continue
		}
		if !found || better(w, best) {
			best, found = w, true
		}
	}
	return best, found
}

// better implements the (length DESC, write DESC) tie-break used by the original
// query's ORDER BY for entries owned by the same user.
func better(a, b aclEntry) bool {
	if len(a.topic) != len(b.topic) {
		return len(a.topic) > len(b.topic)
	}
	if a.write != b.write {
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
