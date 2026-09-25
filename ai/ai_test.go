package ai

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_NilSafety(t *testing.T) {
	var client *Client
	require.False(t, client.Enabled())
	require.Equal(t, "", client.ProviderName())
	require.Equal(t, "", client.DefaultModel())
	require.Nil(t, client.Mock())
	require.NotNil(t, client.Report("user:phil"))
	_, err := client.Complete(context.Background(), &Request{Prompt: "hi"})
	require.ErrorIs(t, err, ErrDisabled)
	require.ErrorIs(t, client.Ping(context.Background()), ErrDisabled)
}

func TestClient_CompleteAndCache(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock", Model: "test-model"})
	mock := client.Mock()
	require.NotNil(t, mock)
	mock.EnqueueText("first response", "second response")

	resp, err := client.Complete(context.Background(), &Request{Feature: FeaturePlan, Prompt: "plan me"})
	require.Nil(t, err)
	require.Equal(t, "first response", resp.Text)
	require.False(t, resp.Cached)
	require.Equal(t, "test-model", resp.Model)

	// Same request again: cache hit, provider not called
	resp, err = client.Complete(context.Background(), &Request{Feature: FeaturePlan, Prompt: "plan me"})
	require.Nil(t, err)
	require.Equal(t, "first response", resp.Text)
	require.True(t, resp.Cached)

	// Different prompt: cache miss
	resp, err = client.Complete(context.Background(), &Request{Feature: FeaturePlan, Prompt: "other"})
	require.Nil(t, err)
	require.Equal(t, "second response", resp.Text)

	// Same prompt, different user: shared cache entry (attribution excluded from key)
	resp, err = client.Complete(context.Background(), &Request{Feature: FeaturePlan, Prompt: "plan me", UserKey: "user:phil"})
	require.Nil(t, err)
	require.True(t, resp.Cached)
}

func TestClient_BudgetEnforced(t *testing.T) {
	client := newTestClient(t, &Config{
		Provider:                "mock",
		VisitorDailyTokenBudget: 100,
		GlobalDailyTokenBudget:  150,
	})
	mock := client.Mock()
	mock.SetHandler(func(req *Request) (*Response, error) {
		return &Response{Text: "ok", InputTokens: 60, OutputTokens: 50}, nil
	})
	_, err := client.Complete(context.Background(), &Request{Feature: FeatureChat, Prompt: "1", UserKey: "user:phil"})
	require.Nil(t, err) // 110 tokens, over visitor budget of 100 — charged after the fact
	_, err = client.Complete(context.Background(), &Request{Feature: FeatureChat, Prompt: "2", UserKey: "user:phil"})
	require.ErrorIs(t, err, ErrBudgetExceeded)

	// Other visitors can still spend until the global budget is hit (110/150 used)
	_, err = client.Complete(context.Background(), &Request{Feature: FeatureChat, Prompt: "3", UserKey: "user:ben"})
	require.Nil(t, err) // global now at 220
	_, err = client.Complete(context.Background(), &Request{Feature: FeatureChat, Prompt: "4", UserKey: "user:ben"})
	require.ErrorIs(t, err, ErrBudgetExceeded)

	report := client.Report("user:phil")
	require.Equal(t, int64(1), report.Visitor.Requests)
	require.Equal(t, int64(110), report.Visitor.Total())
	require.Equal(t, int64(220), report.Global.Total()) // phil 110 + ben 110
}

func TestClient_FeatureModelOverride(t *testing.T) {
	client := newTestClient(t, &Config{
		Provider:      "mock",
		Model:         "default-model",
		FeatureModels: map[Feature]string{FeatureDigest: "digest-model"},
	})
	var seenModels []string
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		seenModels = append(seenModels, req.Model)
		return &Response{Text: "ok"}, nil
	})
	_, err := client.Complete(context.Background(), &Request{Feature: FeatureChat})
	require.Nil(t, err)
	_, err = client.Complete(context.Background(), &Request{Feature: FeatureDigest})
	require.Nil(t, err)
	_, err = client.Complete(context.Background(), &Request{Feature: FeaturePlan})
	require.Nil(t, err)
	require.Equal(t, []string{"default-model", "digest-model", "default-model"}, seenModels)
}

func TestClient_RequestTimeout(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock", RequestTimeout: 10 * time.Millisecond})
	client.Mock().Enqueue(MockResult{Response: &Response{Text: "late"}, Delay: 50 * time.Millisecond})
	_, err := client.Complete(context.Background(), &Request{Prompt: "hi"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestClient_ProviderFailureCounted(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().EnqueueError(errors.New("provider exploded"))
	_, err := client.Complete(context.Background(), &Request{Feature: FeatureEnrich})
	require.EqualError(t, err, "provider exploded")
}

func TestNew_ProviderValidation(t *testing.T) {
	_, err := New(&Config{Provider: "nonsense"})
	require.ErrorContains(t, err, "unknown ai-provider")
	_, err = New(&Config{Provider: ""})
	require.ErrorIs(t, err, ErrDisabled)

	client, err := New(&Config{Provider: "openai"})
	require.Nil(t, err)
	require.True(t, client.Enabled())
	require.Equal(t, "openai", client.ProviderName())
	require.Equal(t, "gpt-4o-mini", client.DefaultModel())

	_, err = New(&Config{Provider: "openai-compatible"})
	require.ErrorContains(t, err, "ai-base-url is required")

	client, err = New(&Config{Provider: "ollama", Model: "llama3.1"})
	require.Nil(t, err)
	require.Equal(t, "ollama", client.ProviderName())
	require.Equal(t, "llama3.1", client.DefaultModel())

	_, err = New(&Config{Provider: "anthropic"})
	require.ErrorContains(t, err, "ai-api-key is required")

	client, err = New(&Config{Provider: "poolside"})
	require.Nil(t, err)
	require.Equal(t, "poolside", client.ProviderName())
	require.Equal(t, "poolside/laguna-s-2.1", client.DefaultModel())

	client, err = New(&Config{Provider: "mock"})
	require.Nil(t, err)
	require.NotNil(t, client.Mock())
}

func newTestClient(t *testing.T, conf *Config) *Client {
	t.Helper()
	client, err := New(conf)
	require.Nil(t, err)
	return client
}

func TestMockProvider_Echo(t *testing.T) {
	mock := NewMockProvider("mock-model")
	resp, err := mock.Complete(context.Background(), &Request{Feature: FeaturePlan, Prompt: "hello"})
	require.Nil(t, err)
	require.Contains(t, resp.Text, "hello")
	require.Equal(t, "mock-model", resp.Model)

	// Messages take precedence, first user message wins
	resp, err = mock.Complete(context.Background(), &Request{
		Messages: []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "user msg"}},
	})
	require.Nil(t, err)
	require.Contains(t, resp.Text, "user msg")
}

func TestMockProvider_QueueFIFO(t *testing.T) {
	mock := NewMockProvider("")
	mock.Enqueue(
		MockResult{Err: fmt.Errorf("first fails")},
		MockResult{Response: &Response{Text: "second"}},
	)
	_, err := mock.Complete(context.Background(), &Request{})
	require.EqualError(t, err, "first fails")
	resp, err := mock.Complete(context.Background(), &Request{})
	require.Nil(t, err)
	require.Equal(t, "second", resp.Text)

	// Queue exhausted: fall back to echo
	resp, err = mock.Complete(context.Background(), &Request{Prompt: "fallback"})
	require.Nil(t, err)
	require.Contains(t, resp.Text, "fallback")
}
