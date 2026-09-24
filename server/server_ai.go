package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/metrics"
	"heckel.io/ntfy/v2/model"
	"heckel.io/ntfy/v2/user"
)

// aiPingTimeout bounds the provider health check in the status endpoint.
const aiPingTimeout = 2 * time.Second

// aiRequestsPerDay caps AI feature requests (plans, tunes, digests) per visitor per day
// (burst = the full daily allowance). The token budget in the ai.Client is the real cost
// control; this keeps prompt spam from wasting even cached budget.
const aiRequestsPerDay = 10

// ensureAIEnabled fails requests with 400 if the AI layer is not configured. Like the
// nil-if-unconfigured mailer/stripe/twilio guards, this keeps every AI route nil-safe.
func (s *Server) ensureAIEnabled(next handleFunc) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, v *visitor) error {
		if !s.ai.Enabled() {
			return errHTTPBadRequestAIDisabled
		}
		return next(w, r, v)
	}
}

// apiAIStatusResponse is the admin-facing view of the AI layer (GET /v1/ai/status).
type apiAIStatusResponse struct {
	Enabled  bool            `json:"enabled"`
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Healthy  bool            `json:"healthy"`
	Health   string          `json:"health_error,omitempty"`
	Usage    *ai.UsageReport `json:"usage"`
}

// handleAIStatus reports provider health and today's global token usage (admin only).
func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request, v *visitor) error {
	ctx, cancel := context.WithTimeout(r.Context(), aiPingTimeout)
	defer cancel()
	response := &apiAIStatusResponse{
		Enabled:  true,
		Provider: s.ai.ProviderName(),
		Model:    s.ai.DefaultModel(),
		Usage:    s.ai.Report(""),
	}
	if err := s.ai.Ping(ctx); err != nil {
		response.Health = err.Error()
	} else {
		response.Healthy = true
	}
	log.Tag(tagAI).Field("provider", response.Provider).Field("healthy", response.Healthy).Debug("AI status requested")
	return s.writeJSON(w, response)
}

// handleAIUsage reports today's token usage for the requesting visitor (GET /v1/ai/usage).
// It is intentionally available to anonymous visitors: AI features are rate-limited per
// visitor, so unauthenticated clients can have usage too.
func (s *Server) handleAIUsage(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return s.writeJSON(w, s.ai.Report(visitorID(v.ip, v.user, v.config)))
}

const (
	// aiEnrichMinMessageLen skips trivially short messages: there is nothing to summarize.
	aiEnrichMinMessageLen = 120
)

// maybeEnrichMessage runs inline AI enrichment (summary -> title, importance ->
// priority if the publisher did not set one) for messages on topics the operator opted
// in via ai-enrich-topics. It is called on the publish path before dispatch, so:
//   - enrichment is bounded by ai-inline-timeout; on breach or any error the message
//     passes through unchanged (instant delivery is ntfy's core promise), and
//   - the enriched message is what gets cached, so every subscriber and poller sees
//     the same enrichment and the provider is paid once per message.
func (s *Server) maybeEnrichMessage(m *model.Message) {
	if !s.ai.Enabled() || !s.config.AIEnrichmentEnabled {
		return
	}
	if !slices.Contains(s.config.AIEnrichTopics, m.Topic) {
		return
	}
	if len(m.Message) < aiEnrichMinMessageLen || m.Title != "" {
		return // Nothing to summarize, or the publisher already chose a title
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.config.AIInlineTimeout)
	defer cancel()
	enrichment, err := ai.NewEnricher(s.ai).Enrich(ctx, m.Topic, m.Title, m.Message)
	if err != nil {
		log.Tag(tagAI).Err(err).Field("topic", m.Topic).Field("message_id", m.ID).Debug("AI enrichment skipped, passing through")
		metrics.AIEnrichmentPassThrough.Inc()
		return
	}
	if m.Priority == 0 && enrichment.Priority > 0 {
		m.Priority = enrichment.Priority // Never override an explicit publisher priority
	}
	m.Title = enrichment.Summary
	metrics.AIEnrichmentApplied.Inc()
	log.Tag(tagAI).Field("topic", m.Topic).Field("message_id", m.ID).Field("ai_feature", string(ai.FeatureEnrich)).Debug("AI enrichment applied")
}

