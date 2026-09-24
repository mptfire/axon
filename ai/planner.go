package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Planner limits. These bound what a model can produce (and therefore what the server
// will echo back into the web app), independent of the model's cooperation.
const (
	PlannerMaxPromptChars       = 1000
	PlannerMaxSubscriptions     = 5
	PlannerMaxTopicChars        = 64
	PlannerMaxTextChars         = 2000 // publisher instructions / justification
	PlannerMaxFollowUpQuestions = 3
)

// topicNameRegex mirrors the server's topicRegex: topics are [-_A-Za-z0-9]{1,64}.
var topicNameRegex = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

// PlanFilters is the filter part of a planned subscription. It maps 1:1 to the
// subscription model the web app already stores (Dexie) and accounts sync (user.prefs).
type PlanFilters struct {
	Search      string `json:"search,omitempty"`       // Server-side query filter (?q=...)
	MinPriority int    `json:"min_priority,omitempty"` // 1..5 (at least this priority)
}

// PlanSubscription is one subscription the planner proposes.
type PlanSubscription struct {
	Topic         string       `json:"topic"`
	DisplayName   string       `json:"display_name,omitempty"`
	Filters       *PlanFilters `json:"filters,omitempty"`
	Justification string       `json:"justification,omitempty"`
}

// Plan is the full planner output. BaseURL is set by the server (never by the model):
// plans always subscribe on the server the user is talking to. Disclaimer is likewise
// server-set, so prompt content can never forge or omit it (prompt-injection rule).
type Plan struct {
	BaseURL               string              `json:"base_url"`
	Subscriptions         []*PlanSubscription `json:"subscriptions"`
	PublisherInstructions string              `json:"publisher_instructions,omitempty"`
	FollowUpQuestions     []string            `json:"follow_up_questions,omitempty"`
	Disclaimer            string              `json:"disclaimer"`
}

// PlannerDisclaimer is stamped onto every plan by the server.
const PlannerDisclaimer = "AI-generated plan — review before applying. You can edit every field before it is used."

// TuneResult is the planner output when adjusting an existing subscription.
type TuneResult struct {
	DisplayName   string       `json:"display_name,omitempty"`
	Filters       *PlanFilters `json:"filters,omitempty"`
	Justification string       `json:"justification,omitempty"`
}

// TuneInput describes the subscription to adjust.
type TuneInput struct {
	Topic       string
	DisplayName string
	Search      string
	MinPriority int
}

// Planner turns natural-language requests into reviewable subscription plans. It is a
// thin, strict wrapper around a Client: schema-constrained completion, then server-side
// validation and sanitization. Plans are never applied by the planner — the web app
// applies them through the existing subscription endpoints.
type Planner struct {
	client *Client
}

// NewPlanner creates a Planner on top of a Client.
func NewPlanner(client *Client) *Planner {
	return &Planner{client: client}
}

// ValidatePrompt checks the shared prompt rules (present, within the length cap) without
// calling the provider. Handlers use it before consuming the per-visitor plan quota.
func ValidatePrompt(prompt string) error {
	if len(prompt) > PlannerMaxPromptChars {
		return fmt.Errorf("prompt too long, max %d characters", PlannerMaxPromptChars)
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	return nil
}

// Plan generates a subscription plan for a natural-language prompt.
func (p *Planner) Plan(ctx context.Context, userKey, baseURL, prompt, locale string, existingTopics []string) (*Plan, error) {
	if err := ValidatePrompt(prompt); err != nil {
		return nil, err
	}
	request := &Request{
		Feature:     FeaturePlan,
		System:      planSystemPrompt(baseURL, locale, existingTopics),
		Prompt:      prompt,
		JSONSchema:  PlanSchema,
		MaxTokens:   2048,
		Temperature: 0.2,
		UserKey:     userKey,
	}
	response, err := p.client.Complete(ctx, request)
	if err != nil {
		return nil, err
	}
	return parsePlan(baseURL, response.Text)
}

// Tune proposes adjustments to an existing subscription, given a natural-language goal.
func (p *Planner) Tune(ctx context.Context, userKey string, input *TuneInput, goal string) (*TuneResult, error) {
	if err := ValidatePrompt(goal); err != nil {
		return nil, err
	}
	current, _ := json.MarshalIndent(input, "", "  ")
	request := &Request{
		Feature:     FeatureTune,
		System:      tuneSystemPrompt(),
		Prompt:      fmt.Sprintf("Current subscription settings:\n\n%s\n\nWhat the user wants:\n\n%s", string(current), goal),
		JSONSchema:  TuneSchema,
		MaxTokens:   1024,
		Temperature: 0.2,
		UserKey:     userKey,
	}
	response, err := p.client.Complete(ctx, request)
	if err != nil {
		return nil, err
	}
	return parseTune(response.Text)
}

// parsePlan validates and sanitizes raw model output into a Plan.
func parsePlan(baseURL, text string) (*Plan, error) {
	if err := ValidateJSONObject(text); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidResponse, err)
	}
	var raw struct {
		Subscriptions []*struct {
			Topic         string `json:"topic"`
			DisplayName   string `json:"display_name"`
			Search        string `json:"search"`
			MinPriority   int    `json:"min_priority"`
			Justification string `json:"justification"`
		} `json:"subscriptions"`
		PublisherInstructions string   `json:"publisher_instructions"`
		FollowUpQuestions     []string `json:"follow_up_questions"`
	}
	if err := json.Unmarshal([]byte(stripFences(text)), &raw); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON: %w", ErrInvalidResponse, err)
	}
	if len(raw.Subscriptions) == 0 {
		return nil, fmt.Errorf("planner returned no subscriptions")
	}
	plan := &Plan{
		BaseURL:               baseURL,
		Disclaimer:            PlannerDisclaimer,
		PublisherInstructions: sanitizeSingleLineText(raw.PublisherInstructions, PlannerMaxTextChars),
	}
	for i, sub := range raw.Subscriptions {
		if i >= PlannerMaxSubscriptions {
			break
		}
		topic := strings.TrimSpace(sub.Topic)
		if !topicNameRegex.MatchString(topic) {
			return nil, fmt.Errorf("planner returned invalid topic %q", topic)
		}
		planned := &PlanSubscription{
			Topic:         topic,
			DisplayName:   sanitizeSingleLineText(sub.DisplayName, 128),
			Justification: sanitizeSingleLineText(sub.Justification, PlannerMaxTextChars),
		}
		if sub.Search != "" || (sub.MinPriority >= 1 && sub.MinPriority <= 5) {
			filters := &PlanFilters{Search: sanitizeSingleLineText(sub.Search, 256)}
			if sub.MinPriority >= 1 && sub.MinPriority <= 5 {
				filters.MinPriority = sub.MinPriority
			}
			planned.Filters = filters
		}
		plan.Subscriptions = append(plan.Subscriptions, planned)
	}
	for i, q := range raw.FollowUpQuestions {
		if i >= PlannerMaxFollowUpQuestions {
			break
		}
		if q = sanitizeSingleLineText(q, 200); q != "" {
			plan.FollowUpQuestions = append(plan.FollowUpQuestions, q)
		}
	}
	return plan, nil
}

