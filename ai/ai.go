// Package ai provides the provider-agnostic AI layer for the axon server: a chat-completion
// abstraction over cloud and local LLM providers, a response cache, and per-visitor and
// global token budgets.
//
// The package has no dependency on the server package. The server holds a *Client that is
// nil when AI is disabled; all call sites must be nil-safe (see Client.Enabled).
package ai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"heckel.io/ntfy/v2/metrics"
)

// DefaultTemperature is applied when a request does not set Temperature (zero value).
// AI features in axon are mostly deterministic extraction tasks, so a low default is used.
const DefaultTemperature = 0.2

// DefaultProviderTimeout is the http.Client backstop for provider calls. The effective
// per-request deadline is min(request context, Client.timeout); this is only a safety net.
const DefaultProviderTimeout = 60 * time.Second

// Config configures the AI client. It is populated from the server config (see
// server/config.go AI options and cmd/serve.go flags).
type Config struct {
	Provider                string             // "openai", "anthropic", "openai-compatible", "ollama", "mock"
	BaseURL                 string             // Provider API base URL; empty = provider default
	APIKey                  string             // Provider API key; not required for local providers
	Model                   string             // Default model for all features
	FeatureModels           map[Feature]string // Per-feature model overrides
	RequestTimeout          time.Duration      // Hard deadline per completion; 0 = DefaultRequestTimeout
	VisitorDailyTokenBudget int64              // Daily token budget per visitor (input+output); 0 = unlimited
	GlobalDailyTokenBudget  int64              // Daily token budget server-wide (input+output); 0 = unlimited
	CacheSize               int64              // Response cache size in bytes; 0 = DefaultCacheSize
	HTTPClient              *http.Client       // Optional; used by tests
}

// DefaultRequestTimeout is the per-completion deadline if Config.RequestTimeout is unset.
const DefaultRequestTimeout = 10 * time.Second

// Client wires a Provider with a response cache and token budgets. It is the only type
// other packages should use.
type Client struct {
	provider     Provider
	budget       *Budget
	cache        *Cache
	timeout      time.Duration
	defaultModel string
	featureModel map[Feature]string
}

// New creates a Client from conf. Returns an error for unknown providers or missing settings.
func New(conf *Config) (*Client, error) {
	provider, err := newProvider(conf.Provider, conf.BaseURL, conf.APIKey, conf.Model, conf.HTTPClient)
	if err != nil {
		return nil, err
	}
	timeout := conf.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	featureModel := make(map[Feature]string, len(conf.FeatureModels))
	for k, v := range conf.FeatureModels {
		if v != "" {
			featureModel[k] = v
		}
	}
	return &Client{
		provider:     provider,
		budget:       NewBudget(conf.VisitorDailyTokenBudget, conf.GlobalDailyTokenBudget),
		cache:        NewCache(conf.CacheSize),
		timeout:      timeout,
		defaultModel: conf.Model,
		featureModel: featureModel,
	}, nil
}

// Enabled reports whether AI is configured. It is safe to call on a nil *Client, so
// call sites can use `if s.ai.Enabled()` unconditionally.
func (c *Client) Enabled() bool {
	return c != nil && c.provider != nil
}

// ProviderName returns the configured provider name, or "" on a nil/disabled client.
func (c *Client) ProviderName() string {
	if !c.Enabled() {
		return ""
	}
	return c.provider.Name()
}

// DefaultModel returns the default model, or the provider's default if unset.
func (c *Client) DefaultModel() string {
	if !c.Enabled() {
		return ""
	}
	if c.defaultModel != "" {
		return c.defaultModel
	}
	if m, ok := c.provider.(interface{ DefaultModel() string }); ok {
		return m.DefaultModel()
	}
	return ""
}

// Complete runs a completion: cache lookup, budget check, provider call, usage recording.
// A cache hit neither charges the budget nor calls the provider.
func (c *Client) Complete(ctx context.Context, req *Request) (*Response, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	if req.Model == "" {
		req.Model = c.modelFor(req.Feature)
	}
	if req.Temperature == 0 {
		req.Temperature = DefaultTemperature
	}
	if cached := c.cache.Get(req); cached != nil {
		cached.Cached = true
		metrics.AICacheHits.WithLabelValues(string(req.Feature)).Inc()
		return cached, nil
	}
	if err := c.budget.Allow(req.UserKey); err != nil {
		metrics.AIRequestsFailure.WithLabelValues(string(req.Feature)).Inc()
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	start := time.Now()
	response, err := c.provider.Complete(callCtx, req)
	metrics.AIRequestDuration.WithLabelValues(string(req.Feature)).Set(float64(time.Since(start).Milliseconds()))
	if err != nil {
		metrics.AIRequestsFailure.WithLabelValues(string(req.Feature)).Inc()
		return nil, err
	}
	metrics.AIRequestsSuccess.WithLabelValues(string(req.Feature)).Inc()
	metrics.AITokensInput.WithLabelValues(string(req.Feature)).Add(float64(response.InputTokens))
	metrics.AITokensOutput.WithLabelValues(string(req.Feature)).Add(float64(response.OutputTokens))
	c.budget.Record(req.UserKey, response.InputTokens, response.OutputTokens)
	c.cache.Store(req, response)
	return response, nil
}

// Ping checks provider health.
func (c *Client) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	return c.provider.Ping(ctx)
}

// Report returns today's usage for a visitor key and the configured budgets.
func (c *Client) Report(userKey string) *UsageReport {
	if !c.Enabled() {
		return &UsageReport{}
	}
	return c.budget.Report(userKey)
}

// Mock returns the underlying MockProvider if this client was created with the "mock"
// provider, or nil otherwise. It is used by tests to script responses of a fully wired
// server, and by operators for local experimentation (ai-provider: mock).
func (c *Client) Mock() *MockProvider {
	if !c.Enabled() {
		return nil
	}
	if mock, ok := c.provider.(*MockProvider); ok {
		return mock
	}
	return nil
}

// modelFor resolves the per-feature model override, falling back to the default model.
func (c *Client) modelFor(feature Feature) string {
	if m, ok := c.featureModel[feature]; ok && m != "" {
		return m
	}
	return c.defaultModel
}

// newProvider creates the configured provider implementation.
func newProvider(name, baseURL, apiKey, defaultModel string, httpClient *http.Client) (Provider, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultProviderTimeout}
	}
	switch name {
	case "openai":
		return newOpenAIProvider("openai", defaultOpenAIBaseURL, baseURL, apiKey, defaultModel, httpClient)
	case "openai-compatible":
		return newOpenAIProvider("openai-compatible", "", baseURL, apiKey, defaultModel, httpClient)
	case "ollama":
		return newOpenAIProvider("ollama", defaultOllamaBaseURL, baseURL, apiKey, defaultModel, httpClient)
	case "anthropic":
		return newAnthropicProvider(defaultAnthropicBaseURL, baseURL, apiKey, defaultModel, httpClient)
	case "mock":
		return NewMockProvider(defaultModel), nil
	case "":
		return nil, fmt.Errorf("%w: ai-provider is required", ErrDisabled)
	default:
		return nil, fmt.Errorf("unknown ai-provider %q, must be one of: openai, anthropic, openai-compatible, ollama, mock", name)
	}
}
