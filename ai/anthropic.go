package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// defaultAnthropicBaseURL is Anthropic's API root (paths include /v1/messages).
const defaultAnthropicBaseURL = "https://api.anthropic.com"

// anthropicVersion pins the Anthropic API version. Kept explicit so upgrades are a
// deliberate change.
const anthropicVersion = "2023-06-01"

// anthropicProvider speaks the Anthropic Messages API.
type anthropicProvider struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
}

func newAnthropicProvider(defaultBaseURL, baseURL, apiKey, defaultModel string, httpClient *http.Client) (Provider, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if apiKey == "" {
		return nil, fmt.Errorf("ai-api-key is required for the anthropic provider")
	}
	if defaultModel == "" {
		defaultModel = "claude-3-5-haiku-latest" // Cheap default; operators should set ai-model
	}
	return &anthropicProvider{
		baseURL:      baseURL,
		apiKey:       apiKey,
		defaultModel: defaultModel,
		httpClient:   httpClient,
	}, nil
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) DefaultModel() string { return p.defaultModel }

// modelOr falls back to the provider default if the client did not resolve a model.
func (p *anthropicProvider) modelOr(req *Request) string {
	if req.Model != "" {
		return req.Model
	}
	return p.defaultModel
}

// anthropicMessagesRequest / anthropicMessagesResponse model the Messages API.
type anthropicMessagesRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"` // Required by the Anthropic API
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    Role   `json:"role"` // Only "user" and "assistant" are supported
	Content string `json:"content"`
}

type anthropicMessagesResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *anthropicProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	system := req.System
	messages := make([]anthropicMessage, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			if system != "" {
				system += "\n\n"
			}
			system += m.Content
			continue
		}
		messages = append(messages, anthropicMessage(m)) // identical field layout (S1016)
	}
	if len(req.Messages) == 0 && req.Prompt != "" {
		messages = append(messages, anthropicMessage{Role: RoleUser, Content: req.Prompt})
	}
	if req.JSONSchema != nil {
		// Anthropic has no native response_format; fall back to prompting. Responses are
		// still validated by the caller.
		if system != "" {
			system += "\n\n"
		}
		system += SchemaPrompt(req.JSONSchema)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	apiReq := anthropicMessagesRequest{
		Model:       p.modelOr(req),
		MaxTokens:   maxTokens,
		System:      system,
		Messages:    messages,
		Temperature: temperaturePtr(req.Temperature),
	}
	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	apiResp, err := decodeJSON[anthropicMessagesResponse](p.Name(), httpResp)
	if err != nil {
		return nil, err
	}
	if apiResp.Error != nil {
		return nil, &ProviderError{Provider: p.Name(), Status: httpResp.StatusCode, Message: apiResp.Error.Message}
	}
	var text strings.Builder
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return &Response{
		Text:         text.String(),
		Model:        apiResp.Model,
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
		FinishReason: apiResp.StopReason,
	}, nil
}

// Stream satisfies the Streamer interface using a non-streaming Complete: Anthropic
// SSE support is a future enhancement, and callers degrade gracefully to one delta.
func (p *anthropicProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	response, err := p.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	events := make(chan StreamEvent, 2)
	events <- StreamEvent{Delta: response.Text}
	events <- StreamEvent{FinishReason: response.FinishReason}
	close(events)
	return events, nil
}

func (p *anthropicProvider) Ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/v1/models", nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return &ProviderError{Provider: p.Name(), Status: httpResp.StatusCode, Message: httpResp.Status}
	}
	return nil
}
