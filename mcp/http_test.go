package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// httpPOST posts a raw JSON-RPC payload to the MCP HTTP handler.
func httpPOST(t *testing.T, handler http.Handler, payload string, headers map[string]string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	handler.ServeHTTP(rr, req)
	return rr
}

func TestHTTPHandler_Initialize(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh", Version: "test"})
	rr := httpPOST(t, s.HTTPHandler(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
	require.Equal(t, 200, rr.Code)
	var response map[string]any
	require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &response))
	result := response["result"].(map[string]any)
	require.Equal(t, ProtocolVersion, result["protocolVersion"])
	require.Contains(t, rr.Header().Get("Content-Type"), "application/json")
}

func TestHTTPHandler_NotificationOnlyReturns202(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	rr := httpPOST(t, s.HTTPHandler(), `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	require.Equal(t, http.StatusAccepted, rr.Code)
	require.Equal(t, "", rr.Body.String())
}

func TestHTTPHandler_Batch(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	payload := `[
		{"jsonrpc":"2.0","method":"notifications/initialized"},
		{"jsonrpc":"2.0","id":"a","method":"ping"},
		{"jsonrpc":"2.0","id":"b","method":"tools/list"}
	]`
	rr := httpPOST(t, s.HTTPHandler(), payload, nil)
	require.Equal(t, 200, rr.Code)
	var responses []map[string]any
	require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &responses))
	require.Len(t, responses, 2) // notification excluded
	require.Equal(t, "a", responses[0]["id"])
	require.Equal(t, "b", responses[1]["id"])
}

func TestHTTPHandler_GETRejected(t *testing.T) {
	s := New(Config{ServiceBaseURL: "https://ntfy.sh"})
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/mcp", nil)
	s.HTTPHandler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

func TestHTTPHandler_Publish(t *testing.T) {
	var seenPath, seenBody, seenAuth string
	fake := fakeNtfy(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPut {
			seenPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			seenBody = string(b)
			seenAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"id":"XYZ"}`))
			return true
		}
		return false
	})
	defer fake.Close()
	s := New(Config{ServiceBaseURL: fake.URL})

	rr := httpPOST(t, s.HTTPHandler(), `{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"publish","arguments":{"topic":"alerts","message":"disk full","priority":5}}}`, map[string]string{
		"Authorization": "Bearer tk_caller",
	})
	require.Equal(t, 200, rr.Code)
	require.False(t, isErrResp(t, rr))
	require.Contains(t, rr.Body.String(), "alerts")
	require.Equal(t, "/alerts", seenPath)
	require.Equal(t, "disk full", seenBody)
	require.Equal(t, "Bearer tk_caller", seenAuth)
}

// isErrResp reports whether a tools/call response carries an error result.
func isErrResp(t *testing.T, rr *httptest.ResponseRecorder) bool {
	t.Helper()
	var parsed struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &parsed)
	return parsed.Result.IsError
}
