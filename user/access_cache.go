package user

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"heckel.io/ntfy/v2/db"
	"heckel.io/ntfy/v2/log"
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
	seq     uint64       // Bumped on every apply; lets a slow full reload notice that a mutation raced its table scan
	mu      sync.RWMutex // Protect exact, pattern, and seq
}

// maxFullReloadRetries bounds how many extra times a full reload re-scans the
// table when a per-user mutation keeps landing mid-scan before it gives up for
// this cycle (the local mutation already kept the cache correct; only external
// writes are deferred to the next cycle).
const maxFullReloadRetries = 2

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
func (c *accessCache) Lookup(username, topic string) (read, write, found bool) {
	escapedTopic := escapeUnderscore(topic)
	c.mu.RLock()
	if username != Everyone {
		if entry, found := c.lookupNoLock(username, topic, escapedTopic); found {
			c.mu.RUnlock()
			maybeLogACLDecision(username, username, topic, entry.read, entry.write)
			return entry.read, entry.write, true
		}
	}
	if entry, found := c.lookupNoLock(Everyone, topic, escapedTopic); found {
		c.mu.RUnlock()
		maybeLogACLDecision(username, Everyone, topic, entry.read, entry.write)
		return entry.read, entry.write, true
	}
	c.mu.RUnlock()
	maybeLogACLDecision(username, "", topic, false, false)
	return false, false, false
}

// Reload scans (user_name, topic, read, write) rows and merges them into the
// cache. With no usernames the cache is replaced wholesale; otherwise the
// query is invoked with those usernames as positional args and only the
// listed users' slices are touched (a username absent from the result drops
// them from both maps). Runs against the primary so a reload after a
// mutation sees the just-written rows.
func (c *accessCache) Reload(d *db.DB, query string, usernames ...string) error {
	started := time.Now()
	scope := "full"
	if len(usernames) > 0 {
		scope = "users=" + strings.Join(usernames, ",")
	}
	args := make([]any, len(usernames))
	for i, u := range usernames {
		args[i] = u
	}
	// Query the database for all ACL entries
	rows, err := d.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	exacts := make(map[string]map[string]aclEntry)
	patterns := make(map[string][]aclEntry)
	updatedEntries := 0
	for rows.Next() {
		var username, escapedTopic string
		var read, write bool
		if err := rows.Scan(&username, &escapedTopic, &read, &write); err != nil {
			return err
		}
		entry, hasWildcard, err := toACLEntry(escapedTopic, read, write)
		if err != nil {
			return err
		}
		if hasWildcard {
			patterns[username] = append(patterns[username], entry)
		} else {
			if exacts[username] == nil {
				exacts[username] = make(map[string]aclEntry)
			}
			exacts[username][escapedTopic] = entry
		}
		updatedEntries++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Replace or update the internal maps
	c.mu.Lock()
	if len(usernames) == 0 {
		c.exact = exacts
		c.pattern = patterns
	} else {
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
	}
	c.mu.Unlock()
	log.Tag(tag).
		Field("reload_scope", scope).
		Field("updated_entries", updatedEntries).
		Field("duration_ms", time.Since(started).Milliseconds()).
		Debug("Reloaded ACL cache")
	return nil
}

// lookupNoLock returns the highest-priority entry for a single user. When
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
func (c *accessCache) lookupNoLock(username, topic, escapedTopic string) (*aclEntry, bool) {
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
func toACLEntry(escapedTopic string, read, write bool) (entry aclEntry, hasWildcard bool, err error) {
	entry = aclEntry{
		length: len(escapedTopic),
		read:   read,
		write:  write,
	}
	if !strings.Contains(escapedTopic, "%") {
		return entry, false, nil
	}
	pattern, err := compileLikeToRegex(escapedTopic)
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

// maybeLogACLDecision logs an ACL lookup result
func maybeLogACLDecision(requestUser, matchedUser, topic string, read, write bool) {
	ev := log.Tag(tag).
		Field("user_name", requestUser).
		Field("topic", topic).
		Field("read", read).
		Field("write", write)
	if !ev.IsTrace() {
		return
	}
	if matchedUser == "" {
		ev.Trace("ACL no match")
		return
	}
	ev.Field("matched_user", matchedUser).Trace("ACL match")
}
