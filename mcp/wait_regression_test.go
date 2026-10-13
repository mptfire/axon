package mcp

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestToolSubscribeWait_IgnoresControlEventsWithIDs(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		fmt.Fprintln(w, `{"id":"open123","event":"open","topic":"replies"}`)
		fmt.Fprintln(w, `{"id":"ping123","event":"keepalive","topic":"replies"}`)
		fmt.Fprintln(w, `{"id":"reply123","event":"message","topic":"replies","message":"approved"}`)
		return true
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL})
	result := s.toolSubscribeWait(context.Background(), map[string]any{"topic": "replies"})
	require.False(t, result.IsError)
	require.Contains(t, result.Content[0].Text, "approved")
	require.NotContains(t, result.Content[0].Text, "open123")
}

func TestToolSubscribeWait_UsesWaitDeadlineNotClientTimeout(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"event":"open"}`)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			return true
		case <-time.After(100 * time.Millisecond):
			fmt.Fprintln(w, `{"id":"reply123","event":"message","message":"approved"}`)
			return true
		}
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL, ClientTimeout: 20 * time.Millisecond, WaitMax: time.Second})
	result := s.toolSubscribeWait(context.Background(), map[string]any{"topic": "replies"})
	require.False(t, result.IsError, result.Content[0].Text)
	require.Contains(t, result.Content[0].Text, "approved")
}
