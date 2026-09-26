package ai

import (
	"context"
	"math"
	"strings"
	"sync"
)

// TextEmbedder is the optional provider capability of computing text embeddings
// (OpenAI-compatible /embeddings; Ollama exposes the same protocol). Providers without
// it simply don't offer embedding-backed retrieval.
type TextEmbedder interface {
	EmbedTexts(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// EmbeddingsCacheMax bounds how many embeddings the in-memory cache keeps. Alert
// storms embed the same texts repeatedly, so the cache absorbs most of the cost.
const EmbeddingsCacheMax = 5000

// Embedder computes and caches text embeddings for one embeddings model. Repeated
// identical texts (alert storms, recurring digests) hit the cache instead of the
// provider. Thread-safe; bounded by insertion-order eviction.
type Embedder struct {
	client *Client
	model  string

	mu    sync.Mutex
	cache map[string][]float32
	keys  []string // insertion order, for eviction
}

// Embedder returns an Embedder bound to the given embeddings model, or nil when AI is
// disabled, the model is empty, or the provider cannot embed. A nil Embedder means
// callers should fall back to keyword-only retrieval.
func (c *Client) Embedder(model string) *Embedder {
	if !c.Enabled() || model == "" {
		return nil
	}
	if _, ok := c.provider.(TextEmbedder); !ok {
		return nil
	}
	return &Embedder{client: c, model: model, cache: make(map[string][]float32)}
}

// Embed returns one vector per input text, in input order. Uncached texts are sent to
// the provider in batches of EmbedBatchSize; repeated identical texts are served from
// the cache.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missingIdx []int
	var missing []string
	for i, text := range texts {
		if vec, ok := e.lookup(text); ok {
			out[i] = vec
			continue
		}
		missingIdx = append(missingIdx, i)
		missing = append(missing, text)
	}
	if len(missing) > 0 {
		const batch = 64
		var vectors [][]float32
		for start := 0; start < len(missing); start += batch {
			end := start + batch
			if end > len(missing) {
				end = len(missing)
			}
			part, err := e.client.embedTexts(ctx, e.model, missing[start:end])
			if err != nil {
				return nil, err
			}
			vectors = append(vectors, part...)
		}
		if len(vectors) != len(missing) {
			return nil, ErrInvalidResponse
		}
		for k, vec := range vectors {
			out[missingIdx[k]] = vec
			e.store(missing[k], vec)
		}
	}
	return out, nil
}

func (e *Embedder) lookup(text string) ([]float32, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	vec, ok := e.cache[embedCacheKey(text)]
	return vec, ok
}

func (e *Embedder) store(text string, vec []float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := embedCacheKey(text)
	if _, ok := e.cache[key]; !ok {
		e.keys = append(e.keys, key)
		if len(e.keys) > EmbeddingsCacheMax {
			oldest := e.keys[0]
			e.keys = e.keys[1:]
			delete(e.cache, oldest)
		}
	}
	e.cache[key] = vec
}

// embedCacheKey normalizes a text for the embedding cache (case-insensitive, trimmed).
func embedCacheKey(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

// Cosine returns the cosine similarity between two vectors, 0 if either is empty or
// the lengths differ.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
