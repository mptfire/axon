package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SchemaPrompt renders a Schema as a text instruction for providers without native
// structured-output support. Callers must still validate responses; this only raises
// the odds of well-formed output.
func SchemaPrompt(schema *Schema) string {
	serialized, err := json.MarshalIndent(schema.Schema, "", "  ")
	if err != nil {
		return "Respond with valid JSON only."
	}
	var b strings.Builder
	b.WriteString("You must respond with a single JSON object only — no prose, no markdown fences, ")
	b.WriteString("no text before or after the JSON. It must conform to this JSON schema:\n\n")
	b.WriteString(fmt.Sprintf("Schema name: %s\n\n%s\n", schema.Name, string(serialized)))
	return b.String()
}

// ValidateJSONObject performs a minimal structural check that text is a JSON object.
// Full schema validation is deliberately left to feature code (or a validator dependency)
// once the first schema-consuming feature lands; the AI layer only guarantees a string.
func ValidateJSONObject(text string) error {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return fmt.Errorf("response is not a JSON object")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return fmt.Errorf("response is not valid JSON: %w", err)
	}
	return nil
}
