package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func TestServer_AI_Briefing(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

	// Subscribe to two topics, seed both
	for _, topic := range []string{"backups", "home-alerts"} {
		rr := request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"`+topic+`"}`, auth)
		require.Equal(t, 200, rr.Code)
	}
	rr := request(t, s, "PUT", "/backups", "backup failed exit 2", map[string]string{"Title": "db-1"})
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PUT", "/home-alerts", "garage door open for 30 minutes", nil)
	require.Equal(t, 200, rr.Code)

	// The briefing prompt must span both topics; the invented section topic is dropped
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		require.Equal(t, ai.FeatureDigest, req.Feature)
		require.Contains(t, req.Prompt, "## Topic: backups")
		require.Contains(t, req.Prompt, "## Topic: home-alerts")
		return &ai.Response{Text: `{"headline": "Two incidents overnight", "sections": [
			{"topic": "backups", "title": "Backup failure", "points": ["db-1 exit 2"]},
			{"topic": "made-up-topic", "title": "Garage", "points": ["open 30 min"]},
			{"topic": "_", "title": "Pattern", "points": ["both after midnight"]}
		]}`}, nil
	})

	rr = request(t, s, "POST", "/v1/ai/briefing", `{"since":"24h"}`, auth)
	require.Equal(t, 200, rr.Code)
	var briefing ai.BriefingResult
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&briefing))
	require.Equal(t, 2, briefing.TopicCount)
	require.Equal(t, 2, briefing.MessageCount)
	require.Equal(t, "Two incidents overnight", briefing.Headline)
	require.Len(t, briefing.Sections, 3)
	require.Equal(t, "backups", briefing.Sections[0].Topic)
	require.Equal(t, "_", briefing.Sections[1].Topic) // invented topic downgraded
	require.Equal(t, ai.BriefingDisclaimer, briefing.Disclaimer)

	// Anonymous: 401
	rr = request(t, s, "POST", "/v1/ai/briefing", `{"since":"24h"}`, nil)
	require.Equal(t, 401, rr.Code)

	// Provider garbage: 500. A new message changes the prompt, so the cached successful
	// briefing above does not mask the failure.
	rr = request(t, s, "PUT", "/backups", "a distinct later backup event", nil)
	require.Equal(t, 200, rr.Code)
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		return &ai.Response{Text: "not json"}, nil
	})
	rr = request(t, s, "POST", "/v1/ai/briefing", `{"since":"24h"}`, auth)
	require.Equal(t, 500, rr.Code)
}

func TestServer_AI_Briefing_EmptySkipsProvider(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"quiet"}`, auth)

	// No provider handler: must not be called
	rr := request(t, s, "POST", "/v1/ai/briefing", `{}`, auth)
	require.Equal(t, 200, rr.Code)
	var briefing ai.BriefingResult
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&briefing))
	require.Contains(t, briefing.Headline, "No messages")
}
