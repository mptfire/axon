package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// newOpenAITestServer spins up an OpenAI-compatible fake and returns the provider wired to it.
func newOpenAITestServer(t *testing.T, handler http.HandlerFunc) (Provider, *[]map[string]any) {
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
	provider, err := newOpenAIProvider("openai", server.URL, "", "", "test-model", server.Client())
	require.Nil(t, err)
	return provider, &requests
}

func TestOpenAIProvider_Complete(t *testing.T) {
	provider, requests := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "test-model",
			"choices": [{"message": {"role": "assistant", "content": "hello there"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 12, "completion_tokens": 34}
		}`))
	})
	resp, err := provider.Complete(context.Background(), &Request{
		Feature:     FeaturePlan,
		System:      "be brief",
		Prompt:      "hi",
		MaxTokens:   128,
		Temperature: 0.1,
	})
	require.Nil(t, err)
	require.Equal(t, "hello there", resp.Text)
	require.Equal(t, int64(12), resp.InputTokens)
	require.Equal(t, int64(34), resp.OutputTokens)
	require.Equal(t, "stop", resp.FinishReason)

	// Verify request wire format
	require.Len(t, *requests, 1)
	req := (*requests)[0]
	require.Equal(t, "test-model", req["model"])
	require.Equal(t, float64(128), req["max_tokens"])
	require.Equal(t, float64(0.1), req["temperature"])
	messages := req["messages"].([]any)
	require.Len(t, messages, 2)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Equal(t, "be brief", messages[0].(map[string]any)["content"])
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
	require.Equal(t, "hi", messages[1].(map[string]any)["content"])
	require.Nil(t, req["response_format"])
}

func TestOpenAIProvider_JSONSchema(t *testing.T) {
	provider, requests := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "{}"}, "finish_reason": "stop"}], "usage": {}}`))
	})
	schema := &Schema{Name: "plan", Schema: map[string]any{"type": "object"}}
	_, err := provider.Complete(context.Background(), &Request{Prompt: "hi", JSONSchema: schema})
	require.Nil(t, err)
	responseFormat := (*requests)[0]["response_format"].(map[string]any)
	require.Equal(t, "json_schema", responseFormat["type"])
	jsonSchema := responseFormat["json_schema"].(map[string]any)
	require.Equal(t, "plan", jsonSchema["name"])
	require.Equal(t, true, jsonSchema["strict"])
}

func TestOpenAIProvider_ErrorMapping(t *testing.T) {
	provider, _ := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"message": "boom", "type": "server_error"}}`))
	})
	_, err := provider.Complete(context.Background(), &Request{Prompt: "hi"})
	providerErr, ok := err.(*ProviderError)
	require.True(t, ok)
	require.Equal(t, 500, providerErr.Status)
	require.Equal(t, "boom", providerErr.Message)
	require.Equal(t, "openai", providerErr.Provider)
}

func TestOpenAIProvider_Ping(t *testing.T) {
	provider, _ := newOpenAITestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data": []}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	require.Nil(t, provider.Ping(context.Background()))
}

func TestOpenAIProvider_AuthHeader(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "x"}, "finish_reason": "stop"}], "usage": {}}`))
	}))
	t.Cleanup(server.Close)
	provider, err := newOpenAIProvider("openai", server.URL, "", "sk-secret", "m", server.Client())
	require.Nil(t, err)
	_, err = provider.Complete(context.Background(), &Request{Prompt: "hi"})
	require.Nil(t, err)
	require.Equal(t, "Bearer sk-secret", authHeader)
}

func TestOpenAIProvider_Defaults(t *testing.T) {
	// No base URL: the OpenAI default is used
	provider, err := newOpenAIProvider("openai", defaultOpenAIBaseURL, "", "", "", nil)
	require.Nil(t, err)
	require.Equal(t, "gpt-4o-mini", provider.(interface{ DefaultModel() string }).DefaultModel())

	// openai-compatible without a base URL is an error
	_, err = newOpenAIProvider("openai-compatible", "", "", "", "", nil)
	require.ErrorContains(t, err, "ai-base-url is required")

	// Ollama default base URL
	_, err = newOpenAIProvider("ollama", defaultOllamaBaseURL, "", "", "", nil)
	require.Nil(t, err)
}
