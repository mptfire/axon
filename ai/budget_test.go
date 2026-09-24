package ai

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBudget_AllowAndRecord(t *testing.T) {
	b := NewBudget(100, 0) // 100 tokens/visitor, unlimited global
	require.Nil(t, b.Allow("user:phil"))
	b.Record("user:phil", 30, 20)
	b.Record("user:ben", 10, 10)
	require.Nil(t, b.Allow("user:phil")) // 50 < 100

	b.Record("user:phil", 40, 20) // 110 total
	err := b.Allow("user:phil")
	require.True(t, errors.Is(err, ErrBudgetExceeded))
	require.Contains(t, err.Error(), "visitor daily budget")

	// ben is unaffected
	require.Nil(t, b.Allow("user:ben"))

	report := b.Report("user:phil")
	require.Equal(t, int64(2), report.Visitor.Requests)
	require.Equal(t, int64(70), report.Visitor.InputTokens)
	require.Equal(t, int64(40), report.Visitor.OutputTokens)
	require.Equal(t, int64(130), report.Global.Total()) // phil 110 + ben 20
	require.Equal(t, int64(100), report.VisitorBudget)
}

func TestBudget_GlobalBudget(t *testing.T) {
	b := NewBudget(0, 100) // unlimited visitors, 100 global
	b.Record("user:phil", 60, 40)
	err := b.Allow("user:ben")
	require.True(t, errors.Is(err, ErrBudgetExceeded))
	require.Contains(t, err.Error(), "global daily budget")
}

func TestBudget_Unlimited(t *testing.T) {
	b := NewBudget(0, 0)
	for i := 0; i < 100; i++ {
		require.Nil(t, b.Allow("user:phil"))
		b.Record("user:phil", 1000, 1000)
	}
}

func TestBudget_AnonymousAttribution(t *testing.T) {
	b := NewBudget(10, 0)
	// Empty user key: allowed regardless of visitor budget, only global counts
	require.Nil(t, b.Allow(""))
	b.Record("", 500, 500)
	require.Nil(t, b.Allow(""))
}

func TestBudget_DayRollover(t *testing.T) {
	now := time.Date(2026, 9, 24, 22, 0, 0, 0, time.UTC)
	b := NewBudget(100, 100)
	b.now = func() time.Time { return now }
	b.Record("user:phil", 60, 60) // 120, over budget
	require.True(t, errors.Is(b.Allow("user:phil"), ErrBudgetExceeded))

	// Next day (UTC): counters reset
	now = now.Add(25 * time.Hour)
	require.Nil(t, b.Allow("user:phil"))
	report := b.Report("user:phil")
	require.Equal(t, "2026-09-25", report.Day)
	require.Equal(t, int64(0), report.Visitor.Total())
}

func TestBudget_ReportShape(t *testing.T) {
	b := NewBudget(1000, 5000)
	b.Record("user:phil", 10, 5)
	report := b.Report("unknown-user")
	require.Equal(t, int64(0), report.Visitor.Total())
	require.Equal(t, int64(15), report.Global.Total())
	require.Equal(t, 1000, int(report.VisitorBudget))
	require.Equal(t, 5000, int(report.GlobalBudget))
	require.True(t, strings.Contains(report.Day, "20"))
}
