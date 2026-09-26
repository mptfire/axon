package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/metrics"
	"heckel.io/ntfy/v2/model"
	"heckel.io/ntfy/v2/user"
)

// aiPingTimeout bounds the provider health check in the status endpoint.
const aiPingTimeout = 2 * time.Second

// aiChatMaxTopics caps how many topics one cross-topic chat may draw from.
const aiChatMaxTopics = 12

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

// ensureAIScope rejects requests authenticated with a token that does not carry the
// "ai" scope (user.TokenScopes). Anonymous visitors and scope-less tokens pass — scopes
// are a restriction for scoped agent tokens only. Applied to the LLM-costing endpoints
// (plan, tune, digest, chat), not to status/usage.
func (s *Server) ensureAIScope(next handleFunc) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, v *visitor) error {
		if u := v.User(); u != nil && !u.HasTokenScope(user.TokenScopeAI) {
			log.Tag(tagAI).With(v).Warn("Token does not carry the ai scope, rejecting request")
			return errHTTPForbidden
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
	translateTo := ""
	if slices.Contains(s.config.AITranslateTopics, m.Topic) {
		translateTo = s.config.AITranslateLang
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.config.AIInlineTimeout)
	defer cancel()
	enrichment, err := ai.NewEnricher(s.ai).Enrich(ctx, &ai.EnrichRequest{Topic: m.Topic, Title: m.Title, Message: m.Message, TranslateTo: translateTo})
	if err != nil {
		log.Tag(tagAI).Err(err).Field("topic", m.Topic).Field("message_id", m.ID).Debug("AI enrichment skipped, passing through")
		metrics.AIEnrichmentPassThrough.Inc()
		return
	}
	if m.Priority == 0 && enrichment.Priority > 0 {
		m.Priority = enrichment.Priority // Never override an explicit publisher priority
	}
	m.Title = enrichment.Summary
	if enrichment.Translation != "" {
		// Translated body first, original preserved below
		m.Message = enrichment.Translation + "\n\n" + m.Message
	}
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
	digester := ai.NewDigester(s.ai)
	if s.embedder != nil {
		digester = ai.NewDigesterWithEmbedder(s.ai, s.embedder) // axon: semantic pre-clustering
	}
	digest, err := digester.Digest(r.Context(), visitorID(v.ip, v.user, v.config), &ai.DigestInput{
		Topic: body.Topic, Messages: digestMessages,
	})
	if err != nil {
		return s.mapAIError(err)
	}
	return s.writeJSON(w, digest)
}

// apiAIChatRequest is the body of POST /v1/ai/chat. History carries prior question/
// answer pairs for follow-up questions; it is sanitized and hard-capped.
type apiAIChatRequest struct {
	Topic    string             `json:"topic"` // Used when All is false
	All      bool               `json:"all"`   // axon: ask across ALL synced subscriptions on this server
	Question string             `json:"question"`
	Since    string             `json:"since"` // Retrieval window; default 7d, max 30d
	History  []apiAIChatHistory `json:"history"`
}

type apiAIChatHistory struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// apiAIChatResponse is the response of POST /v1/ai/chat: the model's answer with
// citations resolved back to the actual cached messages.
type apiAIChatResponse struct {
	Topic      string           `json:"topic"`
	Question   string           `json:"question"`
	Answer     string           `json:"answer"`
	Citations  []*model.Message `json:"citations"`
	Disclaimer string           `json:"disclaimer"`
}

// handleAIChat answers a question about one of the user's topics or across all of them
// (POST /v1/ai/chat). Non-streaming: the full answer comes back as JSON with citations.
func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request, v *visitor) error {
	body, err := readJSONWithLimit[apiAIChatRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	}
	question, context, messageTopics, err := s.chatContext(v, body)
	if err != nil {
		return err
	}
	if len(context) == 0 {
		return s.writeJSON(w, s.emptyChatResponse(body, question))
	}
	context = selectChatContext(r.Context(), question, context, s.embedder)
	log.Tag(tagAI).With(v).Fields(log.Context{
		"ai_feature":      string(ai.FeatureChat),
		"ai_chat_topic":   body.Topic,
		"ai_chat_all":     body.All,
		"ai_chat_context": len(context),
	}).Debug("AI chat requested")
	chatMessages := make([]ai.DigestMessage, 0, len(context))
	for _, m := range context {
		chatMessages = append(chatMessages, ai.DigestMessage{ID: m.ID, Topic: messageTopics[m.ID], Title: m.Title, Message: m.Message, Priority: m.Priority, Time: m.Time})
	}
	answer, err := ai.NewChatter(s.ai).Chat(r.Context(), visitorID(v.ip, v.user, v.config), body.Topic, question, chatMessages, sanitizeChatHistory(body.History))
	if err != nil {
		return s.mapAIError(err)
	}
	response := &apiAIChatResponse{
		Topic: body.Topic, Question: question, Answer: answer.Answer,
		Citations: make([]*model.Message, 0, len(answer.Citations)), Disclaimer: ai.ChatDisclaimer,
	}
	byID := make(map[string]*model.Message, len(context))
	for _, m := range context {
		byID[m.ID] = m
	}
	for _, id := range answer.Citations {
		if m, ok := byID[id]; ok {
			response.Citations = append(response.Citations, m)
		}
	}
	return s.writeJSON(w, response)
}

