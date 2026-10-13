package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func streamingClient(t *testing.T, handler http.HandlerFunc, timeout time.Duration) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c, err := New(&Config{Provider: "openai-compatible", BaseURL: srv.URL, RequestTimeout: timeout})
	require.NoError(t, err)
	return c, srv
}

func TestCompleteStream_TinyChunksStillChargeInputAndOutput(t *testing.T) {
	c, srv := streamingClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 8; i++ {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"x"}}]}`)
		}
		fmt.Fprintln(w, "data: [DONE]")
	}, time.Second)
	defer srv.Close()
	events, err := c.CompleteStream(context.Background(), &Request{Feature: FeatureChat, Prompt: "nonempty prompt", UserKey: "user:one"})
	require.NoError(t, err)
	for range events {
	}
	usage := c.Report("user:one").Visitor
	require.Positive(t, usage.InputTokens)
	require.Equal(t, int64(2), usage.OutputTokens)
}

func TestCompleteStream_DeadlineBoundsProvider(t *testing.T) {
	c, srv := streamingClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, 30*time.Millisecond)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	events, err := c.CompleteStream(ctx, &Request{Feature: FeatureChat, Prompt: "question"})
	require.NoError(t, err)
	var streamErr error
	for event := range events {
		if event.Err != nil {
			streamErr = event.Err
		}
	}
	require.ErrorIs(t, streamErr, context.DeadlineExceeded)
	deadline, _ := ctx.Deadline()
	require.Greater(t, time.Until(deadline), time.Duration(0))
}

func TestCompleteStream_CancellationReleasesUndrainedOutput(t *testing.T) {
	closed := make(chan struct{})
	c, srv := streamingClient(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hello"}}]}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}, time.Second)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	events, err := c.CompleteStream(ctx, &Request{Feature: FeatureChat, Prompt: "question"})
	require.NoError(t, err)
	defer func() {
		for range events {
		}
	}()
	// No output is read until the call is canceled.
	cancel()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("provider request remained open")
	}
	require.Eventually(t, func() bool {
		stack := make([]byte, 64*1024)
		n := runtime.Stack(stack, true)
		return !strings.Contains(string(stack[:n]), "(*Client).CompleteStream.func1")
	}, time.Second, 5*time.Millisecond, "client stream goroutine remained blocked after cancellation")
}

func TestOpenAIStream_RejectsMalformedOrTruncatedSSE(t *testing.T) {
	for _, body := range []string{`data: {broken}` + "\n", `data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n", "data: " + strings.Repeat("x", 1024*1024+1) + "\n"} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			c, srv := streamingClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }, time.Second)
			defer srv.Close()
			events, err := c.CompleteStream(context.Background(), &Request{Feature: FeatureChat, Prompt: "question"})
			require.NoError(t, err)
			var streamErr error
			for event := range events {
				if event.Err != nil {
					streamErr = event.Err
				}
			}
			require.Error(t, streamErr)
		})
	}
}
