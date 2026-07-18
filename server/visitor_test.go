package server

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/user"
)

// newBanTestVisitor creates a visitor wired for ban-feed testing, with the given ban file,
// weighted-bucket threshold, and weights, plus a 1-minute window (so the emit throttle only
// fires once per test).
func newBanTestVisitor(t *testing.T, banFile string, threshold int, weights map[string]int) *visitor {
	conf := NewConfig()
	conf.BanFile = banFile
	conf.BanWindow = time.Minute
	conf.BanThreshold = threshold
	conf.BanWeights = weights
	return newVisitor(conf, nil, nil, netip.MustParseAddr("1.2.3.4"), nil)
}

func readBanLines(t *testing.T, path string) []string {
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestVisitor_RecordStatus_Weight2BansAtHalfThreshold(t *testing.T) {
	banFile := filepath.Join(t.TempDir(), "ban.log")
	// Threshold 10, code weight 2 -> the budget covers exactly 5 hits, so the 6th breaches.
	v := newBanTestVisitor(t, banFile, 10, map[string]int{"*": 2})
	for i := 0; i < 5; i++ {
		v.recordStatus(v.ip, 400, 40001)
	}
	require.NoFileExists(t, banFile) // 5 hits * weight 2 = 10 == budget, exactly at the limit, not over
	v.recordStatus(v.ip, 400, 40001) // 6th hit cannot be covered -> breach
	lines := readBanLines(t, banFile)
	require.Len(t, lines, 1)
	require.True(t, strings.HasSuffix(lines[0], " 1.2.3.4 1.2.3.4/32 400 40001")) // <ip> <prefix> <http> <ntfy-code>
}

func TestVisitor_RecordStatus_Weight10BansFast(t *testing.T) {
	banFile := filepath.Join(t.TempDir(), "ban.log")
	// Threshold 10, code weight 10 -> a single hit drains the whole budget, so the 2nd breaches.
	v := newBanTestVisitor(t, banFile, 10, map[string]int{"42909": 10, "*": 1})
	v.recordStatus(v.ip, 429, 42909)
	require.NoFileExists(t, banFile)
	v.recordStatus(v.ip, 429, 42909) // 2nd hit cannot be covered -> breach
	lines := readBanLines(t, banFile)
	require.Len(t, lines, 1)
	require.True(t, strings.HasSuffix(lines[0], " 1.2.3.4 1.2.3.4/32 429 42909"))
}

func TestVisitor_RecordStatus_Weight0NeverBans(t *testing.T) {
	banFile := filepath.Join(t.TempDir(), "ban.log")
	// The legit-quota code is exempt (weight 0), so no number of hits ever bans.
	v := newBanTestVisitor(t, banFile, 10, map[string]int{"42908": 0, "*": 1})
	for i := 0; i < 100; i++ {
		v.recordStatus(v.ip, 429, 42908)
	}
	require.NoFileExists(t, banFile)
}

func TestVisitor_RecordStatus_SingleBucketNoRelaxation(t *testing.T) {
	banFile := filepath.Join(t.TempDir(), "ban.log")
	// One shared bucket: different codes draw down the SAME budget, so mixing them creates no extra
	// headroom (unlike per-code buckets, which would relax the effective limit for a mixed offender).
	v := newBanTestVisitor(t, banFile, 10, map[string]int{"403*": 2, "*": 1})
	v.recordStatus(v.ip, 403, 40301) // weight 2 -> 8 left
	v.recordStatus(v.ip, 403, 40301) // weight 2 -> 6 left
	v.recordStatus(v.ip, 403, 40301) // weight 2 -> 4 left
	require.NoFileExists(t, banFile)
	for i := 0; i < 4; i++ {
		v.recordStatus(v.ip, 400, 40001) // weight 1 each -> drains the remaining 4 -> 0 left
	}
	require.NoFileExists(t, banFile) // 6 + 4 = 10 == budget exactly, still not over
	v.recordStatus(v.ip, 400, 40001) // one more cannot be covered -> breach
	lines := readBanLines(t, banFile)
	require.Len(t, lines, 1)
}

func TestVisitor_RecordStatus_ExactLineFormat(t *testing.T) {
	banFile := filepath.Join(t.TempDir(), "ban.log")
	v := newBanTestVisitor(t, banFile, 1, map[string]int{"*": 1})
	before := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		v.recordStatus(v.ip, 429, 42901)
	}
	after := time.Now().UTC()
	lines := readBanLines(t, banFile)
	require.Len(t, lines, 1)
	parts := strings.Split(lines[0], " ")
	require.Len(t, parts, 5) // "<timestamp> <ip> <prefix> <http-code> <ntfy-code>"
	ts, err := time.Parse(time.RFC3339, parts[0])
	require.NoError(t, err)
	require.Equal(t, time.UTC, ts.Location())
	require.False(t, ts.Before(before))
	require.False(t, ts.After(after.Add(time.Second)))
	require.Equal(t, "1.2.3.4", parts[1])    // full IP
	require.Equal(t, "1.2.3.4/32", parts[2]) // masked to the default IPv4 prefix (/32)
	require.Equal(t, "429", parts[3])        // HTTP status
	require.Equal(t, "42901", parts[4])      // ntfy code
}