// handleAIChatStream is the SSE variant of handleAIChat (POST /v1/ai/chat/stream):
// delta events as the answer streams in, then a citations event (markers like [1] in
// the text refer to the numbered context and are resolved server-side), then done.
// Providers without streaming support degrade to a single-delta response.
func (s *Server) handleAIChatStream(w http.ResponseWriter, r *http.Request, v *visitor) error {
	body, err := readJSONWithLimit[apiAIChatRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	}
	question, context, messageTopics, err := s.chatContext(v, body)
	if err != nil {
		return err
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errHTTPInternalError
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	writeEvent := func(payload map[string]any) error {
		serialized, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := w.Write(append([]byte("data: "), append(serialized, '\n', '\n')...)); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if len(context) == 0 {
		_ = writeEvent(map[string]any{"type": "delta", "text": "There are no messages in this topic for the selected period."})
		return writeEvent(map[string]any{"type": "done"})
	}
	context = selectChatContext(r.Context(), question, context, s.embedder)
	chatMessages := make([]ai.DigestMessage, 0, len(context))
	for _, m := range context {
		chatMessages = append(chatMessages, ai.DigestMessage{ID: m.ID, Topic: messageTopics[m.ID], Title: m.Title, Message: m.Message, Priority: m.Priority, Time: m.Time})
	}
	var full strings.Builder
	appendCitations := func() error {
		answerText, citedIDs := ai.ExtractChatCitations(full.String(), chatMessages)
		citations := make([]*model.Message, 0, len(citedIDs))
		for _, id := range citedIDs {
			for _, m := range context {
				if m.ID == id {
					citations = append(citations, m)
					break
				}
			}
		}
		return writeEvent(map[string]any{"type": "citations", "text": answerText, "citations": citations})
	}
	events, err := ai.NewChatter(s.ai).ChatStream(r.Context(), visitorID(v.ip, v.user, v.config), body.Topic, question, chatMessages, sanitizeChatHistory(body.History))
	if err != nil {
		if !errors.Is(err, ai.ErrStreamingNotSupported) {
			return s.mapAIError(err)
		}
		// Graceful degradation: provider cannot stream, answer in one shot
		answer, err := ai.NewChatter(s.ai).Chat(r.Context(), visitorID(v.ip, v.user, v.config), body.Topic, question, chatMessages, sanitizeChatHistory(body.History))
		if err != nil {
			return s.mapAIError(err)
		}
		full.WriteString(answer.Answer)
		if err := writeEvent(map[string]any{"type": "delta", "text": answer.Answer}); err != nil {
			return err
		}
		if err := appendCitations(); err != nil {
			return err
		}
		return writeEvent(map[string]any{"type": "done"})
	}
	for event := range events {
		if event.Err != nil {
			return s.mapAIError(event.Err)
		}
		if event.Delta == "" {
			continue
		}
		full.WriteString(event.Delta)
		if err := writeEvent(map[string]any{"type": "delta", "text": event.Delta}); err != nil {
			return err
		}
	}
	if err := appendCitations(); err != nil {
		return err
	}
	return writeEvent(map[string]any{"type": "done"})
}

// chatContext performs the shared gates and retrieval for the chat endpoints: auth is
// enforced by the route decorators; this enforces the request shape and the
// subscription rule, and gathers the context window (single topic or cross-topic).
// A nil context return means "no messages in the window" (not an error).
func (s *Server) chatContext(v *visitor, body *apiAIChatRequest) (string, []*model.Message, map[string]string, error) {
	question := strings.TrimSpace(body.Question)
	if question == "" || len(question) > 1000 || (body.All && body.Topic != "") {
		return "", nil, nil, errHTTPBadRequestAIRequest
	}
	if !body.All && !topicRegex.MatchString(body.Topic) {
		return "", nil, nil, errHTTPBadRequestAIRequest
	}
	u := v.User()
	if !body.All && !userSubscribesTo(u, s.config.BaseURL, body.Topic) {
		return "", nil, nil, errHTTPForbidden
	}
	if !v.aiQuotaAllowed() {
		return "", nil, nil, errHTTPTooManyRequestsLimitAIRequests
	}
	sinceDuration := 7 * 24 * time.Hour
	if body.Since != "" {
		parsed, err := time.ParseDuration(body.Since)
		if err != nil || parsed < time.Hour || parsed > 30*24*time.Hour {
			return "", nil, nil, errHTTPBadRequestAIRequest
		}
		sinceDuration = parsed
	}
	sinceMarker := model.NewSinceTime(time.Now().Add(-1 * sinceDuration).Unix())
	messageTopics := map[string]string{} // axon: message id -> topic (cross-topic mode)
	var cachedMessages []*model.Message
	if body.All {
		if u.Prefs != nil {
			topicsSeen := 0
			for _, sub := range u.Prefs.Subscriptions {
				if topicsSeen >= aiChatMaxTopics {
					break
				}
				if sub.Topic == "" || (sub.BaseURL != "" && sub.BaseURL != s.config.BaseURL) {
					continue
				}
				messages, _, err := s.messageCache.MessagesCapped(sub.Topic, sinceMarker, false, 128*1024)
				if err != nil || len(messages) == 0 {
					continue // A broken topic must not break the chat
				}
				topicsSeen++
				for _, m := range messages {
					messageTopics[m.ID] = sub.Topic
					cachedMessages = append(cachedMessages, m)
				}
			}
		}
	} else {
		var err error
		cachedMessages, _, err = s.messageCache.MessagesCapped(body.Topic, sinceMarker, false, 512*1024)
		if err != nil {
			return "", nil, nil, err
		}
	}
	return question, cachedMessages, messageTopics, nil
}

// emptyChatResponse is the shared "nothing in the window" answer.
func (s *Server) emptyChatResponse(body *apiAIChatRequest, question string) *apiAIChatResponse {
	return &apiAIChatResponse{
		Topic: body.Topic, Question: question,
		Answer:     "There are no messages in this topic for the selected period.",
		Disclaimer: ai.ChatDisclaimer,
	}
}

// toDigestMessages converts cached messages to the ai package's context type.
func toDigestMessages(messages []*model.Message) []ai.DigestMessage {
	out := make([]ai.DigestMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, ai.DigestMessage{ID: m.ID, Title: m.Title, Message: m.Message, Priority: m.Priority, Time: m.Time})
	}
	return out
}

// selectChatContext picks the messages most likely to answer the question. With an
// embedder, retrieval is hybrid: 0.6 * cosine(question, message) + 0.4 * normalized
// term overlap. Without one, it falls back to term overlap and recency only.
// Deterministic and cheap; embeddings are batched and cached per text.
func selectChatContext(ctx context.Context, question string, messages []*model.Message, embedder *ai.Embedder) []*model.Message {
	const k = 60
	tokens := questionTokens(question)
	scores := make(map[string]float64, len(messages)) // id -> score
	for _, m := range messages {
		score := 0.0
		title := strings.ToLower(m.Title)
		body := strings.ToLower(m.Message)
		for _, token := range tokens {
			if strings.Contains(title, token) {
				score += 2
			}
			if strings.Contains(body, token) {
				score++
			}
		}
		scores[m.ID] = score
	}
	sort.SliceStable(messages, func(i, j int) bool {
		si, sj := scores[messages[i].ID], scores[messages[j].ID]
		if si != sj {
			return si > sj // Highest term overlap first
		}
		return messages[i].Time > messages[j].Time // Then newest
	})
	if len(messages) > k {
		messages = messages[:k]
	}
	if embedder == nil {
		// Keep chronological order for the model's readability
		sort.SliceStable(messages, func(i, j int) bool { return messages[i].Time < messages[j].Time })
		return messages
	}
	return hybridReorder(ctx, question, messages, scores, embedder)
}

// hybridReorder re-ranks the top candidates by blending cosine(question, message) with
// the term-overlap score. Embedding failures degrade silently to the term order.
func hybridReorder(ctx context.Context, question string, messages []*model.Message, scores map[string]float64, embedder *ai.Embedder) []*model.Message {
	texts := make([]string, 0, len(messages)+1)
	maxTerm := 0.0
	for _, m := range messages {
		texts = append(texts, m.Title+"\n"+m.Message)
		if scores[m.ID] > maxTerm {
			maxTerm = scores[m.ID]
		}
	}
	texts = append(texts, question)
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		log.Tag(tagAI).Err(err).Debug("Embedding failed, keeping keyword order")
		return messages
	}
	qvec := vectors[len(vectors)-1]
	cosines := make(map[string]float64, len(messages))
	for i, m := range messages {
		cosines[m.ID] = ai.Cosine(qvec, vectors[i])
	}
	sort.SliceStable(messages, func(i, j int) bool {
		hi, hj := hybridScore(cosines[messages[i].ID], scores[messages[i].ID], maxTerm), hybridScore(cosines[messages[j].ID], scores[messages[j].ID], maxTerm)
		if hi != hj {
			return hi > hj
		}
		return messages[i].Time > messages[j].Time
	})
	return messages
}

