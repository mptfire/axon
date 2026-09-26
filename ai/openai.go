package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Default provider base URLs
const (
	defaultOpenAIBaseURL   = "https://api.openai.com/v1"
	defaultOllamaBaseURL   = "http://localhost:11434/v1"
	defaultPoolsideBaseURL = "https://inference.poolside.ai/v1" // axon
)

// openAIProvider speaks the OpenAI Chat Completions API. It also covers every
// OpenAI-compatible backend (Ollama, OpenRouter, vLLM, LM Studio, ...), which is why
// "ollama" and "openai-compatible" map to it with different base URLs.
type openAIProvider struct {
	name         string
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
}

func newOpenAIProvider(name, defaultBaseURL, baseURL, apiKey, defaultModel string, httpClient *http.Client) (Provider, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("ai-base-url is required for this provider")
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if defaultModel == "" {
		defaultModel = "gpt-4o-mini" // A safe, cheap default; operators should set ai-model
		if name == "poolside" {
			defaultModel = "poolside/laguna-s-2.1" // axon: Poolside's default model
		}
	}
	return &openAIProvider{
		name:         name,
		baseURL:      baseURL,
		apiKey:       apiKey,
		defaultModel: defaultModel,
		httpClient:   httpClient,
	}, nil
}

func (p *openAIProvider) Name() string { return p.name }

func (p *openAIProvider) DefaultModel() string { return p.defaultModel }

// modelOr falls back to the provider default if the client did not resolve a model.
func (p *openAIProvider) modelOr(req *Request) string {
	if req.Model != "" {
		return req.Model
	}
	return p.defaultModel
}

// openAIChatRequest / openAIChatResponse model the Chat Completions API. Only the fields
// ntfy uses are included; unknown fields are ignored on decode.
type openAIChatRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	Temperature    *float64              `json:"temperature,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
	Stream         bool                  `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type       string              `json:"type"` // "json_schema"
	JSONSchema *openAIJSONSchemaEl `json:"json_schema"`
}

type openAIJSONSchemaEl struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (p *openAIProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: RoleSystem, Content: req.System})
	}
	for _, m := range req.Messages {
		messages = append(messages, openAIMessage(m)) // identical field layout (S1016)
	}
	if len(req.Messages) == 0 && req.Prompt != "" {
		messages = append(messages, openAIMessage{Role: RoleUser, Content: req.Prompt})
	}
	chatReq := openAIChatRequest{
		Model:       p.modelOr(req),
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: temperaturePtr(req.Temperature),
	}
	if req.JSONSchema != nil {
		chatReq.ResponseFormat = &openAIResponseFormat{
			Type:       "json_schema",
			JSONSchema: &openAIJSONSchemaEl{Name: req.JSONSchema.Name, Schema: req.JSONSchema.Schema, Strict: true},
		}
	}
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	chatResp, err := decodeJSON[openAIChatResponse](p.name, httpResp)
	if err != nil {
		return nil, err
	}
	if chatResp.Error != nil {
		return nil, &ProviderError{Provider: p.name, Status: httpResp.StatusCode, Message: chatResp.Error.Message}
	}
	if len(chatResp.Choices) == 0 {
		return nil, &ProviderError{Provider: p.name, Status: httpResp.StatusCode, Message: "no choices in response"}
	}
	return &Response{
		Text:         chatResp.Choices[0].Message.Content,
		Model:        chatResp.Model,
		InputTokens:  chatResp.Usage.PromptTokens,
		OutputTokens: chatResp.Usage.CompletionTokens,
		FinishReason: chatResp.Choices[0].FinishReason,
	}, nil
}

// Stream implements the Streamer interface using the Chat Completions SSE protocol:
// lines of "data: {...}" terminated by "data: [DONE]", with deltas in
// choices[0].delta.content.
func (p *openAIProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: RoleSystem, Content: req.System})
	}
	for _, m := range req.Messages {
		messages = append(messages, openAIMessage(m)) // identical field layout (S1016)
	}
	if len(req.Messages) == 0 && req.Prompt != "" {
		messages = append(messages, openAIMessage{Role: RoleUser, Content: req.Prompt})
	}
	chatReq := openAIChatRequest{
		Model:       p.modelOr(req),
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: temperaturePtr(req.Temperature),
		Stream:      true,
	}
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, 4096))
		return nil, &ProviderError{Provider: p.name, Status: httpResp.StatusCode, Message: httpResp.Status}
	}
	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		defer httpResp.Body.Close()
		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		finish := ""
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue // Keepalives/comments and unparseable chunks are skipped
			}
			if len(chunk.Choices) > 0 {
				if delta := chunk.Choices[0].Delta.Content; delta != "" {
					events <- StreamEvent{Delta: delta}
				}
				if chunk.Choices[0].FinishReason != "" {
					finish = chunk.Choices[0].FinishReason
				}
			}
		}
		events <- StreamEvent{FinishReason: finish}
	}()
	return events, nil
}

// EmbedTexts computes embeddings via the OpenAI-compatible /embeddings endpoint
// (OpenAI, Ollama with embedding models, Poolside, ...).
func (p *openAIProvider) EmbedTexts(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{Model: model, Input: texts})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, 4096))
		return nil, &ProviderError{Provider: p.name, Status: httpResp.StatusCode, Message: httpResp.Status}
	}
	var embResp struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("invalid embeddings response: %w", err)
	}
	out := make([][]float32, len(texts))
	for _, item := range embResp.Data {
		if item.Index >= 0 && item.Index < len(out) {
			out[item.Index] = item.Embedding
		}
	}
	return out, nil
}

func (p *openAIProvider) Ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResp.Body, 4096))
		return &ProviderError{Provider: p.name, Status: httpResp.StatusCode, Message: httpResp.Status}
	}
	return nil
}

// temperaturePtr maps Request.Temperature to the API field: nil = provider default,
// otherwise the value as-is.
func temperaturePtr(t float64) *float64 {
	if t < 0 {
		return nil
	}
	return &t
}

// decodeJSON decodes a provider API response, mapping non-2xx statuses to a ProviderError.
// It prefers a structured {"error": {"message": ...}} body, then the raw body.
func decodeJSON[T any](providerName string, httpResp *http.Response) (*T, error) {
	limitReader := io.LimitReader(httpResp.Body, 32*1024*1024)
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		raw, _ := io.ReadAll(limitReader)
		message := strings.TrimSpace(string(raw))
		var structured struct {
			Error *openAIError `json:"error"`
		}
		if json.Unmarshal(raw, &structured) == nil && structured.Error != nil && structured.Error.Message != "" {
			message = structured.Error.Message
		}
		if len(message) > 512 {
			message = message[:512]
		}
		return nil, &ProviderError{Provider: providerName, Status: httpResp.StatusCode, Message: message}
	}
	var v T
	if err := json.NewDecoder(limitReader).Decode(&v); err != nil {
		return nil, fmt.Errorf("invalid provider response: %w", err)
	}
	return &v, nil
}