func TestVisitor_RecordStatus_IPv6MaskedToPrefix(t *testing.T) {
	banFile := filepath.Join(t.TempDir(), "ban.log")
	conf := NewConfig()
	conf.BanFile = banFile
	conf.BanWindow = time.Minute
	conf.BanThreshold = 1
	conf.BanWeights = map[string]int{"*": 1}
	v := newVisitor(conf, nil, nil, netip.MustParseAddr("2001:db8::abcd"), nil)
	for i := 0; i < 3; i++ {
		v.recordStatus(v.ip, 429, 42901)
	}
	parts := strings.Split(readBanLines(t, banFile)[0], " ")
	require.Len(t, parts, 5)
	require.Equal(t, "2001:db8::abcd", parts[1]) // full IPv6 address
	require.Equal(t, "2001:db8::/64", parts[2])  // masked to the default IPv6 prefix (/64)
}

func TestVisitor_RecordStatus_TierUserAlsoBanned(t *testing.T) {
	// A tier'd user who keeps hammering the request limiter (42901) is banned by IP like anyone
	// else -- a persistent 429 stream is abusive regardless of the account behind it. The legit
	// case (hitting a paid plan/quota limit) is spared by the per-code weights, not by a tier skip:
	// codes like 42908 are weight 0. So tier does not exempt a visitor from the ban feed.
	banFile := filepath.Join(t.TempDir(), "ban.log")
	v := newBanTestVisitor(t, banFile, 1, map[string]int{"*": 1})
	v.user = &user.User{ID: "u_test", Name: "test", Tier: &user.Tier{ID: "ti_test"}}
	for i := 0; i < 10; i++ {
		v.recordStatus(v.ip, 429, 42901)
	}
	lines := readBanLines(t, banFile)
	require.Len(t, lines, 1)
	require.True(t, strings.HasSuffix(lines[0], " 1.2.3.4 1.2.3.4/32 429 42901"))
}

func TestVisitor_RecordStatus_BansOffendingIPNotVisitorIP(t *testing.T) {
	// For an account-keyed (tier'd) visitor, one visitor object serves many source IPs and its
	// stored v.ip is whichever IP created it first -- stale. The ban feed must write the IP of the
	// request that actually breached, passed in per-call, not the visitor's stored IP.
	banFile := filepath.Join(t.TempDir(), "ban.log")
	v := newBanTestVisitor(t, banFile, 1, map[string]int{"*": 1}) // v.ip == 1.2.3.4
	offender := netip.MustParseAddr("5.6.7.8")
	for i := 0; i < 3; i++ {
		v.recordStatus(offender, 429, 42901)
	}
	parts := strings.Split(readBanLines(t, banFile)[0], " ")
	require.Equal(t, "5.6.7.8", parts[1])    // the offending request IP, not v.ip (1.2.3.4)
	require.Equal(t, "5.6.7.8/32", parts[2]) // its prefix, not the visitor's stored-IP prefix
}

