package ai

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_BudgetReservesInFlightCalls(t *testing.T) {
	c := newTestClient(t, &Config{Provider: "mock", VisitorDailyTokenBudget: 10, GlobalDailyTokenBudget: 10})
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	c.Mock().SetHandler(func(req *Request) (*Response, error) {
		started <- struct{}{}
		<-release
		return &Response{Text: "answer", OutputTokens: 10}, nil
	})
	first := make(chan error, 1)
	go func() {
		_, err := c.Complete(context.Background(), &Request{Feature: FeatureChat, MaxTokens: 10, UserKey: "one"})
		first <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Complete(ctx, &Request{Feature: FeatureDigest, MaxTokens: 10, UserKey: "two"})
	close(release)
	require.NoError(t, <-first)
	require.ErrorIs(t, err, ErrBudgetExceeded)
	require.Equal(t, int64(10), c.Report("one").Global.Total())
}

func TestEmbedder_CallsCannotBypassBudget(t *testing.T) {
	c := newTestClient(t, &Config{Provider: "mock", GlobalDailyTokenBudget: 1})
	c.budget.Record("user:one", 1, 0)
	_, err := c.Embedder("model").Embed(context.Background(), []string{"uncached"})
	require.ErrorIs(t, err, ErrBudgetExceeded)
	require.Equal(t, 0, c.Mock().EmbedCalls())
}

func TestBudget_NegativeAndOverflowUsageCannotReopenBudget(t *testing.T) {
	b := NewBudget(10, 10)
	b.Record("one", math.MaxInt64, math.MaxInt64)
	b.Record("one", -100, -100)
	require.ErrorIs(t, b.Allow("one"), ErrBudgetExceeded)
	require.GreaterOrEqual(t, b.Report("one").Global.Total(), int64(10))
}

func TestBudget_ReservationSurvivesMidnightAndFinishesOnce(t *testing.T) {
	now := time.Date(2026, 10, 13, 23, 59, 59, 0, time.UTC)
	b := NewBudget(10, 10)
	b.now = func() time.Time { return now }
	first, err := b.Reserve("one", 8)
	require.NoError(t, err)
	now = now.Add(2 * time.Second)
	_, err = b.Reserve("two", 3)
	require.ErrorIs(t, err, ErrBudgetExceeded, "a pending call must not disappear at midnight")
	first.Finish(3, 2)
	first.Finish(3, 2)
	second, err := b.Reserve("two", 5)
	require.NoError(t, err)
	second.Finish(1, 0)
	require.Equal(t, int64(6), b.Report("one").Global.Total())
}

func TestEmbedder_ChargesProviderReportedUsage(t *testing.T) {
	c, srv := streamingClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,2]}],"usage":{"total_tokens":27}}`)
	}, time.Second)
	defer srv.Close()
	_, err := c.Embedder("model").Embed(context.Background(), []string{"one"}, "user:one")
	require.NoError(t, err)
	require.Equal(t, int64(27), c.Report("user:one").Visitor.InputTokens)
}
