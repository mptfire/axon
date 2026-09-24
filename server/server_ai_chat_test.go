package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func TestServer_AI_Chat(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

	// Gates: anonymous and unsubscribed
	rr := request(t, s, "POST", "/v1/ai/chat", `{"topic":"backups","question":"what failed?"}`, nil)
	require.Equal(t, 401, rr.Code)
	rr = request(t, s, "POST", "/v1/ai/chat", `{"topic":"backups","question":"what failed?"}`, auth)
	require.Equal(t, 403, rr.Code)

	// Subscribe and seed history
	rr = request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"backups"}`, auth)
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PUT", "/backups", "backup failed exit 2 on db-1", map[string]string{"Title": "failure"})
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PUT", "/backups", "hello world from the new deployment", nil)
	require.Equal(t, 200, rr.Code)

	// The retrieval must pick the failure message; the model only sees that window
	var seenPrompt string
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		require.Equal(t, ai.FeatureChat, req.Feature)
		seenPrompt = req.Prompt
		// "invented-id" is not in the context: the server must strip it
		return &ai.Response{Text: `{"answer": "The last failure was db-1 with exit code 2.", "citations": ["invented-id"]}`}, nil
	})
	rr = request(t, s, "POST", "/v1/ai/chat", `{"topic":"backups","question":"what failed on db-1?"}`, auth)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, seenPrompt, "backup failed exit 2 on db-1")

	var chatResponse apiAIChatResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&chatResponse))
	require.Equal(t, "The last failure was db-1 with exit code 2.", chatResponse.Answer)
	require.Empty(t, chatResponse.Citations) // invented citation stripped
	require.Equal(t, ai.ChatDisclaimer, chatResponse.Disclaimer)
}

func TestServer_AI_Chat_EmptyWindow(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"quiet-topic"}`, auth)

	// No provider handler set: if the digester/chat were called it would return the echo
	// (non-JSON) and fail. Empty window must short-circuit.
	rr := request(t, s, "POST", "/v1/ai/chat", `{"topic":"quiet-topic","question":"anything?"}`, auth)
	require.Equal(t, 200, rr.Code)
	var chatResponse apiAIChatResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&chatResponse))
	require.Contains(t, chatResponse.Answer, "no messages")
}

func TestServer_AI_Chat_InvalidQuestion(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"backups"}`, auth)

	rr := request(t, s, "POST", "/v1/ai/chat", `{"topic":"backups","question":"   "}`, auth)
	require.Equal(t, 400, rr.Code)
	rr = request(t, s, "POST", "/v1/ai/chat", `{"topic":"backups","question":"what?","since":"nonsense"}`, auth)
	require.Equal(t, 400, rr.Code)
}