func TestVisitor_RecordStatus_DisabledWhenNoBanFile(t *testing.T) {
	v := newBanTestVisitor(t, "", 10, map[string]int{"*": 1})
	for i := 0; i < 5; i++ {
		v.recordStatus(v.ip, 403, 40301) // Feature disabled: must be a no-op, must not panic
	}
	require.Nil(t, v.banEmit) // No throttle limiter is created when the feature is off
}

func TestVisitor_RecordStatus_Ignores2xx3xx(t *testing.T) {
	banFile := filepath.Join(t.TempDir(), "ban.log")
	v := newBanTestVisitor(t, banFile, 3, map[string]int{"*": 1})
	// Success and redirects must never count toward a ban, even over the threshold -- otherwise a
	// legit high-volume publisher (lots of 200s) would get banned.
	for i := 0; i < 20; i++ {
		v.recordStatus(v.ip, 200, 20000)
		v.recordStatus(v.ip, 302, 30000)
	}
	require.NoFileExists(t, banFile)
	// A 4xx over the same budget still gets written.
	for i := 0; i < 5; i++ {
		v.recordStatus(v.ip, 400, 40001)
	}
	lines := readBanLines(t, banFile)
	require.Len(t, lines, 1)
	require.True(t, strings.HasSuffix(lines[0], " 1.2.3.4 1.2.3.4/32 400 40001"))
}

func TestParseBanWeights(t *testing.T) {
	// Exact codes, a bare 3-digit HTTP status (normalized to a family), an exempt code, and "*".
	weights, err := ParseBanWeights([]string{"42909:10", "403:2", "42908:0", "*:1"})
	require.NoError(t, err)
	require.Equal(t, map[string]int{"42909": 10, "403*": 2, "42908": 0, "*": 1}, weights)

	// A bare 3-digit HTTP status normalizes to its family.
	weights, err = ParseBanWeights([]string{"429:5"})
	require.NoError(t, err)
	require.Equal(t, map[string]int{"429*": 5}, weights)

	// An explicit family key stays as-is.
	weights, err = ParseBanWeights([]string{"429*:5"})
	require.NoError(t, err)
	require.Equal(t, map[string]int{"429*": 5}, weights)

	// Weight 0 is valid and means exempt.
	weights, err = ParseBanWeights([]string{"42908:0"})
	require.NoError(t, err)
	require.Equal(t, map[string]int{"42908": 0}, weights)

	_, err = ParseBanWeights([]string{"401"}) // Missing weight
	require.Error(t, err)
	_, err = ParseBanWeights([]string{"401:-1"}) // Negative weight
	require.Error(t, err)
	_, err = ParseBanWeights([]string{"401:abc"}) // Non-integer weight
	require.Error(t, err)
	_, err = ParseBanWeights([]string{"abc:10"}) // Non-numeric key
	require.Error(t, err)
	_, err = ParseBanWeights([]string{"4*3:10"}) // Star not at the end
	require.Error(t, err)
}

func TestConfig_WeightFor(t *testing.T) {
	conf := NewConfig()
	weights, err := ParseBanWeights([]string{"42908:0", "42903:0", "42905:0", "42910:0", "42909:10", "429*:1", "403*:2", "4*:1", "5*:1"})
	require.NoError(t, err)
	conf.BanWeights = weights
	// Longest-match-wins: exact 5-digit beats "429*" beats "4*" beats "*".
	require.Equal(t, 0, conf.weightFor(42908))
	require.Equal(t, 10, conf.weightFor(42909))
	require.Equal(t, 1, conf.weightFor(42901))
	require.Equal(t, 2, conf.weightFor(40311))
	require.Equal(t, 1, conf.weightFor(40011))
	require.Equal(t, 1, conf.weightFor(50312))
	// A code that matches nothing (no "*" here) returns 0.
	require.Equal(t, 0, conf.weightFor(30012))
}
