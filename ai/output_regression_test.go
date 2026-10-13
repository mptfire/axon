package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanner_RejectsNullSubscription(t *testing.T) {
	require.NotPanics(t, func() {
		_, err := parsePlan("https://axon.example", `{"subscriptions":[null]}`)
		require.ErrorIs(t, err, ErrInvalidResponse)
	})
}

func TestEnrich_TranslationRequiresOptIn(t *testing.T) {
	c, err := New(&Config{Provider: "mock"})
	require.NoError(t, err)
	c.Mock().EnqueueText(`{"summary":"An alert","translation":"model-supplied replacement"}`)
	out, err := NewEnricher(c).Enrich(context.Background(), &EnrichRequest{Topic: "alerts", Message: "untrusted content"})
	require.NoError(t, err)
	require.Empty(t, out.Translation)
}

// OpenAI's strict structured-output contract requires all properties to be
// required and additionalProperties:false on every object, including array items.
func TestFeatureSchemas_StrictOutputContract(t *testing.T) {
	for _, schema := range []*Schema{PlanSchema, TuneSchema, EnrichmentSchema, DigestSchema, BriefingSchema, ChatSchema} {
		t.Run(schema.Name, func(t *testing.T) { checkStrictObject(t, schema.Schema) })
	}
}

func checkStrictObject(t *testing.T, schema map[string]any) {
	t.Helper()
	if schema["type"] == "object" {
		require.Equal(t, false, schema["additionalProperties"])
		props := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]string)
		require.Len(t, required, len(props))
		for key, property := range props {
			require.Contains(t, required, key)
			checkStrictObject(t, property.(map[string]any))
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		checkStrictObject(t, items)
	}
}
