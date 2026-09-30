package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// topicNameRegex mirrors the server's topic validation. Enforced client-side because the
// topic goes into the URL path — no exceptions.
var topicNameRegex = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

const maxReadMessages = 50

// toolDefinition describes one tool for tools/list.
type toolDefinition struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	InputSchema *inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]propertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

type propertySchema struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Items       *propertySchema `json:"items,omitempty"`
	Minimum     *float64        `json:"minimum,omitempty"`
	Maximum     *float64        `json:"maximum,omitempty"`
}

// toolDefinitions returns the tool catalog.
func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "publish",
			Description: "Publish a push notification to a topic on the ntfy server. Every subscriber (phones, web app, other agents) is notified instantly.",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"topic":    {Type: "string", Description: "Topic name (1-64 chars: letters, digits, '-', '_')"},
					"message":  {Type: "string", Description: "Message body"},
					"title":    {Type: "string", Description: "Optional title"},
					"priority": {Type: "integer", Description: "Optional priority 1 (min) to 5 (max/urgent)"},
					"tags":     {Type: "array", Description: "Optional tags/emoji shortcodes", Items: &propertySchema{Type: "string"}},
					"delay":    {Type: "string", Description: `Optional scheduled delivery, e.g. "30s", "9am" or RFC 3339`},
				},
				Required: []string{"topic", "message"},
			},
		},
		{
			Name:        "read_messages",
			Description: "Read recent cached messages from a topic (history replay, does not wait).",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"topic": {Type: "string", Description: "Topic name"},
					"since": {Type: "string", Description: `Replay window: "all", a duration like "12h", or a unix timestamp. Default "all".`},
					"limit": {Type: "integer", Description: "Max messages to return (default 20, max 50)"},
				},
				Required: []string{"topic"},
			},
		},
		{
			Name:        "subscribe_wait",
			Description: "Wait for the next live message on a topic and return it (long poll). Use to await replies or events. Returns a timeout message if nothing arrives.",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"topic":        {Type: "string", Description: "Topic name"},
					"timeout_secs": {Type: "integer", Description: "How long to wait (default 60, max 300)"},
				},
				Required: []string{"topic"},
			},
		},
		{
			Name:        "ask_history",
			Description: "Ask a question about recent notification history (one topic, or all the account's topics) and get an answer. AI layer required on the server.",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"question": {Type: "string", Description: "The question, e.g. \"what failed last night?\""},
					"topic":    {Type: "string", Description: "Topic name. Omit to search across all the account's topics."},
					"since":    {Type: "string", Description: `History window: "168h" (default), "720h", ... (max 30 days)`},
				},
				Required: []string{"question"},
			},
		},
		{
			Name:        "digest_topic",
			Description: "Summarize the recent messages of a topic into a short briefing (AI layer required on the server).",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"topic": {Type: "string", Description: "Topic name"},
					"since": {Type: "string", Description: `Summary window: "24h" (default), "168h", "720h", ... (max 30 days)`},
				},
				Required: []string{"topic"},
			},
		},
		{
			Name:        "briefing",
			Description: "Summarize recent messages across ALL of the account's topics on this server into one briefing (AI layer required; token required).",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"since": {Type: "string", Description: `Summary window: "24h" (default), "168h", "720h", ... (max 30 days)`},
				},
			},
		},
		{
			Name:        "list_subscriptions",
			Description: "List the account's synced topic subscriptions. Requires an access token (--token).",
			InputSchema: &inputSchema{Type: "object", Properties: map[string]propertySchema{}},
		},
		{
			Name:        "plan_subscription",
			Description: "Turn a natural-language wish into a proposed topic subscription plan (topics, filters, priorities). Plan only — applying it is a separate human decision. Requires the server's AI layer.",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"prompt": {Type: "string", Description: "What should the user be notified about?"},
					"locale": {Type: "string", Description: "Language for the plan, e.g. \"en\""},
				},
				Required: []string{"prompt"},
			},
		},
		{
			Name:        "set_device_config",
			Description: "Write a paired device's agent-channel config: subscriptions [{topic, muted, min_priority, auto_delete_seconds, insistent, display_name}], and manage (\"full\" lets removals apply). The app applies it on sync (max 15 min). Requires an access token.",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"device_id": {Type: "string", Description: "Device ID (dv_..., from device_status)"},
					"config":    {Type: "object", Description: "Full config JSON object; replaces the previous config"},
				},
				Required: []string{"device_id", "config"},
			},
		},
		{
			Name:        "device_status",
			Description: "List the account's paired devices: id, label, created/updated, last sync (last_seen). Requires an access token.",
			InputSchema: &inputSchema{Type: "object", Properties: map[string]propertySchema{}},
		},
		{
			Name:        "request_pairing",
			Description: "Mint a one-time pairing code so the user can pair their phone's axon app with a device-scoped token (tap axon://pair/<code> within 5 minutes). The code grants nothing by itself — the human tap on the phone is the consent step.",
			InputSchema: &inputSchema{
				Type: "object",
				Properties: map[string]propertySchema{
					"label": {Type: "string", Description: "Optional device label, e.g. \"Pixel 9\""},
				},
			},
		},
	}
}

