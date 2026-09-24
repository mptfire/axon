package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const validPlanJSON = `{
  "subscriptions": [
    {
      "topic": "phil-ci",
      "display_name": "CI failures",
      "search": "failed|error",
      "min_priority": 3,
      "justification": "Only failed builds should notify."
    }
  ],
  "publisher_instructions": "curl -d \"build failed\" -H \"Priority: 4\" http://127.0.0.1:12345/phil-ci",
  "follow_up_questions": ["Do you also want PR reviews?"]
}`

func newTestPlanner(t *testing.T, responseText string) *Planner {
	t.Helper()
	client := newTestClient(t, &Config{Provider: "mock", Model: "test-model"})
	client.Mock().EnqueueText(responseText)
	return NewPlanner(client)
}

func TestPlanner_Plan(t *testing.T) {
	planner := newTestPlanner(t, validPlanJSON)
	plan, err := planner.Plan(context.Background(), "http://127.0.0.1:12345", "notify me when CI fails", "en", []string{"misc"})
	require.Nil(t, err)

	// BaseURL and disclaimer are server-stamped, never model text
	require.Equal(t, "http://127.0.0.1:12345", plan.BaseURL)
	require.Equal(t, PlannerDisclaimer, plan.Disclaimer)

	require.Len(t, plan.Subscriptions, 1)
	sub := plan.Subscriptions[0]
	require.Equal(t, "phil-ci", sub.Topic)
	require.Equal(t, "CI failures", sub.DisplayName)
	require.NotNil(t, sub.Filters)
	require.Equal(t, "failed|error", sub.Filters.Search)
	require.Equal(t, 3, sub.Filters.MinPriority)
	require.Equal(t, "Only failed builds should notify.", sub.Justification)
	require.Len(t, plan.FollowUpQuestions, 1)
	require.Contains(t, plan.PublisherInstructions, "curl")
}

func TestPlanner_PlanAcceptsFences(t *testing.T) {
	planner := newTestPlanner(t, "```json\n"+validPlanJSON+"\n```")
	plan, err := planner.Plan(context.Background(), "http://x", "wish", "", nil)
	require.Nil(t, err)
	require.Len(t, plan.Subscriptions, 1)
}

func TestPlanner_PlanRejectsGarbage(t *testing.T) {
	for _, garbage := range []string{"", "sorry I cannot help with that", "here you go: {...}"} {
		planner := newTestPlanner(t, garbage)
		_, err := planner.Plan(context.Background(), "http://x", "wish", "", nil)
		require.Error(t, err, garbage)
	}
	planner := newTestPlanner(t, `{"subscriptions": []}`)
	_, err := planner.Plan(context.Background(), "http://x", "wish", "", nil)
	require.ErrorContains(t, err, "no subscriptions")
}

func TestPlanner_PlanRejectsInvalidTopic(t *testing.T) {
	// Topics must be [-_A-Za-z0-9]{1,64} — anything else (paths, spaces, injection) is rejected
	for _, topic := range []string{"", "a/b", "my topic", "../etc", strings.Repeat("x", 65), "topıcos"} {
		planner := newTestPlanner(t, `{"subscriptions": [{"topic": "`+topic+`"}]}`)
		_, err := planner.Plan(context.Background(), "http://x", "wish", "", nil)
		require.ErrorContains(t, err, "invalid topic", topic)
	}
}

func TestPlanner_PlanSanitizes(t *testing.T) {
	// Out-of-range priority dropped, control characters stripped, oversized list truncated,
	// display name capped — model output is data, not instructions.
	raw := `{"subscriptions": [`
	for i := 0; i < 8; i++ {
		raw += `{"topic": "topic-` + string(rune('a'+i)) + `", "min_priority": 9, "display_name": "name\nwith\nnewlines", "justification": "j"},`
	}
	raw = strings.TrimSuffix(raw, ",") + `]}`
	planner := newTestPlanner(t, raw)
	plan, err := planner.Plan(context.Background(), "http://x", "wish", "", nil)
	require.Nil(t, err)
	require.Len(t, plan.Subscriptions, PlannerMaxSubscriptions)
	for _, sub := range plan.Subscriptions {
		require.Nil(t, sub.Filters) // invalid min_priority alone does not create filters
		require.NotContains(t, sub.DisplayName, "\n")
	}
}

func TestPlanner_PlanPromptLimits(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	planner := NewPlanner(client)
	_, err := planner.Plan(context.Background(), "http://x", strings.Repeat("x", PlannerMaxPromptChars+1), "", nil)
	require.ErrorContains(t, err, "prompt too long")
	_, err = planner.Plan(context.Background(), "http://x", "   ", "", nil)
	require.ErrorContains(t, err, "required")
	// Nothing was sent to the provider
	report := client.Report("")
	require.Equal(t, int64(0), report.Global.Requests)
}

func TestPlanner_PlanPromptContainsCapabilityMap(t *testing.T) {
	var seenSystem string
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		seenSystem = req.System
		return &Response{Text: validPlanJSON}, nil
	})
	planner := NewPlanner(client)
	_, err := planner.Plan(context.Background(), "http://ntfy.example.com", "wish", "en", []string{"one", "two"})
	require.Nil(t, err)
	require.Contains(t, seenSystem, "http://ntfy.example.com/mytopic")
	require.Contains(t, seenSystem, "CANNOT")       // capability honesty
	require.Contains(t, seenSystem, `"one", "two"`) // existing topics
	require.Contains(t, seenSystem, "JSON object")
}

func TestPlanner_Tune(t *testing.T) {
	planner := newTestPlanner(t, `{"display_name": "Night pages", "min_priority": 4, "search": "prod|fatal", "justification": "Only critical at night."}`)
	result, err := planner.Tune(context.Background(), &TuneInput{Topic: "prod-alerts", MinPriority: 2}, "only page me at night for real emergencies")
	require.Nil(t, err)
	require.Equal(t, "Night pages", result.DisplayName)
	require.Equal(t, 4, result.Filters.MinPriority)
	require.Equal(t, "prod|fatal", result.Filters.Search)
	require.Equal(t, "Only critical at night.", result.Justification)

	// Prompt includes the current settings as data
	client := newTestClient(t, &Config{Provider: "mock"})
	var seenPrompt string
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		seenPrompt = req.Prompt
		return &Response{Text: `{"search": ""}`}, nil
	})
	planner = NewPlanner(client)
	_, err = planner.Tune(context.Background(), &TuneInput{Topic: "t", Search: "old", MinPriority: 3}, "louder")
	require.Nil(t, err)
	require.Contains(t, seenPrompt, `"Topic": "t"`)
	require.Contains(t, seenPrompt, "louder")
}

func TestPlanner_TuneRejectsGarbage(t *testing.T) {
	planner := newTestPlanner(t, "nope")
	_, err := planner.Tune(context.Background(), &TuneInput{Topic: "t"}, "goal")
	require.Error(t, err)
}

func TestSanitizeSingleLineText(t *testing.T) {
	require.Equal(t, "clean", sanitizeSingleLineText("  clean  ", 100))
	require.Equal(t, "ab", sanitizeSingleLineText("a\x00b\x1f", 100))
	require.Equal(t, "0123456789", sanitizeSingleLineText("0123456789", 10))
	require.Equal(t, "", sanitizeSingleLineText("\n\t", 10))
}
