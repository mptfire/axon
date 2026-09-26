package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbedder_BatchesCachesAndDedupes(t *testing.T) {
	var requests int
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		batchSizes = append(batchSizes, len(body.Input))
		data := make([]map[string]any, len(body.Input))
		for i, text := range body.Input {
			data[i] = map[string]any{"index": i, "embedding": []float32{float32(len(text)), 1}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()

	prov, err := newOpenAIProvider("openai-compatible", server.URL, "", "", "embed-model", server.Client())
	require.Nil(t, err)
	client := newTestClient(t, &Config{Provider: "mock"})
	client.provider = prov

	emb := client.Embedder("embed-model")
	require.NotNil(t, emb)

	vectors, err := emb.Embed(context.Background(), []string{"alpha", "beta", "alpha"})
	require.Nil(t, err)
	require.Len(t, vectors, 3)
	require.Equal(t, []float32{5, 1}, vectors[0])
	require.Equal(t, vectors[0], vectors[2], "duplicate text shares the vector")
	require.Equal(t, []int{3}, batchSizes, "single batched request for 3 texts")

	// Second call: fully cached — no provider request
	before := requests
	_, err = emb.Embed(context.Background(), []string{"alpha", "beta"})
	require.Nil(t, err)
	require.Equal(t, before, requests)
}

func TestEmbedder_NilWhenNotCapableOrUnset(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "anthropic", APIKey: "k"})
	require.Nil(t, client.Embedder("embed-model"), "anthropic cannot embed")
	client2 := newTestClient(t, &Config{Provider: "mock"})
	require.NotNil(t, client2.Embedder("m"))
	require.Nil(t, client2.Embedder(""))
	var nilClient *Client
	require.Nil(t, nilClient.Embedder("m"))
}

func TestCosine(t *testing.T) {
	require.InDelta(t, 1, Cosine([]float32{1, 2}, []float32{2, 4}), 0.0001)
	require.InDelta(t, 0, Cosine([]float32{1, 0}, []float32{0, 1}), 0.0001)
	require.InDelta(t, 0, Cosine(nil, []float32{1}), 0.0001)
	require.InDelta(t, 0, Cosine([]float32{0, 0}, []float32{0, 0}), 0.0001)
}