// callToolParams / errors
var (
	errTopicRequired = fmt.Errorf("argument 'topic' is required (1-64 chars: letters, digits, '-', '_')")
	errNoToken       = fmt.Errorf("this tool requires an access token: restart with --token tk_<value>")
)

func validTopic(topic string) bool { return topicNameRegex.MatchString(topic) }

// validDeviceID matches the server's device ID shape (dv_ + base62).
var deviceIDRegex = regexp.MustCompile(`^dv_[A-Za-z0-9]{4,32}$`)

func validDeviceID(id string) bool { return deviceIDRegex.MatchString(id) }

// validSince accepts the documented since= forms: "all", a Go-style duration with
// d/h/m/s units, a unix timestamp, or a message ID. Anything else (including
// URL metacharacters) is rejected before it reaches the inner request path
// (audit: unescaped since could alter the inner request's query).
var sinceRegex = regexp.MustCompile(`^(all|\d{1,13}|\d+(\.\d+)?(ms|s|m|h|d)|[A-Za-z0-9_-]{4,64})$`)

func validSince(since string) bool { return sinceRegex.MatchString(since) }

// do performs a request against the configured ntfy service. The auth value is the
// caller's raw Authorization header (HTTP transport) or empty (stdio transport, which
// falls back to the server-wide configured access token).
func (s *Server) do(ctx context.Context, method, path string, body io.Reader, header http.Header) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(s.config.ServiceBaseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range header {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	// Attribute inner self-calls to the real caller (audit F-B): without this,
	// every MCP user shares the server/proxy rate-limit bucket and ban strikes.
	if ip := callerIPFrom(ctx); ip != "" {
		httpReq.Header.Set("X-Forwarded-For", ip)
	}
	// HTTP transport: caller's own credentials via context value; stdio: config token
	switch {
	case authTokenFrom(ctx) != "":
		httpReq.Header.Set("Authorization", authTokenFrom(ctx))
	case s.config.AccessToken != "":
		httpReq.Header.Set("Authorization", "Bearer "+s.config.AccessToken)
	}
	return s.client.Do(httpReq)
}

// toolPublish publishes a message via PUT /{topic}.
func (s *Server) toolPublish(ctx context.Context, args map[string]any) *toolResult {
	topic, _ := args["topic"].(string)
	message, _ := args["message"].(string)
	if !validTopic(topic) {
		return errorResult(errTopicRequired)
	}
	if strings.TrimSpace(message) == "" {
		return errorResult(fmt.Errorf("argument 'message' is required"))
	}
	header := http.Header{}
	if title, _ := args["title"].(string); title != "" {
		header.Set("X-Title", title)
	}
	if priority := intArg(args, "priority"); priority > 0 {
		header.Set("X-Priority", strconv.Itoa(priority))
	}
	if tags := stringSliceArg(args, "tags"); len(tags) > 0 {
		header.Set("X-Tags", strings.Join(tags, ","))
	}
	if delay, _ := args["delay"].(string); delay != "" {
		header.Set("X-Delay", delay)
	}
	resp, err := s.do(ctx, http.MethodPut, "/"+topic, strings.NewReader(message), header)
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	var published struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&published)
	return textResult(fmt.Sprintf("Message published to topic %q (id: %s)", topic, published.ID))
}