// hybridScore blends normalized cosine similarity with the term-overlap score.
func hybridScore(cosine, termScore, maxTerm float64) float64 {
	if maxTerm <= 0 {
		return cosine
	}
	return 0.6*cosine + 0.4*(termScore/maxTerm)
}

// questionTokens lowercases and splits the question, dropping short noise words.
func questionTokens(question string) []string {
	fields := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) >= 3 {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

// apiAIBriefingRequest is the body of POST /v1/ai/briefing.
type apiAIBriefingRequest struct {
	Since string `json:"since"` // Duration like "24h" or "7d"; default 24h, max 30d
}

// handleAIBriefing summarizes recent messages across ALL of the user's subscriptions on
// this server (POST /v1/ai/briefing) — the "what did I miss?" view. Requires an account.
func (s *Server) handleAIBriefing(w http.ResponseWriter, r *http.Request, v *visitor) error {
	body, err := readJSONWithLimit[apiAIBriefingRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
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
	u := v.User()
	topics := make([]ai.BriefingTopicMessages, 0, len(u.Prefs.Subscriptions))
	totalMessages := 0
	if u.Prefs != nil {
		for _, sub := range u.Prefs.Subscriptions {
			if len(topics) >= ai.BriefingMaxTopics {
				break // Cost cap: a briefing covers at most BriefingMaxTopics topics
			}
			if sub.Topic == "" || (sub.BaseURL != "" && sub.BaseURL != s.config.BaseURL) {
				continue // Other-server subscriptions are not ours to read
			}
			cachedMessages, _, err := s.messageCache.MessagesCapped(sub.Topic, sinceMarker, false, 128*1024)
			if err != nil {
				return err
			}
			if len(cachedMessages) == 0 {
				continue
			}
			topics = append(topics, ai.BriefingTopicMessages{Topic: sub.Topic, Messages: toDigestMessages(cachedMessages)})
			totalMessages += len(cachedMessages)
		}
	}
	log.Tag(tagAI).With(v).Fields(log.Context{
		"ai_feature":         string(ai.FeatureDigest),
		"ai_briefing_topics": len(topics),
		"ai_briefing_count":  totalMessages,
	}).Debug("AI briefing requested")
	if totalMessages == 0 {
		// Nothing in the window: answer without spending provider budget
		return s.writeJSON(w, &ai.BriefingResult{
			Headline:   "No messages in this period.",
			Disclaimer: ai.BriefingDisclaimer,
		})
	}
	briefing, err := ai.NewBriefinger(s.ai).Briefing(r.Context(), visitorID(v.ip, v.user, v.config), &ai.BriefingInput{Topics: topics})
	if err != nil {
		return s.mapAIError(err)
	}
	return s.writeJSON(w, briefing)
}

// sanitizeChatHistory caps the client-supplied conversation history: at most 5 turns,
// each question/answer trimmed and length-capped. History is untrusted input.
func sanitizeChatHistory(history []apiAIChatHistory) (turns []ai.ChatTurn) {
	for i, turn := range history {
		if i >= 5 {
			break
		}
		question, answer := strings.TrimSpace(turn.Question), strings.TrimSpace(turn.Answer)
		if question == "" || answer == "" {
			continue
		}
		if len(question) > 1000 {
			question = question[:1000]
		}
		if len(answer) > ai.ChatMaxAnswerChars {
			answer = answer[:ai.ChatMaxAnswerChars]
		}
		turns = append(turns, ai.ChatTurn{Question: question, Answer: answer})
	}
	return turns
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
