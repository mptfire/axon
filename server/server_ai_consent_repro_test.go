package server

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

// Consent must be checked before parsing content, querying history or consulting a cache.
func TestServer_AIConsent_DefaultDeniedEveryContentEndpoint(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled, c.AIProvider = true, "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("alice", "alice")}
	rr := request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+c.BaseURL+`","topic":"alerts"}`, auth)
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PUT", "/alerts", "private account content", auth)
	require.Equal(t, 200, rr.Code)
	var calls atomic.Int32
	var embedCalls atomic.Int32
	s.embedder = s.ai.Embedder("test-embeddings")
	s.ai.Mock().SetEmbedHandler(func(text string) ([]float32, error) {
		embedCalls.Add(1)
		return []float32{1, 0}, nil
	})
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		calls.Add(1)
		return &ai.Response{Text: `{"subscriptions":[{"topic":"alerts"}],"display_name":"Alerts","summary":"Private","answer":"Private","headline":"Private","sections":[]}`}, nil
	})
	for _, endpoint := range []string{"plan", "tune", "digest", "chat", "briefing", "chat/stream"} {
		t.Run(endpoint, func(t *testing.T) {
			rr := request(t, s, "POST", "/v1/ai/"+endpoint, `{"prompt":"private wish","topic":"alerts","goal":"quiet","question":"private question"}`, auth)
			require.Equal(t, 403, rr.Code, rr.Body.String())
			require.Equal(t, 40302, toHTTPError(t, rr.Body.String()).Code)
		})
	}
	require.Zero(t, calls.Load())
	require.Zero(t, embedCalls.Load())
}