// toolReadMessages replays cached messages via GET /{topic}/json?poll=1&since=...
func (s *Server) toolReadMessages(ctx context.Context, args map[string]any) *toolResult {
	topic, _ := args["topic"].(string)
	if !validTopic(topic) {
		return errorResult(errTopicRequired)
	}
	since, _ := args["since"].(string)
	if since == "" {
		since = "all"
	}
	limit := intArg(args, "limit")
	if limit <= 0 {
		limit = 20
	}
	if limit > maxReadMessages {
		limit = maxReadMessages
	}
	if !validSince(since) {
		return errorResult(fmt.Errorf("invalid since: must be \"all\", a duration (e.g. 12h), a unix timestamp, or a message ID"))
	}
	resp, err := s.do(ctx, http.MethodGet, "/"+topic+"/json?poll=1&since="+url.QueryEscape(since), nil, nil)
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	messages := make([]map[string]any, 0, limit)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var generic map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &generic); err != nil {
			continue
		}
		if id, _ := generic["id"].(string); id == "" {
			continue // open/keepalive events have no id
		}
		messages = append(messages, generic)
		if len(messages) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return errorResult(err)
	}
	serialized, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return errorResult(err)
	}
	return textResult(fmt.Sprintf("%d message(s) on topic %q:\n%s", len(messages), topic, string(serialized)))
}

