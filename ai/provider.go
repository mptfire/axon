package ai

import (
	"context"
)

// Feature identifies the AI feature performing a request. It is used for budget
// attribution, per-feature model overrides, metrics, and log lines.
type Feature string

// The AI features supported by the server. Each maps to an optional model override
// (ai-model-<feature>) and its own metrics label.
const (
	FeaturePlan   Feature = "plan"   // Natural-language subscription planning (Phase 2)
	FeatureTune   Feature = "tune"   // Subscription tuning (Phase 2)
	FeatureEnrich Feature = "enrich" // Message enrichment (Phase 3)
	FeatureDigest Feature = "digest" // Scheduled digests (Phase 4)
	FeatureChat   Feature = "chat"   // Chat over notification history (Phase 5)
)

// Role is the role of a message in a chat-style conversation.
type Role string

// Supported chat roles.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single chat message.
type Message struct {
	Role    Role
	Content string
}

// Schema describes a strict JSON output schema. Providers with native structured-output
// support (OpenAI-compatible response_format) use it directly; others fall back to
// embedding the schema in the prompt (see SchemaPrompt). Either way, responses are
// validated by the caller — the schema is a hint, never a guarantee.
type Schema struct {
	Name   string         // Schema name, e.g. "subscription_plan"
	Schema map[string]any // JSON schema draft-style object
}

// Request is an AI completion request. A request is either single-turn (Prompt set) or
// multi-turn (Messages set); if both are set, Messages wins and Prompt is ignored.
//
// Message content embedded in requests is UNTRUSTED input (notification bodies are
// attacker-controllable). Callers must treat model output as data; see the fork's
// prompt-injection rules in docs/ai-plan/index.md.
type Request struct {
	Feature     Feature   // Budget/metrics attribution and model override selection
	Model       string    // Resolved by the Client if empty (per-feature override or default)
	System      string    // System prompt
	Messages    []Message // Conversation turns
	Prompt      string    // Convenience for single-turn requests
	MaxTokens   int       // Max output tokens; 0 = provider default
	Temperature float64   // 0 = deterministic (sent as-is); negative = provider default. Most axon features set 0.2 explicitly.
	JSONSchema  *Schema   // Optional strict JSON output schema
	UserKey     string    // Budget attribution key, e.g. "user:phil" or "ip:1.2.3.4"
}

// Response is a completed AI response.
type Response struct {
	Text         string
	Model        string
	InputTokens  int64
	OutputTokens int64
	FinishReason string
	Cached       bool // True when served from the response cache
}

// Provider is a chat-completion backend. Implementations must be safe for concurrent use
// and must not mutate the request.
type Provider interface {
	// Name returns the provider name, e.g. "openai".
	Name() string
	// Complete performs a chat completion.
	Complete(ctx context.Context, req *Request) (*Response, error)
	// Ping performs a cheap health check.
	Ping(ctx context.Context) error
}
