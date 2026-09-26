package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockResult is a scripted completion outcome for the MockProvider.
type MockResult struct {
	Response *Response
	Err      error
	Delay    time.Duration // Simulated provider latency, applied before returning
}

// MockProvider is a scripted, in-process provider used by tests and local development
// (ai-provider: mock). It never touches the network. If no results are queued and no
// handler is set, it echoes the request back with estimated token counts.
type MockProvider struct {
	mu           sync.Mutex
	defaultModel string
	queue        []MockResult
	handler      func(req *Request) (*Response, error)
	pingErr      error
	embedCalls   int
	embedHandler func(text string) ([]float32, error)
}

// NewMockProvider creates a MockProvider. The defaultModel is reported in responses
// (it does not influence behavior).
func NewMockProvider(defaultModel string) *MockProvider {
	if defaultModel == "" {
		defaultModel = "mock-model"
	}
	return &MockProvider{defaultModel: defaultModel}
}

// Name returns the provider name.
func (p *MockProvider) Name() string { return "mock" }

// DefaultModel returns the model name reported in responses.
func (p *MockProvider) DefaultModel() string { return p.defaultModel }

// Enqueue appends a scripted result, consumed FIFO by subsequent Complete calls.
func (p *MockProvider) Enqueue(results ...MockResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = append(p.queue, results...)
}

// EnqueueText is a convenience for scripting plain-text responses with estimated tokens.
func (p *MockProvider) EnqueueText(texts ...string) {
	results := make([]MockResult, len(texts))
	for i, text := range texts {
		results[i] = MockResult{Response: &Response{
			Text:         text,
			Model:        p.defaultModel,
			InputTokens:  int64(len(text) / 4),
			OutputTokens: int64(len(text) / 4),
			FinishReason: "stop",
		}}
	}
	p.Enqueue(results...)
}

// EnqueueError scripts a provider failure.
func (p *MockProvider) EnqueueError(err error) {
	p.Enqueue(MockResult{Err: err})
}

// SetHandler installs a dynamic handler, consulted before the scripted queue. Useful to
// inspect requests in tests.
func (p *MockProvider) SetHandler(handler func(req *Request) (*Response, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = handler
}

// SetPingError makes Ping fail (e.g. to exercise the unhealthy status path).
func (p *MockProvider) SetPingError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pingErr = err
}

// Complete produces the scripted response: handler first, then queue, then echo.
func (p *MockProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	p.mu.Lock()
	var result MockResult
	if p.handler != nil {
		handler := p.handler
		p.mu.Unlock()
		// Run the handler with the caller's deadline: a slow handler must behave like a
		// slow provider (ctx.Done wins, see the select below).
		type handlerOutcome struct {
			resp *Response
			err  error
		}
		done := make(chan handlerOutcome, 1)
		go func() {
			resp, err := handler(req)
			done <- handlerOutcome{resp, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case outcome := <-done:
			if outcome.err != nil {
				return nil, outcome.err
			}
			result = MockResult{Response: outcome.resp}
		}
	} else if len(p.queue) > 0 {
		result = p.queue[0]
		p.queue = p.queue[1:]
		p.mu.Unlock()
	} else {
		p.mu.Unlock()
		result = MockResult{Response: p.echo(req)}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(result.Delay):
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return result.Response, nil
}

// Stream satisfies the Streamer interface: it produces the same result as Complete
// but emits it in small chunks, so streaming consumers can be tested end to end.
func (p *MockProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	response, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		runes := []rune(response.Text)
		for i := 0; i < len(runes); i += 8 {
			end := i + 8
			if end > len(runes) {
				end = len(runes)
			}
			select {
			case <-ctx.Done():
				events <- StreamEvent{Err: ctx.Err()}
				return
			case events <- StreamEvent{Delta: string(runes[i:end])}:
			}
		}
		events <- StreamEvent{FinishReason: response.FinishReason}
	}()
	return events, nil
}

// EmbedTexts returns deterministic per-text vectors so embedding-backed tests run
// without a real model. Similar texts do NOT produce similar vectors; use
// SetEmbedHandler when similarity semantics matter.
func (p *MockProvider) EmbedTexts(ctx context.Context, model string, texts []string) ([][]float32, error) {
	p.mu.Lock()
	p.embedCalls++
	handler := p.embedHandler
	p.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if handler != nil {
			vec, err := handler(text)
			if err != nil {
				return nil, err
			}
			out[i] = vec
			continue
		}
		out[i] = deterministicVector(text)
	}
	return out, nil
}

// EmbedCalls reports how many texts were embedded (for cost assertions in tests).
func (p *MockProvider) EmbedCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.embedCalls
}

// SetEmbedHandler installs a per-text embedding function (used by tests to control
// similarity semantics).
func (p *MockProvider) SetEmbedHandler(fn func(text string) ([]float32, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.embedHandler = fn
}

// deterministicVector derives an 8-dimensional test vector from text via FNV-1a.
func deterministicVector(text string) []float32 {
	const dims = 8
	vec := make([]float32, dims)
	h := uint64(14695981039346656037)
	for _, b := range []byte(strings.ToLower(strings.TrimSpace(text))) {
		h ^= uint64(b)
		h *= 1099511628211
		for i := 0; i < dims; i++ {
			vec[i] += float32((h >> (uint(i) * 5)) & 0x0F)
		}
	}
	return vec
}

// Ping always succeeds unless a ping error was set.
func (p *MockProvider) Ping(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pingErr
}

// echo produces a deterministic default response so that smoke tests and local servers
// with ai-provider: mock work without any scripting.
func (p *MockProvider) echo(req *Request) *Response {
	prompt := req.Prompt
	for _, m := range req.Messages {
		if m.Role == RoleUser {
			prompt = m.Content
			break
		}
	}
	if len(prompt) > 512 {
		prompt = prompt[:512]
	}
	text := fmt.Sprintf("mock response for feature %q: %s", req.Feature, prompt)
	return &Response{
		Text:         strings.TrimSpace(text),
		Model:        p.defaultModel,
		InputTokens:  int64(len(prompt) / 4),
		OutputTokens: int64(len(text) / 4),
		FinishReason: "stop",
	}
}