// parseTune validates and sanitizes raw tune output.
func parseTune(text string) (*TuneResult, error) {
	if err := ValidateJSONObject(text); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidResponse, err)
	}
	var raw struct {
		DisplayName   string `json:"display_name"`
		Search        string `json:"search"`
		MinPriority   int    `json:"min_priority"`
		Justification string `json:"justification"`
	}
	if err := json.Unmarshal([]byte(stripFences(text)), &raw); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON: %w", ErrInvalidResponse, err)
	}
	result := &TuneResult{
		DisplayName:   sanitizeSingleLineText(raw.DisplayName, 128),
		Justification: sanitizeSingleLineText(raw.Justification, PlannerMaxTextChars),
	}
	if raw.Search != "" || (raw.MinPriority >= 1 && raw.MinPriority <= 5) {
		filters := &PlanFilters{Search: sanitizeSingleLineText(raw.Search, 256)}
		if raw.MinPriority >= 1 && raw.MinPriority <= 5 {
			filters.MinPriority = raw.MinPriority
		}
		result.Filters = filters
	}
	return result, nil
}

// sanitizeSingleLineText strips control characters (defensive against model output that
// contains markup or smuggled newlines) and hard-caps the length. Model output is data.
func sanitizeSingleLineText(text string, maxChars int) string {
	text = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(text))
	runes := []rune(text)
	if len(runes) > maxChars {
		return string(runes[:maxChars])
	}
	return text
}

// stripFences removes markdown code fences some models add despite instructions.
func stripFences(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
