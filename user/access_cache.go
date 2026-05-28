package user

import (
	"regexp"
	"strings"
	"sync/atomic"

	"heckel.io/ntfy/v2/db"
)

// aclEntry mirrors one user_access row in the in-memory snapshot.
//
// topic is the raw stored value: it may contain \_ escapes (for literal underscores)
// and % wildcards (translated from user-supplied *). For exact-match entries (no %)
// matcher is nil and the entry is keyed by topic in aclSnapshot.exact. For wildcard
// entries (with %) matcher is the pre-compiled regex equivalent of the LIKE pattern.
type aclEntry struct {
	topic   string
	read    bool
	write   bool
	matcher *regexp.Regexp
}

// aclSnapshot is an immutable indexed form of the entire user_access table.
//
// exact[userName][escapedTopic] returns the matching entry in O(1) for the common
// case where the requested topic appears verbatim in some rule. The key is the
// stored form of the topic (i.e. with \_ escapes), so callers must pass topics
// through escapeUnderscore before probing.
//
// wildcards[userName] is the linear scan list of %-bearing rules for that user.
// Walked per request; trivially small in practice. Wildcards are NOT u_everyone-
// only -- any user can create them.
type aclSnapshot struct {
	exact     map[string]map[string]aclEntry
	wildcards map[string][]aclEntry
}

// aclCache holds the current snapshot behind an atomic pointer so that the hot
// path (Lookup) is lock-free. reload builds a fresh snapshot off the request
// path and atomically swaps the pointer; the old snapshot is GC'd once in-flight
// Lookups release their references.
//
// A nil receiver behaves as if no snapshot were loaded -- Lookup returns
// found=false, which the caller then resolves via DefaultAccess. This keeps
// tests and edge cases (e.g. early-startup) safe.
type aclCache struct {
	snap atomic.Pointer[aclSnapshot]
}

func newAccessCache() *aclCache {
	return &aclCache{}
}

// reload runs the bulk-load query against the primary and atomically swaps in
// a fresh snapshot. The primary is used (not ReadOnly) so a reload immediately
// after an ACL mutation sees the freshly-written rows without replica lag.
func (c *aclCache) reload(d *db.DB, query string) error {
	rows, err := d.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	snap := &aclSnapshot{
		exact:     make(map[string]map[string]aclEntry),
		wildcards: make(map[string][]aclEntry),
	}
	for rows.Next() {
		var userName string
		var entry aclEntry
		if err := rows.Scan(&userName, &entry.topic, &entry.read, &entry.write); err != nil {
			return err
		}
		if strings.Contains(entry.topic, "%") {
			entry.matcher = compileLikeToRegex(entry.topic)
			snap.wildcards[userName] = append(snap.wildcards[userName], entry)
		} else {
			if snap.exact[userName] == nil {
				snap.exact[userName] = make(map[string]aclEntry)
			}
			snap.exact[userName][entry.topic] = entry
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	c.snap.Store(snap)
	return nil
}

// Lookup returns the effective (read, write, found) permission for the given
// (username, topic), preserving the priority ordering of the original SQL query:
//   1. specific user beats Everyone
//   2. longer pattern beats shorter (more specific wins)
//   3. write beats read at equal length (write is "stronger")
func (c *aclCache) Lookup(usernameOrEveryone, topic string) (read, write, found bool) {
	if c == nil {
		return false, false, false
	}
	snap := c.snap.Load()
	if snap == nil {
		return false, false, false
	}
	// Pre-compute the escaped form once: exact-match keys in the snapshot are
	// stored as toSQLWildcard would emit them (literal _ -> \_), so the
	// incoming topic must be escaped the same way before map lookup.
	escaped := escapeUnderscore(topic)

	// Specific user takes priority over Everyone. Skip the first lookup when
	// the request is already anonymous to avoid scanning the same map twice.
	if usernameOrEveryone != Everyone {
		if e, ok := pickBest(snap, usernameOrEveryone, topic, escaped); ok {
			return e.read, e.write, true
		}
	}
	if e, ok := pickBest(snap, Everyone, topic, escaped); ok {
		return e.read, e.write, true
	}
	return false, false, false
}

// pickBest returns the highest-priority entry for a single user, combining the
// exact-match O(1) probe with a linear scan over the (usually empty or tiny)
// wildcard list. Priority within a user: longer pattern wins; write wins ties.
func pickBest(snap *aclSnapshot, userName, topic, escaped string) (aclEntry, bool) {
	var best aclEntry
	var found bool
	if m, ok := snap.exact[userName]; ok {
		if e, ok := m[escaped]; ok {
			best, found = e, true
		}
	}
	for _, w := range snap.wildcards[userName] {
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
func compileLikeToRegex(pattern string) *regexp.Regexp {
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
	return regexp.MustCompile(sb.String())
}
