package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServer_MCP_Endpoint(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled = true
	c.AIProvider = "mock"
	c.EnableMCP = true
	s := newTestServer(t, c)
	defer s.closeDatabases()

	// initialize handshake through the axon server itself
	rr := request(t, s, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
	require.Equal(t, 200, rr.Code)
	var initResp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	require.Nil(t, json.NewDecoder(rr.Body).Decode(&initResp))
	require.Equal(t, "2024-11-05", initResp.Result.ProtocolVersion)
	require.Equal(t, "ntfy-mcp", initResp.Result.ServerInfo.Name)

	// tools/list through the axon server
	rr = request(t, s, "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), "publish")

	// Note: GET /mcp is served by the topic-poll route (single-segment path == topic
	// "mcp"), so no assertion here. The MCP transport is POST-only.
}

func TestServer_MCP_Disabled(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	s := newTestServer(t, c)
	defer s.closeDatabases()
	// With MCP disabled, POST /mcp falls through to the publish route, where topic
	// "mcp" is rejected as disallowed (axon reserves the topic name for the endpoint).
	rr := request(t, s, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, nil)
	require.Equal(t, 400, rr.Code)
}
