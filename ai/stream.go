package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"heckel.io/ntfy/v2/metrics"
)

// StreamEvent is one incremental event from a streaming completion.
type StreamEvent struct {
	Delta        string // Text chunk
	Err          error  // Terminal error (stream aborted)
	FinishReason string // Set on the final event
	Usage        *Usage // Optional provider-reported input/output token counts
}

// Streamer is the optional streaming capability of a Provider.
type Streamer interface {
	Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error)
}

// maxStreamResponseBytes bounds output from a malfunctioning provider.
const maxStreamResponseBytes = 64 * 1024

// CompleteStream applies the same deadline and budgets as Complete. Provider usage
// wins when available; otherwise input and aggregate output are estimated. Partial
// output is charged even when a stream fails or its consumer cancels.
func (c *Client) CompleteStream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	request := *req // Callers may reuse requests concurrently.
	if request.MaxTokens <= 0 {
		request.MaxTokens = 1024
	}
	if request.Model == "" {
		request.Model = c.modelFor(request.Feature)
	}
	streamer, ok := c.provider.(Streamer)
	if !ok {
		return nil, ErrStreamingNotSupported
	}
	reservation, err := c.budget.Reserve(request.UserKey, requestInputTokens(&request)+int64(request.MaxTokens))
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	events, err := streamer.Stream(callCtx, &request)
	if err != nil {
		cancel()
		reservation.Finish(0, 0)
		metrics.AIRequestsFailure.WithLabelValues(string(request.Feature)).Inc()
		return nil, err
	}
	out := make(chan StreamEvent, 1)
	go func() {
		defer close(out)
		defer cancel()
		input := requestInputTokens(&request)
		var outputBytes int64
		var reported *Usage
		success := false
		defer func() {
			output := (outputBytes + 3) / 4
			if reported != nil && reported.Total() > 0 {
				input, output = reported.InputTokens, reported.OutputTokens
			}
			reservation.Finish(input, output)
			metrics.AITokensInput.WithLabelValues(string(request.Feature)).Add(float64(max(0, input)))
			metrics.AITokensOutput.WithLabelValues(string(request.Feature)).Add(float64(max(0, output)))
			if success {
				metrics.AIRequestsSuccess.WithLabelValues(string(request.Feature)).Inc()
			} else {
				metrics.AIRequestsFailure.WithLabelValues(string(request.Feature)).Inc()
			}
		}()
		fail := func(err error) {
			terminal := StreamEvent{Err: err}
			select {
			case out <- terminal:
				return
			default:
			}
			// A terminal error replaces a queued delta. This both preserves failure
			// reporting and releases the goroutine when the consumer stopped reading.
			select {
			case <-out:
			default:
			}
			select {
			case out <- terminal:
			default:
			}
		}
		for {
			select {
			case <-callCtx.Done():
				fail(callCtx.Err())
				return
			case event, open := <-events:
				if !open {
					if err := callCtx.Err(); err != nil {
						fail(err)
					} else {
						success = true
					}
					return
				}
				if event.Usage != nil {
					if event.Usage.InputTokens < 0 || event.Usage.OutputTokens < 0 {
						fail(ErrInvalidResponse)
						return
					}
					reported = event.Usage
				}
				if event.Err != nil {
					fail(event.Err)
					return
				}
				outputBytes += int64(len(event.Delta))
				if outputBytes > maxStreamResponseBytes {
					fail(fmt.Errorf("%w: stream output exceeds limit", ErrInvalidResponse))
					return
				}
				select {
				case out <- event:
				case <-callCtx.Done():
					fail(callCtx.Err())
					return
				}
			}
		}
	}()
	return out, nil
}

func requestInputTokens(req *Request) int64 {
	bytes := len(req.System)
	if len(req.Messages) > 0 {
		for _, m := range req.Messages {
			bytes += len(m.Content)
		}
	} else {
		bytes += len(req.Prompt)
	}
	if req.JSONSchema != nil {
		schema, _ := json.Marshal(req.JSONSchema.Schema)
		bytes += len(schema)
	}
	return int64((bytes + 3) / 4)
}

func sendStreamEvent(ctx context.Context, events chan<- StreamEvent, event StreamEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
