package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// rpcCall drives one JSON-RPC request through the server and returns the parsed response.
func rpcCall(t *testing.T, s *Server, id, method string, params any) map[string]any {
	t.Helper()
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	serialized, err := json.Marshal(request)
	require.Nil(t, err)
	var out bytes.Buffer
	require.Nil(t, s.Serve(context.Background(), strings.NewReader(string(serialized)+"\n"), &out))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 1)
	var response map[string]any
	require.Nil(t, json.Unmarshal([]byte(lines[0]), &response))
	return response
}

// isErrorResult reports whether a tools/call response carries an error result.
// isError:false is omitted from the JSON (omitempty), so absence means success.
func isErrorResult(t *testing.T, response map[string]any) bool {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	require.True(t, ok)
	toolResult, ok := result["isError"].(bool)
	return ok && toolResult
}

// resultText extracts the text content of a tools/call result.
func resultText(t *testing.T, response map[string]any) string {
	t.Helper()
	result := response["result"].(map[string]any)
	content := result["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}

// fakeNtfy is a minimal ntfy API fake for the tool backends.
func fakeNtfy(t *testing.T, hook func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil && hook(w, r) {
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestServe_InitializeHandshake(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh", Version: "test"})
	response := rpcCall(t, s, "1", "initialize", map[string]any{"protocolVersion": ProtocolVersion})
	result := response["result"].(map[string]any)
	require.Equal(t, ProtocolVersion, result["protocolVersion"])
	serverInfo := result["serverInfo"].(map[string]any)
	require.Equal(t, ServerName, serverInfo["name"])
	require.Equal(t, "test", serverInfo["version"])
}

func TestServe_NotificationGetsNoResponse(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	var out bytes.Buffer
	require.Nil(t, s.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"), &out))
	require.Equal(t, "", out.String())
}

func TestServe_UnknownMethod(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	response := rpcCall(t, s, "7", "resources/list", nil)
	errObj := response["error"].(map[string]any)
	require.Equal(t, float64(codeMethodNotFound), errObj["code"])
}

func TestServe_MalformedLine(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	var out bytes.Buffer
	require.Nil(t, s.Serve(context.Background(), strings.NewReader("this is not json\n"), &out))
	var response map[string]any
	require.Nil(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &response))
	require.Equal(t, "2.0", response["jsonrpc"])
	require.NotNil(t, response["error"])
}

func TestToolsList(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	response := rpcCall(t, s, "2", "tools/list", nil)
	tools := response["result"].(map[string]any)["tools"].([]any)
	names := make([]string, 0)
	for _, tool := range tools {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	require.Equal(t, []string{"publish", "read_messages", "subscribe_wait", "ask_history", "digest_topic", "briefing", "list_subscriptions", "plan_subscription"}, names)
}

func TestToolPublish(t *testing.T) {
	var seenPath, seenBody, seenTitle, seenPriority, seenTags, seenAuth string
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPut {
			seenPath = r.URL.Path
			seenTitle = r.Header.Get("X-Title")
			seenPriority = r.Header.Get("X-Priority")
			seenTags = r.Header.Get("X-Tags")
			seenAuth = r.Header.Get("Authorization")
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(r.Body)
			seenBody = buf.String()
			_, _ = w.Write([]byte(`{"id":"ABC123"}`))
			return true
		}
		return false
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL, AccessToken: "tk_secret"})

	params := map[string]any{
		"name": "publish",
		"arguments": map[string]any{
			"topic": "ci-alerts", "message": "build failed", "title": "CI", "priority": float64(4), "tags": []any{"warning", "skull"},
		},
	}
	response := rpcCall(t, s, "3", "tools/call", params)
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "ci-alerts")
	require.Contains(t, resultText(t, response), "ABC123")

	require.Equal(t, "/ci-alerts", seenPath)
	require.Equal(t, "build failed", seenBody)
	require.Equal(t, "CI", seenTitle)
	require.Equal(t, "4", seenPriority)
	require.Equal(t, "warning,skull", seenTags)
	require.Equal(t, "Bearer tk_secret", seenAuth)
}

func TestToolPublish_PathTraversalRejected(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	for _, topic := range []string{"", "../etc/passwd", "a/b", strings.Repeat("x", 65)} {
		params := map[string]any{"name": "publish", "arguments": map[string]any{"topic": topic, "message": "m"}}
		response := rpcCall(t, s, "1", "tools/call", params)
		result := response["result"].(map[string]any)
		require.Equal(t, true, result["isError"], topic)
	}
}

func TestToolReadMessages(t *testing.T) {
	var seenQuery string
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json") {
			seenQuery = r.URL.RawQuery
			fmt.Fprintln(w, `{"id":"m1","event":"message","topic":"alerts","message":"first"}`)
			fmt.Fprintln(w, `{"id":"","event":"keepalive"}`) // skipped
			fmt.Fprintln(w, `{"id":"m2","event":"message","topic":"alerts","message":"second"}`)
			return true
		}
		return false
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL})

	response := rpcCall(t, s, "4", "tools/call", map[string]any{
		"name": "read_messages", "arguments": map[string]any{"topic": "alerts", "since": "12h", "limit": float64(10)},
	})
	require.False(t, isErrorResult(t, response))
	text := resultText(t, response)
	require.Contains(t, text, "first")
	require.Contains(t, text, "second")
	require.NotContains(t, text, "keepalive")
	require.Equal(t, "poll=1&since=12h", seenQuery)
}

