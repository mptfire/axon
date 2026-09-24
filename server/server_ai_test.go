package server

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func TestServer_AI_Disabled(t *testing.T) {
	s := newTestServer(t, newTestConfig(t, ""))
	defer s.closeDatabases()

	// AI endpoints are gone entirely when ai-enabled is not set
	rr := request(t, s, "GET", "/v1/ai/usage", "", nil)
	require.Equal(t, 400, rr.Code)
	require.Equal(t, 40059, toHTTPError(t, rr.Body.String()).Code)
}

func TestServer_AI_Usage(t *testing.T) {
	c := newTestConfig(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	c.AIModel = "test-model"
	s := newTestServer(t, c)
	defer s.closeDatabases()

	rr := request(t, s, "GET", "/v1/ai/usage", "", nil)
	require.Equal(t, 200, rr.Code)
	var report ai.UsageReport
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&report))
	require.Equal(t, int64(20000), report.VisitorBudget)
	require.Equal(t, int64(2000000), report.GlobalBudget)
	require.Equal(t, int64(0), report.Global.Total())
}

func TestServer_AI_Status_Admin(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		c := newTestConfigWithAuthFile(t, databaseURL)
		c.AIEnabled = true
		c.AIProvider = "mock"
		c.AIModel = "test-model"
		s := newTestServer(t, c)
		defer s.closeDatabases()
		require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
		require.Nil(t, s.userManager.AddUser("ben", "ben", user.RoleUser, false))

		// Admin sees provider health and usage
		rr := request(t, s, "GET", "/v1/ai/status", "", map[string]string{
			"Authorization": util.BasicAuth("phil", "phil"),
		})
		require.Equal(t, 200, rr.Code)
		var status apiAIStatusResponse
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&status))
		require.True(t, status.Enabled)
		require.Equal(t, "mock", status.Provider)
		require.Equal(t, "test-model", status.Model)
		require.True(t, status.Healthy)
		require.Equal(t, "", status.Health)

		// Non-admin and anonymous are rejected
		rr = request(t, s, "GET", "/v1/ai/status", "", map[string]string{
			"Authorization": util.BasicAuth("ben", "ben"),
		})
		require.Equal(t, 401, rr.Code)
		rr = request(t, s, "GET", "/v1/ai/status", "", nil)
		require.Equal(t, 401, rr.Code)
	})
}

func TestServer_AI_Status_Unhealthy(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	s.ai.Mock().SetPingError(errors.New("connection refused"))

	rr := request(t, s, "GET", "/v1/ai/status", "", map[string]string{
		"Authorization": util.BasicAuth("phil", "phil"),
	})
	require.Equal(t, 200, rr.Code)
	var status apiAIStatusResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&status))
	require.False(t, status.Healthy)
	require.Contains(t, status.Health, "connection refused")
}

func TestServer_AI_EnabledFlagInConfigResponse(t *testing.T) {
	// Off by default
	s := newTestServer(t, newTestConfig(t, ""))
	defer s.closeDatabases()
	rr := request(t, s, "GET", "/v1/config", "", nil)
	require.Equal(t, 200, rr.Code)
	var config apiConfigResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&config))
	require.False(t, config.EnableAI)

	// On with AI configured
	c := newTestConfig(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s2 := newTestServer(t, c)
	defer s2.closeDatabases()
	rr = request(t, s2, "GET", "/v1/config", "", nil)
	require.Equal(t, 200, rr.Code)
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&config))
	require.True(t, config.EnableAI)
}

func TestServer_AI_ClientWiring(t *testing.T) {
	// The nil client is safe and disabled
	c := newTestConfig(t, "")
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.False(t, s.ai.Enabled())
	require.Nil(t, s.ai.Mock())
	require.Equal(t, "", s.ai.ProviderName())
}
