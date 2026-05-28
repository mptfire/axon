package user

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Cache-only unit tests. Integration with the Manager (loading from the DB,
// reload-after-mutation, end-to-end Authorize behavior) is covered by the
// existing TestStoreAuthorizeTopicAccess* tests in manager_test.go via
// forEachStoreBackend.

func TestCompileLikeToRegex_Exact(t *testing.T) {
	r := compileLikeToRegex("foo")
	require.True(t, r.MatchString("foo"))
	require.False(t, r.MatchString("foox"))
	require.False(t, r.MatchString("xfoo"))
}

func TestCompileLikeToRegex_TrailingPercent(t *testing.T) {
	r := compileLikeToRegex("up%")
	require.True(t, r.MatchString("up"))
	require.True(t, r.MatchString("up123"))
	require.False(t, r.MatchString("xup"))
}

func TestCompileLikeToRegex_LeadingAndEmbeddedPercent(t *testing.T) {
	r := compileLikeToRegex("%test%")
	require.True(t, r.MatchString("test"))
	require.True(t, r.MatchString("mytest"))
	require.True(t, r.MatchString("testxxx"))
	require.True(t, r.MatchString("xtestx"))
	require.False(t, r.MatchString("nope"))
}

func TestCompileLikeToRegex_EscapedUnderscore(t *testing.T) {
	// "my\_topic" is the stored form of a literal "my_topic" -- the underscore
	// must match itself, NOT act as a SQL one-character wildcard.
	r := compileLikeToRegex(`my\_topic`)
	require.True(t, r.MatchString("my_topic"))
	require.False(t, r.MatchString("myXtopic"))
	require.False(t, r.MatchString("mytopic"))
}

func TestCompileLikeToRegex_EscapedUnderscoreAdjacentToPercent(t *testing.T) {
	// "nz\_vip\_%" is the stored form of "nz_vip_*" -- literal "nz_vip_" prefix
	// followed by any suffix.
	r := compileLikeToRegex(`nz\_vip\_%`)
	require.True(t, r.MatchString("nz_vip_"))
	require.True(t, r.MatchString("nz_vip_alpha"))
	require.False(t, r.MatchString("nz_vipX"))
	require.False(t, r.MatchString("nzvip_alpha"))
}

func TestCompileLikeToRegex_RegexMetaCharsInTopic(t *testing.T) {
	// Topics in ntfy can include '-', which is benign, but make sure
	// regex metacharacters in the pattern are escaped properly anyway.
	r := compileLikeToRegex("foo-bar")
	require.True(t, r.MatchString("foo-bar"))
	require.False(t, r.MatchString("foo.bar")) // would match if '-' leaked into a character class
}

func TestACLCache_LookupOnNilReceiverSafe(t *testing.T) {
	var c *aclCache
	read, write, found := c.Lookup("phil", "mytopic")
	require.False(t, found)
	require.False(t, read)
	require.False(t, write)
}

func TestACLCache_LookupBeforeReload(t *testing.T) {
	// Before reload the snapshot pointer is nil. The cache treats this as
	// "no rule found", which the caller resolves via DefaultAccess.
	c := newAccessCache()
	read, write, found := c.Lookup("phil", "mytopic")
	require.False(t, found)
	require.False(t, read)
	require.False(t, write)
}

func TestACLCache_ExactMatchHit(t *testing.T) {
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: "phil", topic: "mytopic", read: true, write: true},
	}))
	read, write, found := c.Lookup("phil", "mytopic")
	require.True(t, found)
	require.True(t, read)
	require.True(t, write)
}

func TestACLCache_ExactMatchMiss(t *testing.T) {
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: "phil", topic: "mytopic", read: true, write: true},
	}))
	_, _, found := c.Lookup("phil", "othertopic")
	require.False(t, found)
}

func TestACLCache_LiteralUnderscoreExactMatch(t *testing.T) {
	// Stored as "my\_topic" (toSQLWildcard of "my_topic"). A literal underscore
	// in the requested topic must match, while any other single char must not.
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: "phil", topic: `my\_topic`, read: true, write: false},
	}))
	read, write, found := c.Lookup("phil", "my_topic")
	require.True(t, found)
	require.True(t, read)
	require.False(t, write)

	_, _, found = c.Lookup("phil", "myXtopic")
	require.False(t, found)
}

