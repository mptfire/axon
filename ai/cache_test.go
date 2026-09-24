package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCache_HitAndMiss(t *testing.T) {
	c := NewCache(1024 * 1024)
	req := &Request{Feature: FeaturePlan, Prompt: "plan", UserKey: "user:phil"}
	require.Nil(t, c.Get(req))

	resp := &Response{Text: "the plan", Model: "m"}
	c.Store(req, resp)

	// Hit, same user
	got := c.Get(req)
	require.NotNil(t, got)
	require.Equal(t, "the plan", got.Text)
	require.True(t, got.Cached)

	// Hit, different user (attribution is excluded from the key)
	got = c.Get(&Request{Feature: FeaturePlan, Prompt: "plan", UserKey: "user:ben"})
	require.NotNil(t, got)

	// Miss: different feature
	require.Nil(t, c.Get(&Request{Feature: FeatureChat, Prompt: "plan"}))
	// Miss: different prompt
	require.Nil(t, c.Get(&Request{Feature: FeaturePlan, Prompt: "other"}))
	// Miss: different model
	require.Nil(t, c.Get(&Request{Feature: FeaturePlan, Prompt: "plan", Model: "other-model"}))
}

func TestCache_LRUEviction(t *testing.T) {
	// Entries are ~len(text)+overhead bytes; make the cache fit ~2 entries
	c := NewCache(int64(len("message")*3 + 256))

	a := &Request{Prompt: "a"}
	bReq := &Request{Prompt: "b"}
	cc := &Request{Prompt: "c"}
	text := strings.Repeat("message", 1)
	c.Store(a, &Response{Text: text})
	c.Store(bReq, &Response{Text: text})

	// Touch a, making b the LRU entry
	require.NotNil(t, c.Get(a))

	c.Store(cc, &Response{Text: text})
	require.NotNil(t, c.Get(a))
	require.Nil(t, c.Get(bReq), "b should have been evicted as least recently used")
	require.NotNil(t, c.Get(cc))

	// Re-store overwrites without growing
	c.Store(a, &Response{Text: text})
	require.NotNil(t, c.Get(a))
}

func TestCache_TooLargeNotStored(t *testing.T) {
	c := NewCache(16)
	big := &Request{Prompt: "big"}
	c.Store(big, &Response{Text: strings.Repeat("x", 4096)})
	require.Nil(t, c.Get(big))
}

func TestCache_MessagesIncludedInKey(t *testing.T) {
	c := NewCache(1024 * 1024)
	req := &Request{Messages: []Message{{Role: RoleUser, Content: "one"}}}
	c.Store(req, &Response{Text: "r1"})
	require.Nil(t, c.Get(&Request{Messages: []Message{{Role: RoleUser, Content: "two"}}}))

	// Schema changes the key
	schemaReq := &Request{Prompt: "s", JSONSchema: &Schema{Name: "thing", Schema: map[string]any{"type": "object"}}}
	require.Nil(t, c.Get(schemaReq))
	c.Store(schemaReq, &Response{Text: "r2"})
	require.NotNil(t, c.Get(&Request{Prompt: "s", JSONSchema: &Schema{Name: "thing", Schema: map[string]any{"type": "object"}}}))
}
