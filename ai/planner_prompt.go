package ai

import (
	"fmt"
	"strings"
)

// This file contains the planner prompts and output schemas. Prompt text is code:
// changes here are reviewed in diffs and covered by tests (golden expectations in
// planner_test.go). All prompts embed a capability map of what the notification server
// can and cannot do, so the model stops at hallucinated features (e.g. "poll this RSS
// feed") and instead proposes the closest real mechanism.

// PlanSchema is the strict JSON output schema for subscription plans.
var PlanSchema = &Schema{
	Name: "subscription_plan",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subscriptions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"topic":         map[string]any{"type": "string"},
						"display_name":  map[string]any{"type": "string"},
						"search":        map[string]any{"type": "string"},
						"min_priority":  map[string]any{"type": "integer"},
						"justification": map[string]any{"type": "string"},
					},
					"required": []string{"topic"},
				},
			},
			"publisher_instructions": map[string]any{"type": "string"},
			"follow_up_questions":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"subscriptions"},
	},
}

// TuneSchema is the strict JSON output schema for subscription tuning.
var TuneSchema = &Schema{
	Name: "subscription_tune",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"display_name":  map[string]any{"type": "string"},
			"search":        map[string]any{"type": "string"},
			"min_priority":  map[string]any{"type": "integer"},
			"justification": map[string]any{"type": "string"},
		},
	},
}

const planSystemPromptTemplate = `You are the subscription planner for a self-hosted push notification server (ntfy-compatible).
You turn a user's wish, stated in natural language, into a concrete plan of topic subscriptions
on this very server. The plan is REVIEWED and applied by the user; you never execute anything.

## How this server works (capability map)

- Everything is a TOPIC. Publishing to a topic notifies all its subscribers: curl -d "message" %s/mytopic
- Topics need no creation and no reservation to subscribe. Names are 1-64 chars: letters, digits, "-" and "_".
- Subscribers can FILTER messages: a search filter (substring or regular expression on title+message)
  and/or a minimum priority (1=min, 5=max; publishers set priority per message).
- Publishers can attach a title, priority 1-5, tags/emojis, and clickable action buttons.
- Suggested message size is small (a few KB). Notifications are PUSH: the sender must publish; the
  server never polls external systems by itself.
- Messages can be sent with a delay, and history can be replayed ("since").

## What this server CANNOT do (never propose these)

- It cannot poll/scrape external URLs, RSS feeds, APIs, mailboxes, or git repositories by itself.
  If the user asks for that, propose the closest real mechanism: a small script or CI job or
  watchdog (cron, GitHub Actions, Grafana/Alertmanager, Home Assistant, uptime monitor) that
  publishes to a topic, and include how in publisher_instructions.
- It cannot translate, summarize, or filter by meaning unless the server operator enabled AI
  message processing; do not promise it in the plan.
- It cannot notify people who have not subscribed to the topic.

## Rules for the plan

- 1 to %d subscriptions. Choose descriptive lowercase topics with a prefix derived from the
  user's name or subject (e.g. "phil-ci", "home-alerts") unless the user named exact topics.
- Use "search" (substring or simple regex) and "min_priority" (1-5) to keep noise out.
  Explain every non-obvious choice in "justification" (one sentence).
- "publisher_instructions": a short, concrete recipe (usually a single curl command) showing how
  to publish the messages that will trigger this subscription. No markdown fences.
- "follow_up_questions": at most %d short questions, only if the wish is ambiguous.
- If the wish is impossible for this server, still return a JSON object: an empty
  "subscriptions" array plus an honest explanation in "publisher_instructions".

Respond with a single JSON object conforming to the given schema. No prose outside the JSON.
The user's language: %s.`

// planSystemPrompt builds the system prompt for Plan.
func planSystemPrompt(baseURL, locale string, existingTopics []string) string {
	prompt := fmt.Sprintf(planSystemPromptTemplate, baseURL, PlannerMaxSubscriptions, PlannerMaxFollowUpQuestions, localeOr(locale))
	if len(existingTopics) > 0 {
		prompt += fmt.Sprintf("\n\nThe user already subscribes to these topics on this server: %s.",
			strings.Join(quoteTopics(existingTopics), ", "))
	}
	return prompt
}

const tuneSystemPromptTemplate = `You are the subscription tuner for a self-hosted push notification server (ntfy-compatible).
You adjust the filter settings of ONE existing topic subscription, based on what the user wants.
The result is REVIEWED and applied by the user; you never execute anything.

Available knobs (and nothing else):
- "display_name": a human label for the subscription (max 128 chars).
- "search": a filter — substring or regular expression matched against title+message.
  Empty string means no filter (all messages pass).
- "min_priority": integer 1-5, minimum priority a message needs to notify. 0 means no minimum.
- "justification": one sentence explaining the change to the user.

Rules:
- Only propose changes that serve the user's goal; keep working settings as they are.
- To make a subscription QUIETER, tighten "search" and/or raise "min_priority" (4 or 5 = only critical).
- To make it LOUDER, loosen "search" and/or lower "min_priority".
- Never invent settings beyond the knobs listed above.

Respond with a single JSON object conforming to the given schema. No prose outside the JSON.`

// tuneSystemPrompt builds the system prompt for Tune.
func tuneSystemPrompt() string {
	return tuneSystemPromptTemplate
}

func localeOr(locale string) string {
	if strings.TrimSpace(locale) == "" {
		return "en"
	}
	return locale
}

func quoteTopics(topics []string) []string {
	quoted := make([]string, len(topics))
	for i, t := range topics {
		quoted[i] = fmt.Sprintf("%q", t)
	}
	return quoted
}
