package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func TestServer_AI_DigestScheduler(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	c.AuthDefault = user.PermissionDenyAll // Digest privacy relies on the read ACL grant
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

	// Subscribe and seed history (phil is admin, so publishing works under deny-all)
	rr := request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"backups"}`, auth)
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PUT", "/backups", "backup failed exit 2", map[string]string{
		"Title":         "db-1",
		"Authorization": util.BasicAuth("phil", "phil"), // deny-all: publish needs auth
	})
	require.Equal(t, 200, rr.Code)

	// Enable the daily briefing for the current UTC hour
	hour := time.Now().UTC().Hour()
	rr = request(t, s, "PATCH", "/v1/account/settings", `{"digest":{"enabled":true,"hour":`+fmt.Sprintf("%d", hour)+`,"since_hours":24}}`, auth)
	require.Equal(t, 200, rr.Code)

	// Invalid settings are rejected
	rr = request(t, s, "PATCH", "/v1/account/settings", `{"digest":{"hour":99}}`, auth)
	require.Equal(t, 400, rr.Code)

	// Run the scheduler: briefing generated, delivered, marked
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		require.Equal(t, ai.FeatureDigest, req.Feature)
		require.Contains(t, req.Prompt, "## Topic: backups")
		return &ai.Response{Text: `{"headline": "One backup failure", "sections": [{"topic": "backups", "title": "db-1", "points": ["exit code 2"]}]}`}, nil
	})
	s.sendDueBriefings()

	// The digest topic was provisioned and the briefing is cached there
	rr = request(t, s, "GET", "/v1/account", "", auth)
	require.Equal(t, 200, rr.Code)
	var account apiAccountResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&account))
	require.NotNil(t, account.Digest)
	require.NotNil(t, account.Digest.Enabled)
	require.True(t, *account.Digest.Enabled)
	require.NotNil(t, account.Digest.Topic)
	require.NotNil(t, account.Digest.LastDaily)
	require.Equal(t, time.Now().UTC().Format("2006-01-02"), *account.Digest.LastDaily)

	digestTopic := *account.Digest.Topic
	require.True(t, strings.HasPrefix(digestTopic, "dg_"))
	known := false
	for _, sub := range account.Subscriptions {
		if sub.Topic == digestTopic {
			known = true
		}
	}
	require.True(t, known, "digest topic should be provisioned as a subscription")

	rr = request(t, s, "GET", "/"+digestTopic+"/json?poll=1", "", auth)
	require.Equal(t, 200, rr.Code)
	messages := toMessages(t, rr.Body.String())
	require.Len(t, messages, 1)
	require.Equal(t, "Daily briefing", messages[0].Title)
	require.Contains(t, messages[0].Message, "One backup failure")
	require.Contains(t, messages[0].Message, "[backups]")

	// Running again the same day: no duplicate
	s.sendDueBriefings()
	rr = request(t, s, "GET", "/"+digestTopic+"/json?poll=1", "", auth)
	require.Equal(t, 200, rr.Code)
	require.Len(t, toMessages(t, rr.Body.String()), 1)

	// The digest topic is ACL-protected: another user cannot read it
	require.Nil(t, s.userManager.AddUser("ben", "ben", user.RoleUser, false))
	rr = request(t, s, "GET", "/"+digestTopic+"/json?poll=1", "", map[string]string{
		"Authorization": util.BasicAuth("ben", "ben"),
	})
	require.Equal(t, 403, rr.Code)
}

func TestServer_AI_DigestScheduler_QuietDay(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"quiet-topic"}`, auth)
	rr := request(t, s, "PATCH", "/v1/account/settings", `{"digest":{"enabled":true,"hour":`+fmt.Sprintf("%d", time.Now().UTC().Hour())+`}}`, auth)
	require.Equal(t, 200, rr.Code)

	// Quiet day: marked as sent, provider never called, no digest topic provisioned
	providerCalled := false
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		providerCalled = true
		return &ai.Response{Text: `{"headline": "x", "sections": []}`}, nil
	})
	s.sendDueBriefings()
	require.False(t, providerCalled)

	rr = request(t, s, "GET", "/v1/account", "", auth)
	require.Equal(t, 200, rr.Code)
	var account apiAccountResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&account))
	require.NotNil(t, account.Digest.LastDaily) // Marked sent even though nothing was delivered
}

func TestServer_AI_DigestScheduler_Timezone(t *testing.T) {
	// Berlin is UTC+2 in September: local hour 8 == 06:00 UTC
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"backups"}`, auth)
	request(t, s, "PUT", "/backups", "backup failed", nil)

	rr := request(t, s, "PATCH", "/v1/account/settings", `{"digest":{"enabled":true,"hour":8,"timezone":"Europe/Berlin"}}`, auth)
	require.Equal(t, 200, rr.Code)

	// 05:00 UTC = 07:00 Berlin: not due
	nowUTC := time.Date(2026, 9, 25, 5, 0, 0, 0, time.UTC)
	u, _ := s.userManager.User("phil")
	_, due := digestDue(u, nowUTC)
	require.False(t, due)

	// 06:00 UTC = 08:00 Berlin: due
	nowUTC = time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	_, due = digestDue(u, nowUTC)
	require.True(t, due)

	// Invalid timezone rejected
	rr = request(t, s, "PATCH", "/v1/account/settings", `{"digest":{"timezone":"Mars/Olympus"}}`, auth)
	require.Equal(t, 400, rr.Code)

	// Valid timezone stored, empty resets to UTC
	rr = request(t, s, "PATCH", "/v1/account/settings", `{"digest":{"timezone":"Asia/Tokyo"}}`, auth)
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "GET", "/v1/account", "", auth)
	var account apiAccountResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&account))
	require.Equal(t, "Asia/Tokyo", *account.Digest.Timezone)
	rr = request(t, s, "PATCH", "/v1/account/settings", `{"digest":{"timezone":""}}`, auth)
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "GET", "/v1/account", "", auth)
	account = apiAccountResponse{} // fresh decode target: absent JSON fields must not retain old values
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&account))
	freshUser, _ := s.userManager.User("phil")
	require.Nil(t, freshUser.Prefs.Digest.Timezone) // DB: timezone cleared
	require.Nil(t, account.Digest.Timezone)         // API: timezone cleared, other prefs preserved
	require.True(t, *account.Digest.Enabled)
}
