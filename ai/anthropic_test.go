package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newAnthropicTestServer(t *testing.T, handler http.HandlerFunc) (Provider, *[]map[string]any) {
	t.Helper()
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := map[string]any{}
		_ = json.Unmarshal(body, &req)
		requests = append(requests, req)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	provider, err := newAnthropicProvider(server.URL, "", "sk-ant-test", "test-model", server.Client())
	require.Nil(t, err)
	return provider, &requests
}

func TestAnthropicProvider_Complete(t *testing.T) {
	var seenHeaders http.Header
	provider, requests := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenHeaders = r.Header
		require.Equal(t, "/v1/messages", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "test-model",
			"content": [{"type": "text", "text": "answer"}, {"type": "other", "text": "ignored"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 7, "output_tokens": 9}
		}`))
	})
	resp, err := provider.Complete(context.Background(), &Request{
		System: "sys prompt",
		Prompt: "user prompt",
	})
	require.Nil(t, err)
	require.Equal(t, "answer", resp.Text)
	require.Equal(t, int64(7), resp.InputTokens)
	require.Equal(t, int64(9), resp.OutputTokens)
	require.Equal(t, "end_turn", resp.FinishReason)

	require.Equal(t, "sk-ant-test", seenHeaders.Get("x-api-key"))
	require.Equal(t, anthropicVersion, seenHeaders.Get("anthropic-version"))

	req := (*requests)[0]
	require.Equal(t, "sys prompt", req["system"])
	require.Equal(t, float64(1024), req["max_tokens"]) // Anthropic requires max_tokens
	messages := req["messages"].([]any)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].(map[string]any)["role"])
}

func TestAnthropicProvider_SystemRoleFoldedIntoSystem(t *testing.T) {
	provider, requests := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content": [{"type": "text", "text": "ok"}], "usage": {}}`))
	})
	_, err := provider.Complete(context.Background(), &Request{
		System:   "sys",
		Messages: []Message{{Role: RoleSystem, Content: "extra sys"}, {Role: RoleUser, Content: "u"}},
	})
	require.Nil(t, err)
	req := (*requests)[0]
	require.Equal(t, "sys\n\nextra sys", req["system"])
	require.Len(t, req["messages"].([]any), 1) // system turn never sent as a message
}

func TestAnthropicProvider_SchemaFallbackPrompt(t *testing.T) {
	provider, requests := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content": [{"type": "text", "text": "{}"}], "usage": {}}`))
	})
	schema := &Schema{Name: "plan", Schema: map[string]any{"type": "object"}}
	_, err := provider.Complete(context.Background(), &Request{Prompt: "hi", JSONSchema: schema})
	require.Nil(t, err)
	system := (*requests)[0]["system"].(string)
	require.Contains(t, system, "single JSON object")
	require.Contains(t, system, "Schema name: plan")
}

func TestAnthropicProvider_ErrorMapping(t *testing.T) {
	provider, _ := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type": "error", "error": {"type": "rate_limit_error", "message": "slow down"}}`))
	})
	_, err := provider.Complete(context.Background(), &Request{Prompt: "hi"})
	providerErr, ok := err.(*ProviderError)
	require.True(t, ok)
	require.Equal(t, 429, providerErr.Status)
	require.Equal(t, "slow down", providerErr.Message)
}

func TestAnthropicProvider_Ping(t *testing.T) {
	provider, _ := newAnthropicTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data": []}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	require.Nil(t, provider.Ping(context.Background()))
}

func TestAnthropicProvider_RequiresAPIKey(t *testing.T) {
	_, err := newAnthropicProvider(defaultAnthropicBaseURL, "", "", "", nil)
	require.ErrorContains(t, err, "ai-api-key is required")
}

func TestSchemaPromptAndValidation(t *testing.T) {
	prompt := SchemaPrompt(&Schema{Name: "plan", Schema: map[string]any{"type": "object"}})
	require.True(t, strings.Contains(prompt, "plan"))
	require.True(t, strings.Contains(prompt, "JSON schema"))

	require.Nil(t, ValidateJSONObject(`{"a": 1}`))
	require.Nil(t, ValidateJSONObject("  ```json\n{\"a\": 1}\n```  "))
	require.Error(t, ValidateJSONObject("not json at all"))
	require.Error(t, ValidateJSONObject(`[1,2]`))
	require.Error(t, ValidateJSONObject(`{"a": `))
}