// toolSubscribeWait waits for the next live message on a topic.
func (s *Server) toolSubscribeWait(ctx context.Context, args map[string]any) *toolResult {
	topic, _ := args["topic"].(string)
	if !validTopic(topic) {
		return errorResult(errTopicRequired)
	}
	wait := time.Duration(intArg(args, "timeout_secs")) * time.Second
	if wait <= 0 {
		wait = DefaultWaitTimeout
	}
	if wait > s.config.WaitMax {
		wait = s.config.WaitMax
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	// Live-only stream (no since=): the first message event is the next message.
	resp, err := s.do(waitCtx, http.MethodGet, "/"+topic+"/json", nil, nil)
	if err != nil {
		if waitCtx.Err() != nil || ctx.Err() != nil {
			return textResult(fmt.Sprintf("timeout: no message arrived on topic %q within %s", topic, wait))
		}
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var generic map[string]any
		if err := json.Unmarshal(line, &generic); err != nil {
			continue
		}
		if eventID, _ := generic["id"].(string); eventID == "" {
			continue // open/keepalive events have no id
		}
		return textResult("Next message on topic " + strconv.Quote(topic) + ":\n" + string(line))
	}
	if err := scanner.Err(); err != nil {
		if waitCtx.Err() != nil {
			return textResult(fmt.Sprintf("timeout: no message arrived on topic %q within %s", topic, wait))
		}
		return errorResult(err)
	}
	return textResult(fmt.Sprintf("timeout: no message arrived on topic %q within %s", topic, wait))
}

// toolAskHistory asks a question over notification history via the server's AI layer.
func (s *Server) toolAskHistory(ctx context.Context, args map[string]any) *toolResult {
	question, _ := args["question"].(string)
	if strings.TrimSpace(question) == "" {
		return errorResult(fmt.Errorf("argument 'question' is required"))
	}
	topic, _ := args["topic"].(string)
	if topic != "" && !validTopic(topic) {
		return errorResult(errTopicRequired)
	}
	if topic == "" && authTokenFrom(ctx) == "" && s.config.AccessToken == "" {
		return errorResult(errNoToken) // cross-topic mode is account-scoped
	}
	since, _ := args["since"].(string)
	if since == "" {
		since = "168h"
	}
	payload := map[string]any{"question": question, "since": since, "all": topic == ""}
	if topic != "" {
		payload["topic"] = topic
	}
	body, _ := json.Marshal(payload)
	resp, err := s.do(ctx, http.MethodPost, "/v1/ai/chat", strings.NewReader(string(body)), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorResult(err)
	}
	return textResult("Answer:\n" + string(answer))
}

// toolDigestTopic summarizes a topic via the server's AI layer. Requires an
// authenticated token: only the account's own subscriptions can be digested.
func (s *Server) toolDigestTopic(ctx context.Context, args map[string]any) *toolResult {
	topic, _ := args["topic"].(string)
	if !validTopic(topic) {
		return errorResult(errTopicRequired)
	}
	if authTokenFrom(ctx) == "" && s.config.AccessToken == "" {
		return errorResult(errNoToken)
	}
	since, _ := args["since"].(string)
	if since == "" {
		since = "24h"
	}
	body, _ := json.Marshal(map[string]string{"topic": topic, "since": since})
	resp, err := s.do(ctx, http.MethodPost, "/v1/ai/digest", strings.NewReader(string(body)), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	digest, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorResult(err)
	}
	return textResult("Digest of topic " + strconv.Quote(topic) + ":\n" + string(digest))
}

// toolBriefing summarizes all of the account's topics via the server's AI layer.
func (s *Server) toolBriefing(ctx context.Context, args map[string]any) *toolResult {
	if authTokenFrom(ctx) == "" && s.config.AccessToken == "" {
		return errorResult(errNoToken)
	}
	since, _ := args["since"].(string)
	if since == "" {
		since = "24h"
	}
	body, _ := json.Marshal(map[string]string{"since": since})
	resp, err := s.do(ctx, http.MethodPost, "/v1/ai/briefing", strings.NewReader(string(body)), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	briefing, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorResult(err)
	}
	return textResult("Briefing across your topics:\n" + string(briefing))
}

// toolListSubscriptions lists the account's synced subscriptions.
func (s *Server) toolListSubscriptions(ctx context.Context, _ map[string]any) *toolResult {
	if authTokenFrom(ctx) == "" && s.config.AccessToken == "" {
		return errorResult(errNoToken)
	}
	resp, err := s.do(ctx, http.MethodGet, "/v1/account", nil, nil)
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errorResult(errNoToken)
	}
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	var account struct {
		Subscriptions []map[string]any `json:"subscriptions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return errorResult(err)
	}
	if len(account.Subscriptions) == 0 {
		return textResult("The account has no subscriptions.")
	}
	serialized, err := json.MarshalIndent(account.Subscriptions, "", "  ")
	if err != nil {
		return errorResult(err)
	}
	return textResult("Subscriptions:\n" + string(serialized))
}

// toolPlanSubscription asks the server's AI layer for a subscription plan.
func (s *Server) toolPlanSubscription(ctx context.Context, args map[string]any) *toolResult {
	prompt, _ := args["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return errorResult(fmt.Errorf("argument 'prompt' is required"))
	}
	locale, _ := args["locale"].(string)
	body, _ := json.Marshal(map[string]string{"prompt": prompt, "locale": locale})
	resp, err := s.do(ctx, http.MethodPost, "/v1/ai/plan", strings.NewReader(string(body)), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("server returned %s: %s", resp.Status, firstKB(resp.Body)))
	}
	plan, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorResult(err)
	}
	return textResult("Proposed subscription plan (review before subscribing):\n" + string(plan))
}

// --- small helpers ---

func intArg(args map[string]any, name string) int {
	switch v := args[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func stringSliceArg(args map[string]any, name string) []string {
	raw, ok := args[name].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if str, ok := v.(string); ok && str != "" {
			out = append(out, str)
		}
	}
	return out
}

func firstKB(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 1024))
	return strings.TrimSpace(string(b))
}

// toolRequestPairing mints a device pairing code via POST /v1/device/pairing
// with the caller's credentials. The agent relays the code/deep link to the
// human; the tap on the phone completes the pairing (POST /v1/device/claim).
func (s *Server) toolRequestPairing(ctx context.Context, args map[string]any) *toolResult {
	if authTokenFrom(ctx) == "" && s.config.AccessToken == "" {
		return errorResult(errNoToken)
	}
	body := map[string]any{}
	if label, _ := args["label"].(string); label != "" {
		body["label"] = label
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return errorResult(err)
	}
	resp, err := s.do(ctx, http.MethodPost, "/v1/device/pairing", bytes.NewReader(payload), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorResult(err)
	}
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("pairing request failed: HTTP %d: %s", resp.StatusCode, string(b)))
	}
	text := "Pairing code minted (code: " + pairCode(b) + ", expires in 5 minutes). " +
		"Fire this link ON THE PHONE: " + pairDeepLink(b) + "?auto=1" +
		" (auto mode: the app pairs itself, zero user input; plain link " + pairDeepLink(b) + " opens a confirm screen)."
	return &toolResult{Content: []toolContent{{Type: "text", Text: text}}}
}

func pairCode(pairingResponse []byte) string {
	var parsed struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(pairingResponse, &parsed)
	return parsed.Code
}

func pairDeepLink(pairingResponse []byte) string {
	var parsed struct {
		DeepLink string `json:"deep_link"`
	}
	_ = json.Unmarshal(pairingResponse, &parsed)
	return parsed.DeepLink
}

// toolSetDeviceConfig writes a device's agent-channel config with the caller's
// credentials. A device-scoped caller can only address its own device (the
// server enforces this); unrestricted callers manage any of their devices.
func (s *Server) toolSetDeviceConfig(ctx context.Context, args map[string]any) *toolResult {
	if authTokenFrom(ctx) == "" && s.config.AccessToken == "" {
		return errorResult(errNoToken)
	}
	deviceID, _ := args["device_id"].(string)
	if !validDeviceID(deviceID) {
		return errorResult(fmt.Errorf("invalid device_id (expected dv_...)"))
	}
	cfg, ok := args["config"].(map[string]any)
	if !ok {
		return errorResult(fmt.Errorf("config must be a JSON object"))
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return errorResult(err)
	}
	resp, err := s.do(ctx, http.MethodPut, "/v1/device/"+deviceID+"/config", bytes.NewReader(payload), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("set_device_config failed: HTTP %d: %s", resp.StatusCode, string(b)))
	}
	return textResult("Device config stored. The app applies it on its next sync (background worker, max 15 min; immediately on open).")
}

// toolDeviceStatus lists paired devices via GET /v1/device.
func (s *Server) toolDeviceStatus(ctx context.Context, args map[string]any) *toolResult {
	if authTokenFrom(ctx) == "" && s.config.AccessToken == "" {
		return errorResult(errNoToken)
	}
	resp, err := s.do(ctx, http.MethodGet, "/v1/device", nil, nil)
	if err != nil {
		return errorResult(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return errorResult(fmt.Errorf("device_status failed: HTTP %d: %s", resp.StatusCode, string(b)))
	}
	var devices []struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		CreatedAt int64  `json:"created_at"`
		UpdatedAt int64  `json:"updated_at"`
		LastSeen  int64  `json:"last_seen"`
	}
	if err := json.Unmarshal(b, &devices); err != nil {
		return errorResult(err)
	}
	if len(devices) == 0 {
		return textResult("No paired devices. Use request_pairing to pair one.")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d paired device(s):\n", len(devices))
	for _, d := range devices {
		fmt.Fprintf(&sb, "- %s", d.ID)
		if d.Label != "" {
			fmt.Fprintf(&sb, " (%s)", d.Label)
		}
		fmt.Fprintf(&sb, " last sync: %d\n", d.LastSeen)
	}
	return textResult(sb.String())
}
