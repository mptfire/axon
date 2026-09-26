// Package mcp implements a minimal Model Context Protocol (MCP) server over stdio,
// so AI agents (Claude, IDE agents, scripts) can use a ntfy server as their
// notification transport: publish messages, wait for messages, read history, list
// subscriptions, and generate subscription plans.
//
// The MCP layer is a thin client of the ntfy public HTTP API — it holds no server
// internals, inherits all rate limits, ACLs and budgets, and needs no extra
// dependencies. Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout (the
// MCP stdio transport).
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxHTTPBodyBytes bounds the MCP HTTP request body (generous; tool arguments are small).
const maxHTTPBodyBytes = 4 * 1024 * 1024

// ProtocolVersion is the MCP protocol version this server speaks.
const ProtocolVersion = "2024-11-05"

// ServerName identifies this MCP server in the initialize handshake.
const ServerName = "ntfy-mcp"

// Default timeouts
const (
	DefaultClientTimeout = 15 * time.Second
	DefaultWaitTimeout   = 60 * time.Second
	MaxWaitTimeout       = 5 * time.Minute
)

// Config configures the MCP server.
type Config struct {
	ServiceBaseURL string        // ntfy server base URL, e.g. https://ntfy.sh (required)
	AccessToken    string        // ntfy access token (tk_...), optional but needed for account tools
	ClientTimeout  time.Duration // HTTP timeout for publish/poll/plan calls; 0 = DefaultClientTimeout
	WaitMax        time.Duration // Hard cap for subscribe_wait; 0 = MaxWaitTimeout
	Version        string        // Reported in the initialize handshake
}

// Server is a stdio MCP server talking to one ntfy service.
type Server struct {
	config Config
	client *http.Client
}

// New creates a Server.
func New(conf Config) *Server {
	if conf.ClientTimeout <= 0 {
		conf.ClientTimeout = DefaultClientTimeout
	}
	if conf.WaitMax <= 0 {
		conf.WaitMax = MaxWaitTimeout
	}
	return &Server{
		config: conf,
		client: &http.Client{Timeout: conf.ClientTimeout},
	}
}

// authTokenKey is the context key for the caller's raw Authorization header value
// (HTTP transport). Empty means "no caller credentials" — stdio mode uses the
// server-wide configured token instead.
type authTokenKey struct{}

func withAuthToken(ctx context.Context, auth string) context.Context {
	if auth == "" {
		return ctx
	}
	return context.WithValue(ctx, authTokenKey{}, auth)
}

func authTokenFrom(ctx context.Context) string {
	if v, ok := ctx.Value(authTokenKey{}).(string); ok {
		return v
	}
	return ""
}

// rpcRequest is an incoming JSON-RPC 2.0 request or notification.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // Absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is an outgoing JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Serve runs the request loop until the input is exhausted or the context is
// cancelled. Responses are written newline-delimited to out. Malformed lines are
// answered with a parse error (or dropped for notifications, which carry no ID).
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	writer := bufio.NewWriter(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if err := writeResponse(writer, out, &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeParseError, Message: "parse error"}}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			continue // Notification: no response (e.g. notifications/initialized)
		}
		response := s.dispatch(ctx, &req)
		if err := writeResponse(writer, out, response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func writeResponse(writer *bufio.Writer, out io.Writer, response *rpcResponse) error {
	serialized, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("cannot marshal response: %w", err)
	}
	if _, err := writer.Write(append(serialized, '\n')); err != nil {
		return err
	}
	return writer.Flush()
}

// HTTPHandler implements the MCP streamable HTTP transport on top of the same tool
// dispatch used by stdio. Clients POST JSON-RPC messages (single object or batch);
// responses come back as JSON. Notification-only requests return HTTP 202. Unlike
// stdio, auth is per-request: pass your ntfy access token as `Authorization: Bearer tk_...`
// (or Basic) and every tool call is made with those credentials.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "MCP endpoint: use POST with a JSON-RPC message", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHTTPBodyBytes))
	if err != nil {
		http.Error(w, "cannot read request body", http.StatusBadRequest)
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}
	authHeader := r.Header.Get("Authorization")
	ctx := withAuthToken(r.Context(), authHeader)
	// Batch: JSON array of messages. Responses (in order, notifications excluded) as a JSON array.
	if trimmed[0] == '[' {
		var requests []rpcRequest
		if err := json.Unmarshal(trimmed, &requests); err != nil {
			http.Error(w, "parse error", http.StatusBadRequest)
			return
		}
		responses := make([]*rpcResponse, 0, len(requests))
		for i := range requests {
			if len(requests[i].ID) == 0 {
				continue
			}
			responses = append(responses, s.dispatch(ctx, &requests[i]))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(trimmed, &req); err != nil {
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted) // Notification-only request
		return
	}
	_ = json.NewEncoder(w).Encode(s.dispatch(ctx, &req))
}

// dispatch routes a single request to its handler.
func (s *Server) dispatch(ctx context.Context, req *rpcRequest) *rpcResponse {
	response := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": ServerName, "version": s.config.Version},
		}
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{"tools": toolDefinitions()}
	case "tools/call":
		response.Result = s.callTool(ctx, req.Params)
	default:
		response.Error = &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}
	return response
}

// toolResult models the MCP tools/call result.
type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textResult(text string) *toolResult {
	return &toolResult{Content: []toolContent{{Type: "text", Text: text}}}
}

func errorResult(err error) *toolResult {
	return &toolResult{Content: []toolContent{{Type: "text", Text: err.Error()}}, IsError: true}
}

// callTool executes a tools/call request. Tool errors are reported as isError results
// (per the MCP spec), never as JSON-RPC protocol errors.
func (s *Server) callTool(ctx context.Context, params json.RawMessage) *toolResult {
	var request struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return errorResult(fmt.Errorf("invalid tools/call params: %w", err))
	}
	var args map[string]any
	if len(request.Arguments) > 0 {
		if err := json.Unmarshal(request.Arguments, &args); err != nil {
			return errorResult(fmt.Errorf("invalid tool arguments: %w", err))
		}
	}
	switch request.Name {
	case "publish":
		return s.toolPublish(ctx, args)
	case "read_messages":
		return s.toolReadMessages(ctx, args)
	case "subscribe_wait":
		return s.toolSubscribeWait(ctx, args)
	case "ask_history":
		return s.toolAskHistory(ctx, args)
	case "digest_topic":
		return s.toolDigestTopic(ctx, args)
	case "briefing":
		return s.toolBriefing(ctx, args)
	case "list_subscriptions":
		return s.toolListSubscriptions(ctx, args)
	case "plan_subscription":
		return s.toolPlanSubscription(ctx, args)
	default:
		return errorResult(fmt.Errorf("unknown tool: %s", request.Name))
	}
}
