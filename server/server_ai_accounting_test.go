package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func TestServer_AI_Enrichment_AttributesPublisherUsage(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	c.AIEnrichmentEnabled = true
	c.AIEnrichTopics = []string{"alerts"}
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
	auth := map[string]string{"Authorization": util.BasicAuth("alice", "alice")}
	s.ai.Mock().Enqueue(ai.MockResult{Response: &ai.Response{Text: `{"summary":"Alert"}`, InputTokens: 3, OutputTokens: 2}})
	rr := request(t, s, "PUT", "/alerts", strings.Repeat("message data ", 30), auth)
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "GET", "/v1/ai/usage", "", auth)
	require.Equal(t, 200, rr.Code)
	var usage ai.UsageReport
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&usage))
	require.Equal(t, int64(1), usage.Visitor.Requests)
	require.Equal(t, int64(5), usage.Visitor.Total())
}