func TestToolSubscribeWait(t *testing.T) {
	messageReceived := make(chan struct{})
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json") {
			w.Header().Set("Content-Type", "application/x-ndjson")
			flusher := w.(http.Flusher)
			fmt.Fprintln(w, `{"id":"","event":"open"}`)
			flusher.Flush()
			<-messageReceived // hold the stream open until we're ready
			fmt.Fprintln(w, `{"id":"n1","event":"message","topic":"replies","message":"approved"}`)
			flusher.Flush()
			return true
		}
		return false
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL})

	// Drive the request on a goroutine: Serve blocks until the message arrives
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(messageReceived)
	}()
	params := map[string]any{"name": "subscribe_wait", "arguments": map[string]any{"topic": "replies", "timeout_secs": float64(5)}}
	var out bytes.Buffer
	serialized, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "5", "method": "tools/call", "params": params})
	require.Nil(t, s.Serve(context.Background(), strings.NewReader(string(serialized)+"\n"), &out))
	var response map[string]any
	require.Nil(t, json.Unmarshal(bytes.TrimSpace(out.Bytes()), &response))
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "approved")
}

func TestToolSubscribeWait_Timeout(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet {
			time.Sleep(2 * time.Second) // No message within the test's tiny budget
			return true
		}
		return false
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL, WaitMax: 200 * time.Millisecond})

	start := time.Now()
	response := rpcCall(t, s, "6", "tools/call", map[string]any{
		"name": "subscribe_wait", "arguments": map[string]any{"topic": "quiet", "timeout_secs": float64(300)},
	})
	require.Less(t, time.Since(start), time.Second) // capped by WaitMax, not 300s
	require.Contains(t, resultText(t, response), "timeout")
}

func TestToolAskHistory(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/v1/ai/chat" && r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["all"] == true {
				fmt.Fprintln(w, `{"answer":"All quiet.","citations":[]}`)
				return true
			}
			require.Equal(t, "backups", body["topic"])
			fmt.Fprintln(w, `{"answer":"Backup failed at 02:00.","citations":[]}`)
			return true
		}
		return false
	})
	defer fake.Close()

	s := New(Config{ServiceBaseURL: fake.URL, AccessToken: "tk_x"})
	response := rpcCall(t, s, "1", "tools/call", map[string]any{
		"name": "ask_history", "arguments": map[string]any{"topic": "backups", "question": "what failed?"},
	})
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "Backup failed")

	// Cross-topic (no topic argument)
	response = rpcCall(t, s, "2", "tools/call", map[string]any{
		"name": "ask_history", "arguments": map[string]any{"question": "anything broken?"},
	})
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "All quiet")

	// Anonymous + cross-topic: needs a token
	s2 := New(Config{ServiceBaseURL: fake.URL})
	response = rpcCall(t, s2, "3", "tools/call", map[string]any{
		"name": "ask_history", "arguments": map[string]any{"question": "q"},
	})
	require.True(t, isErrorResult(t, response))
}