func TestACLCache_WildcardMatch(t *testing.T) {
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: Everyone, topic: "up%", read: false, write: true},
	}))
	read, write, found := c.Lookup("phil", "up42")
	require.True(t, found)
	require.False(t, read)
	require.True(t, write)
}

func TestACLCache_SpecificUserBeatsEveryone(t *testing.T) {
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: Everyone, topic: "mytopic", read: true, write: false},
		{user: "phil", topic: "mytopic", read: false, write: false}, // deny-all for phil
	}))
	read, write, found := c.Lookup("phil", "mytopic")
	require.True(t, found)
	require.False(t, read)
	require.False(t, write)
}

func TestACLCache_AnonymousReadsEveryone(t *testing.T) {
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: Everyone, topic: "announcements", read: true, write: false},
	}))
	read, write, found := c.Lookup(Everyone, "announcements")
	require.True(t, found)
	require.True(t, read)
	require.False(t, write)
}

func TestACLCache_LongerPatternWinsForSameUser(t *testing.T) {
	// Both rules belong to the same user (Everyone). The more specific (longer)
	// "mytopic%" should beat the catch-all "%".
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: Everyone, topic: "%", read: true, write: false},
		{user: Everyone, topic: "mytopic%", read: true, write: true},
	}))
	read, write, found := c.Lookup(Everyone, "mytopicX")
	require.True(t, found)
	require.True(t, read)
	require.True(t, write)
}

func TestACLCache_WriteBeatsReadAtEqualLength(t *testing.T) {
	// Two wildcard rules of identical length for the same user. The write rule
	// should win the tie-break.
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: Everyone, topic: "ab%", read: true, write: false},
		{user: Everyone, topic: "ab%", read: false, write: true}, // synthesized; impossible via real upsert but exercises the tie-break
	}))
	// One of the two will be the surviving exact-key entry (map collision keeps last);
	// but the wildcard slice is what we want to exercise. Inject two wildcard entries
	// directly to force the tie-break path.
	c.snap.Store(&aclSnapshot{
		exact: map[string]map[string]aclEntry{},
		wildcards: map[string][]aclEntry{
			Everyone: {
				{topic: "ab%", read: true, write: false, matcher: compileLikeToRegex("ab%")},
				{topic: "ab%", read: false, write: true, matcher: compileLikeToRegex("ab%")},
			},
		},
	})
	_, write, found := c.Lookup(Everyone, "abc")
	require.True(t, found)
	require.True(t, write)
}

func TestACLCache_ConcurrentLookupAndReload(t *testing.T) {
	// Atomic-pointer swap must be safe under concurrent reads. The race detector
	// catches any unsafe shared mutation.
	c := newAccessCache()
	c.snap.Store(buildSnapshot(t, []rawACLRow{
		{user: Everyone, topic: "mytopic", read: true, write: true},
	}))

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			_, _, _ = c.Lookup(Everyone, "mytopic")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.snap.Store(buildSnapshot(t, []rawACLRow{
				{user: Everyone, topic: "mytopic", read: i%2 == 0, write: i%2 == 1},
			}))
		}
		stop.Store(true)
	}()
	wg.Wait()
}

// rawACLRow + buildSnapshot mirror the rows that reload would Scan from the DB
// but avoid actually opening a DB for these unit tests.
type rawACLRow struct {
	user  string
	topic string
	read  bool
	write bool
}

func buildSnapshot(t *testing.T, rows []rawACLRow) *aclSnapshot {
	t.Helper()
	snap := &aclSnapshot{
		exact:     make(map[string]map[string]aclEntry),
		wildcards: make(map[string][]aclEntry),
	}
	for _, r := range rows {
		e := aclEntry{topic: r.topic, read: r.read, write: r.write}
		if containsPercent(r.topic) {
			e.matcher = compileLikeToRegex(r.topic)
			snap.wildcards[r.user] = append(snap.wildcards[r.user], e)
		} else {
			if snap.exact[r.user] == nil {
				snap.exact[r.user] = make(map[string]aclEntry)
			}
			snap.exact[r.user][r.topic] = e
		}
	}
	return snap
}

func containsPercent(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			return true
		}
	}
	return false
}