// apiAIPlanRequest is the body of POST /v1/ai/plan. Context optionally carries prior
// turns of the planning conversation (user/assistant alternating) so the web app can
// refine a plan; each turn is sanitized and hard-capped server-side.
type apiAIPlanRequest struct {
	Prompt  string             `json:"prompt"`
	Locale  string             `json:"locale"`
	Context []apiAIPlanContext `json:"context"`
}

type apiAIPlanContext struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleAIPlan turns a natural-language wish into a reviewable subscription plan
// (POST /v1/ai/plan). The plan is NEVER applied server-side — the web app applies it
// through the existing subscription endpoints after the user reviews and confirms.
// Available to anonymous visitors (rate-limited per visitor), because subscribing to a
// public topic needs no account.
func (s *Server) handleAIPlan(w http.ResponseWriter, r *http.Request, v *visitor) error {
	if s.config.BaseURL == "" {
		return errHTTPInternalErrorMissingBaseURL
	}
	body, err := readJSONWithLimit[apiAIPlanRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	}
	if ai.ValidatePrompt(body.Prompt) != nil {
		return errHTTPBadRequestAIRequest // invalid requests do not consume the daily quota
	}
	if !v.aiQuotaAllowed() {
		return errHTTPTooManyRequestsLimitAIRequests
	}
	existingTopics := visitorTopics(v)
	planner := ai.NewPlanner(s.ai)
	plan, err := planner.Plan(r.Context(), visitorID(v.ip, v.user, v.config), s.config.BaseURL, body.Prompt, body.Locale, existingTopics, sanitizePlanContext(body.Context))
	if err != nil {
		return s.mapAIError(err)
	}
	log.Tag(tagAI).With(v).Fields(log.Context{
		"ai_feature":     string(ai.FeaturePlan),
		"ai_plan_topics": len(plan.Subscriptions),
	}).Debug("AI plan generated")
	return s.writeJSON(w, plan)
}

// apiAITuneRequest is the body of POST /v1/ai/tune.
type apiAITuneRequest struct {
	Topic       string `json:"topic"`
	DisplayName string `json:"display_name"`
	Search      string `json:"search"`
	MinPriority int    `json:"min_priority"`
	Goal        string `json:"goal"`
}

// handleAITune proposes adjustments to an existing subscription (POST /v1/ai/tune).
// Requires a user account: anonymous subscriptions are local to the browser, so the
// server has nothing to tune against.
func (s *Server) handleAITune(w http.ResponseWriter, r *http.Request, v *visitor) error {
	body, err := readJSONWithLimit[apiAITuneRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	}
	if !topicRegex.MatchString(body.Topic) || ai.ValidatePrompt(body.Goal) != nil {
		return errHTTPBadRequestAIRequest // invalid requests do not consume the daily quota
	}
	if !v.aiQuotaAllowed() {
		return errHTTPTooManyRequestsLimitAIRequests
	}
	planner := ai.NewPlanner(s.ai)
	tune, err := planner.Tune(r.Context(), visitorID(v.ip, v.user, v.config), &ai.TuneInput{
		Topic:       body.Topic,
		DisplayName: body.DisplayName,
		Search:      body.Search,
		MinPriority: body.MinPriority,
	}, body.Goal)
	if err != nil {
		return s.mapAIError(err)
	}
	log.Tag(tagAI).With(v).Field("ai_feature", string(ai.FeatureTune)).Debug("AI tune generated")
	return s.writeJSON(w, tune)
}

// mapAIError translates ai package sentinel errors into the matching HTTP errors.
func (s *Server) mapAIError(err error) error {
	if errors.Is(err, ai.ErrBudgetExceeded) {
		return errHTTPTooManyRequestsLimitAIRequests
	}
	if errors.Is(err, ai.ErrDisabled) {
		return errHTTPBadRequestAIDisabled
	}
	if errors.Is(err, ai.ErrInvalidResponse) {
		return errHTTPInternalError // provider misbehavior is not user error
	}
	return errHTTPBadRequestAIRequest
}