func TestToolDigestTopic(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/v1/ai/digest" && r.Method == http.MethodPost {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return true
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			require.Equal(t, "24h", body["since"])
			fmt.Fprintln(w, `{"topic":"alerts","message_count":3,"headline":"All quiet-ish","sections":[],"disclaimer":"d"}`)
			return true
		}
		return false
	})
	defer fake.Close()

	s := New(Config{ServiceBaseURL: fake.URL, AccessToken: "tk_x"})
	response := rpcCall(t, s, "1", "tools/call", map[string]any{
		"name": "digest_topic", "arguments": map[string]any{"topic": "alerts"},
	})
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "All quiet-ish")

	// Anonymous: tool error
	s2 := New(Config{ServiceBaseURL: fake.URL})
	response = rpcCall(t, s2, "2", "tools/call", map[string]any{
		"name": "digest_topic", "arguments": map[string]any{"topic": "alerts"},
	})
	require.True(t, isErrorResult(t, response))
}

func TestToolBriefing(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/v1/ai/briefing" && r.Method == http.MethodPost {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return true
			}
			fmt.Fprintln(w, `{"headline":"One incident","sections":[],"message_count":2,"topic_count":2,"disclaimer":"d"}`)
			return true
		}
		return false
	})
	defer fake.Close()

	s := New(Config{ServiceBaseURL: fake.URL, AccessToken: "tk_x"})
	response := rpcCall(t, s, "1", "tools/call", map[string]any{
		"name": "briefing", "arguments": map[string]any{"since": "168h"},
	})
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "One incident")

	s2 := New(Config{ServiceBaseURL: fake.URL})
	response = rpcCall(t, s2, "2", "tools/call", map[string]any{
		"name": "briefing", "arguments": map[string]any{},
	})
	require.True(t, isErrorResult(t, response))
}

func TestToolListSubscriptions(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/v1/account" {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return true
			}
			fmt.Fprintln(w, `{"subscriptions":[{"base_url":"https://ntfy.example.com","topic":"ci","display_name":"CI"}]}`)
			return true
		}
		return false
	})
	defer fake.Close()

	// Without a token: tool error, not a protocol error
	s := New(Config{ServiceBaseURL: fake.URL})
	response := rpcCall(t, s, "1", "tools/call", map[string]any{"name": "list_subscriptions", "arguments": map[string]any{}})
	require.True(t, isErrorResult(t, response))

	// With a token
	s2 := New(Config{ServiceBaseURL: fake.URL, AccessToken: "tk_x"})
	response = rpcCall(t, s2, "2", "tools/call", map[string]any{"name": "list_subscriptions", "arguments": map[string]any{}})
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "ci")
}

func TestToolPlanSubscription(t *testing.T) {
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/v1/ai/plan" && r.Method == http.MethodPost {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			require.Equal(t, "notify me when backups fail", body["prompt"])
			fmt.Fprintln(w, `{"base_url":"https://x","subscriptions":[{"topic":"backups"}],"disclaimer":"d"}`)
			return true
		}
		return false
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL})
	response := rpcCall(t, s, "8", "tools/call", map[string]any{
		"name": "plan_subscription", "arguments": map[string]any{"prompt": "notify me when backups fail", "locale": "en"},
	})
	require.False(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "backups")

	// AI disabled on the server: reported as a tool error with the server's message
	fake2 := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/v1/ai/plan" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, `{"code":40059,"http":400,"error":"ai endpoints are not enabled on this server"}`)
			return true
		}
		return false
	})
	defer fake2.Close()
	s2 := New(Config{ServiceBaseURL: fake2.URL})
	response = rpcCall(t, s2, "9", "tools/call", map[string]any{
		"name": "plan_subscription", "arguments": map[string]any{"prompt": "wish"},
	})
	require.True(t, isErrorResult(t, response))
	require.Contains(t, resultText(t, response), "ai endpoints are not enabled")
}

func TestToolUnknownTool(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	response := rpcCall(t, s, "1", "tools/call", map[string]any{"name": "launch_missiles", "arguments": map[string]any{}})
	require.True(t, isErrorResult(t, response))
}
