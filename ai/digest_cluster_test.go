package ai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClusterMessages(t *testing.T) {
	// [1,0] family vs [0,1] family: two tight groups
	vectors := [][]float32{
		{1, 0.05}, {0.95, 0.1}, {0.9, 0.05}, // incident family
		{0.05, 1}, {0.1, 0.95}, // garden family
	}
	clusters := ClusterMessages(vectors, ClusterSimilarityThreshold)
	require.Len(t, clusters, 2)
	require.Equal(t, []int{0, 1, 2}, clusters[0])
	require.Equal(t, []int{3, 4}, clusters[1])
}

func TestClusterMessages_SingleMessageStartsCluster(t *testing.T) {
	clusters := ClusterMessages([][]float32{{1, 0}}, ClusterSimilarityThreshold)
	require.Len(t, clusters, 1)
}

func TestBuildClusterHints(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	embedder := client.Embedder("embed-model")
	require.NotNil(t, embedder)
	messages := []DigestMessage{
		{ID: "m1", Message: "disk usage alert on server one"},
		{ID: "m2", Message: "disk usage alert on server two"},
		{ID: "m3", Message: "disk usage alert on server three"},
		{ID: "m4", Message: "totally different subject entirely"},
		{ID: "m5", Message: "another unrelated subject here"},
	}
	hints := buildClusterHints(embedder, messages)
	t.Logf("hints: %q", hints)
	// The deterministic mock vectors may or may not split these into 2+ groups;
	// the function must never panic and must never fabricate message numbers.
	require.NotContains(t, hints, "#0")
}

func TestBuildClusterHints_EmptyOnError(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetEmbedHandler(func(text string) ([]float32, error) {
		return nil, ErrInvalidResponse
	})
	embedder := client.Embedder("embed-model")
	hints := buildClusterHints(embedder, []DigestMessage{{Message: "x"}, {Message: "y"}})
	require.Equal(t, "", hints)
}