// sanitizePlanContext bounds and sanitizes the client-supplied conversation turns:
// at most 6 turns, each at most PlannerMaxPromptChars, and only user/assistant roles.
// Everything else is dropped — context is untrusted input.
func sanitizePlanContext(context []apiAIPlanContext) (turns []ai.Message) {
	for i, turn := range context {
		if i >= 6 {
			break
		}
		role := ai.Role(turn.Role)
		if role != ai.RoleUser && role != ai.RoleAssistant {
			continue
		}
		content := turn.Content
		if len(content) > ai.PlannerMaxPromptChars {
			content = content[:ai.PlannerMaxPromptChars]
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		turns = append(turns, ai.Message{Role: role, Content: content})
	}
	return turns
}

// apiAIDigestRequest is the body of POST /v1/ai/digest.
type apiAIDigestRequest struct {
	Topic string `json:"topic"`
	Since string `json:"since"` // Duration like "24h" or "7d"; default 24h, max 30d
}

// handleAIDigest summarizes recent messages of one of the user's subscribed topics
// (POST /v1/ai/digest). Requires an account: only synced subscriptions can be digested.
func (s *Server) handleAIDigest(w http.ResponseWriter, r *http.Request, v *visitor) error {
	body, err := readJSONWithLimit[apiAIDigestRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	}
	if !topicRegex.MatchString(body.Topic) {
		return errHTTPBadRequestAIRequest
	}
	u := v.User()
	if !userSubscribesTo(u, s.config.BaseURL, body.Topic) {
		return errHTTPForbidden // Only your own subscriptions, keeps the quota honest
	}
	if !v.aiQuotaAllowed() {
		return errHTTPTooManyRequestsLimitAIRequests
	}
	sinceDuration := 24 * time.Hour
	if body.Since != "" {
		parsed, err := time.ParseDuration(body.Since)
		if err != nil || parsed < time.Hour || parsed > 30*24*time.Hour {
			return errHTTPBadRequestAIRequest
		}
		sinceDuration = parsed
	}
	sinceMarker := model.NewSinceTime(time.Now().Add(-1 * sinceDuration).Unix())
	cachedMessages, _, err := s.messageCache.MessagesCapped(body.Topic, sinceMarker, false, 512*1024)
	if err != nil {
		return err
	}
	digestMessages := make([]ai.DigestMessage, 0, len(cachedMessages))
	for _, m := range cachedMessages {
		digestMessages = append(digestMessages, ai.DigestMessage{
			ID: m.ID, Title: m.Title, Message: m.Message, Priority: m.Priority, Time: m.Time,
		})
	}
	log.Tag(tagAI).With(v).Fields(log.Context{
		"ai_feature":      string(ai.FeatureDigest),
		"ai_digest_topic": body.Topic,
		"ai_digest_count": len(digestMessages),
	}).Debug("AI digest requested")
	if len(digestMessages) == 0 {
		// Nothing in the window: answer without spending provider budget
		return s.writeJSON(w, &ai.DigestResult{
			Topic:      body.Topic,
			Headline:   "No messages in this period.",
			Disclaimer: ai.DigestDisclaimer,
		})
	}
	digest, err := ai.NewDigester(s.ai).Digest(r.Context(), visitorID(v.ip, v.user, v.config), &ai.DigestInput{
		Topic: body.Topic, Messages: digestMessages,
	})
	if err != nil {
		return s.mapAIError(err)
	}
	return s.writeJSON(w, digest)
}

// userSubscribesTo reports whether the user has a synced subscription for the topic.
func userSubscribesTo(u *user.User, baseURL, topic string) bool {
	if u == nil || u.Prefs == nil {
		return false
	}
	for _, sub := range u.Prefs.Subscriptions {
		if sub.Topic == topic && (sub.BaseURL == baseURL || sub.BaseURL == "") {
			return true
		}
	}
	return false
}

// visitorTopics returns the topics of the visitor's synced subscriptions (empty for
// anonymous visitors), used as context for the planner.
func visitorTopics(v *visitor) []string {
	u := v.User()
	if u == nil || u.Prefs == nil {
		return nil
	}
	topics := make([]string, 0, len(u.Prefs.Subscriptions))
	for _, sub := range u.Prefs.Subscriptions {
		if sub.Topic != "" {
			topics = append(topics, sub.Topic)
		}
	}
	return topics
}
