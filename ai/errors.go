package ai

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by the AI layer. The server maps ErrDisabled to a 400 and
// ErrBudgetExceeded to a 429 (rate limiting) response.
var (
	// ErrDisabled is returned when no AI provider is configured.
	ErrDisabled = errors.New("ai is not enabled")

	// ErrBudgetExceeded is returned when a request would exceed the visitor or global
	// daily token budget. Wrapped with details about which budget was hit.
	ErrBudgetExceeded = errors.New("ai token budget exceeded")

	// ErrStreamingNotSupported is returned when a caller requests streaming but the
	// configured provider does not implement the Streamer interface. Callers should
	// fall back to a non-streaming Complete.
	ErrStreamingNotSupported = errors.New("provider does not support streaming")

	// ErrInvalidResponse is returned when a provider response cannot be parsed into the
	// expected structure (e.g. the planner asked for JSON and got prose). This is a
	// provider-side failure, not user error.
	ErrInvalidResponse = errors.New("invalid ai response")
)

// ProviderError is an error response from a provider API, with its HTTP status code.
type ProviderError struct {
	Provider string // Provider name, e.g. "openai"
	Status   int    // HTTP status code
	Message  string // Error message from the provider, trimmed
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %s returned status %d: %s", e.Provider, e.Status, e.Message)
}
