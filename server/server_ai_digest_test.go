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

func TestServer_AI_Digest_RequiresAuthAndSubscription(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))

	// Anonymous: 401
	rr := request(t, s, "POST", "/v1/ai/digest", `{"topic":"backups"}`, nil)
	require.Equal(t, 401, rr.Code)

	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

	// Logged in, but not subscribed: 403
	rr = request(t, s, "POST", "/v1/ai/digest", `{"topic":"backups"}`, auth)
	require.Equal(t, 403, rr.Code)

	// Subscribe via the regular account endpoint
	rr = request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"backups"}`, auth)
	require.Equal(t, 200, rr.Code)

	// Invalid duration: 400
	rr = request(t, s, "POST", "/v1/ai/digest", `{"topic":"backups","since":"2h"}`, auth)
	require.Equal(t, 200, rr.Code) // sanity: subscribed now, 2h is valid
	rr = request(t, s, "POST", "/v1/ai/digest", `{"topic":"backups","since":"45m"}`, auth)
	require.Equal(t, 400, rr.Code) // below 1h floor
	rr = request(t, s, "POST", "/v1/ai/digest", `{"topic":"backups","since":"nonsense"}`, auth)
	require.Equal(t, 400, rr.Code)
}

func TestServer_AI_Digest(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	rr := request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"backups"}`, auth)
	require.Equal(t, 200, rr.Code)

	// Seed the cache
	rr = request(t, s, "PUT", "/backups", "backup failed exit 2", map[string]string{"Title": "db-1"})
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PUT", "/backups", "backup retry ok", nil)
	require.Equal(t, 200, rr.Code)

	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		require.Equal(t, ai.FeatureDigest, req.Feature)
		require.Contains(t, req.Prompt, "backup failed exit 2")
		require.Contains(t, req.Prompt, "backup retry ok")
		require.Contains(t, req.System, "untrusted DATA")
		return &ai.Response{Text: `{"headline": "Backups degraded", "sections": [{"title": "Failures", "points": ["2 failures", "db-1 exit 2"]}]}`}, nil
	})

	rr = request(t, s, "POST", "/v1/ai/digest", `{"topic":"backups","since":"24h"}`, auth)
	require.Equal(t, 200, rr.Code)
	var digest ai.DigestResult
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&digest))
	require.Equal(t, "backups", digest.Topic)
	require.Equal(t, 2, digest.MessageCount)
	require.Equal(t, "Backups degraded", digest.Headline)
	require.Len(t, digest.Sections, 1)
	require.Equal(t, ai.DigestDisclaimer, digest.Disclaimer)

	// Provider garbage: 500, not user error. A new message changes the prompt, so the
	// response cache from the successful digest above does not mask the failure.
	rr = request(t, s, "PUT", "/backups", "a third, distinct backup event", nil)
	require.Equal(t, 200, rr.Code)
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		return &ai.Response{Text: "sorry, no json"}, nil
	})
	rr = request(t, s, "POST", "/v1/ai/digest", `{"topic":"backups"}`, auth)
	require.Equal(t, 500, rr.Code)
}

func TestServer_AI_Digest_EmptyTopicReturnsZeroMessages(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"empty-topic"}`, auth)

	// No messages yet: the digester must not be called; server answers with count 0
	rr := request(t, s, "POST", "/v1/ai/digest", `{"topic":"empty-topic"}`, auth)
	require.Equal(t, 200, rr.Code)
	var digest ai.DigestResult
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&digest))
	require.Equal(t, 0, digest.MessageCount)
	require.True(t, strings.Contains(digest.Disclaimer, "AI-generated"))
}
