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

	// Follow-up question with conversation history: history is forwarded to the model
	rr = request(t, s, "POST", "/v1/ai/chat", `{"topic":"backups","question":"how often?","history":[{"question":"what failed?","answer":"db-1, twice"}]}`, auth)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, seenPrompt, "Earlier in this conversation:")
	require.Contains(t, seenPrompt, "Q: what failed?")

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

func TestServer_AI_Chat_AllTopics(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	for _, topic := range []string{"backups", "home-alerts"} {
		rr := request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"`+topic+`"}`, auth)
		require.Equal(t, 200, rr.Code)
	}
	request(t, s, "PUT", "/backups", "backup failed exit 2", nil)
	request(t, s, "PUT", "/home-alerts", "garage door open", nil)

	var seenPrompt string
	var seenTopicParam string
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		seenPrompt = req.Prompt
		seenTopicParam = req.Prompt[:0]
		_ = seenTopicParam
		return &ai.Response{Text: `{"answer": "Backup failed and the garage is open.", "citations": []}`}, nil
	})

	// Cross-topic question: messages from both topics, each tagged with its topic
	rr := request(t, s, "POST", "/v1/ai/chat", `{"all":true,"question":"anything broken at home or in backups?"}`, auth)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, seenPrompt, "topic=backups")
	require.Contains(t, seenPrompt, "topic=home-alerts")
	require.Contains(t, seenPrompt, "backup failed exit 2")
	require.Contains(t, seenPrompt, "garage door open")

	var chatResponse apiAIChatResponse
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&chatResponse))
	require.Equal(t, "Backup failed and the garage is open.", chatResponse.Answer)

	// all + topic together: invalid
	rr = request(t, s, "POST", "/v1/ai/chat", `{"all":true,"topic":"backups","question":"q"}`, auth)
	require.Equal(t, 400, rr.Code)
}

func TestServer_AI_ChatStream(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"backups"}`, auth)
	request(t, s, "PUT", "/backups", "backup failed exit 2", map[string]string{"Title": "failure"})

	// The mock streams in [n]-citing plain text; markers resolve to real messages.
	// NOTE: no require inside the handler — FailNow from the mock goroutine would
	// deadlock the stream. Assertions on the request happen after the fact.
	var seenSystem string
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		seenSystem = req.System
		return &ai.Response{Text: "The failure at [1] was exit code 2. Also see [9]."}, nil
	})

	rr := request(t, s, "POST", "/v1/ai/chat/stream", `{"topic":"backups","question":"what failed?"}`, auth)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "text/event-stream")
	require.NotContains(t, seenSystem, "single JSON object") // streamed answers are plain text

	body := rr.Body.String()
	require.Contains(t, body, `"type":"delta"`)
	require.Contains(t, body, `"type":"citations"`)
	require.Contains(t, body, `"type":"done"`)
	require.Contains(t, body, "backup failed exit 2") // citation carries the real message
	// The citations event's text has the out-of-range marker [9] stripped
	citationsStart := strings.LastIndex(body, `data: {"citations":`)
	require.GreaterOrEqual(t, citationsStart, 0)
	citationsLine := body[citationsStart:]
	require.Contains(t, citationsLine, "Also see .")
	require.NotContains(t, citationsLine, "[9]")
}

func TestServer_AI_ChatStream_Gates(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

	// Anonymous: 401; unsubscribed: 403
	rr := request(t, s, "POST", "/v1/ai/chat/stream", `{"topic":"backups","question":"q"}`, nil)
	require.Equal(t, 401, rr.Code)
	rr = request(t, s, "POST", "/v1/ai/chat/stream", `{"topic":"backups","question":"q"}`, auth)
	require.Equal(t, 403, rr.Code)

	// Empty window: streamed delta without provider call
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"quiet"}`, auth)
	rr = request(t, s, "POST", "/v1/ai/chat/stream", `{"topic":"quiet","question":"q"}`, auth)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), "no messages")
}

func TestServer_AI_Chat_HybridRetrieval(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	c.AIEmbeddingsModel = "nomic-embed-test"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NotNil(t, s.embedder) // wired when ai-embeddings-model is set
	require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
	auth := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}
	request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"ops"}`, auth)
	request(t, s, "PUT", "/ops", "the database migration failed at 03:00", map[string]string{"Title": "migration failure"})
	request(t, s, "PUT", "/ops", "someone pushed a poetry blog post", nil)

	// Semantic embedding: "migration" text scores high, poetry low; the embedding path
	// must be exercised (cost accounting) and the prompt must contain the right message.
	var promptSeen string
	embedCalls := 0
	s.ai.Mock().SetEmbedHandler(func(text string) ([]float32, error) {
		embedCalls++
		if strings.Contains(text, "migration failed") {
			return []float32{0.95, 0.1}, nil
		}
		return []float32{0.05, 0.05}, nil
	})
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		promptSeen = req.Prompt
		return &ai.Response{Text: `{"answer": "The migration failed.", "citations": []}`}, nil
	})

	rr := request(t, s, "POST", "/v1/ai/chat", `{"topic":"ops","question":"what happened with the migration?"}`, auth)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, promptSeen, "migration failure")
	require.Greater(t, embedCalls, 0)
}
