package ai

import (
	"context"

	"heckel.io/ntfy/v2/metrics"
)

// StreamEvent is one incremental event from a streaming completion.
type StreamEvent struct {
	Delta        string // Text chunk
	Err          error  // Terminal error (stream aborted)
	FinishReason string // Set on the final event
}

// Streamer is the optional streaming capability of a Provider. Providers that do not
// implement it fall back to a single-shot Complete (the server degrades gracefully).
type Streamer interface {
	Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error)
}

// CompleteStream runs a streaming completion: budget check, provider stream, usage
// recording. Streaming responses bypass the response cache (deltas cannot be replayed
// meaningfully) but are budgeted exactly like non-streaming calls. Token accounting is
// approximate for providers that do not report usage on streams (estimated from text
// length).
func (c *Client) CompleteStream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	if req.Model == "" {
		req.Model = c.modelFor(req.Feature)
	}
	if req.Temperature == 0 {
		req.Temperature = DefaultTemperature
	}
	if err := c.budget.Allow(req.UserKey); err != nil {
		metrics.AIRequestsFailure.WithLabelValues(string(req.Feature)).Inc()
		return nil, err
	}
	streamer, ok := c.provider.(Streamer)
	if !ok {
		return nil, ErrStreamingNotSupported
	}
	events, err := streamer.Stream(ctx, req)
	if err != nil {
		metrics.AIRequestsFailure.WithLabelValues(string(req.Feature)).Inc()
		return nil, err
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		inputTokens, outputTokens := int64(0), int64(0)
		for event := range events {
			if event.Err != nil {
				metrics.AIRequestsFailure.WithLabelValues(string(req.Feature)).Inc()
				out <- StreamEvent{Err: event.Err}
				return
			}
			outputTokens += int64(len(event.Delta) / 4) // Approximation when the provider does not report stream usage
			out <- event
		}
		metrics.AIRequestsSuccess.WithLabelValues(string(req.Feature)).Inc()
		metrics.AITokensInput.WithLabelValues(string(req.Feature)).Add(float64(inputTokens))
		metrics.AITokensOutput.WithLabelValues(string(req.Feature)).Add(float64(outputTokens))
		c.budget.Record(req.UserKey, inputTokens, outputTokens)
	}()
	return out, nil
}
