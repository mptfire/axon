package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func TestServer_Authentication_RequestKeepsItsAccount(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
	require.NoError(t, s.userManager.AddUser("bob", "bob", user.RoleUser, false))
	alice, _ := http.NewRequest("GET", "/v1/account", nil)
	alice.RemoteAddr = "192.0.2.1:1234"
	alice.Header.Set("Authorization", util.BasicAuth("alice", "alice"))
	alice, v, err := s.maybeAuthenticate(alice)
	require.NoError(t, err)
	bob, _ := http.NewRequest("GET", "/v1/account", nil)
	bob.RemoteAddr = alice.RemoteAddr
	bob.Header.Set("Authorization", util.BasicAuth("bob", "bob"))
	_, _, err = s.maybeAuthenticate(bob) // deterministic interleaving from the same IP
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	require.NoError(t, s.handleInternal(rr, alice, v))
	var account apiAccountResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &account))
	require.Equal(t, "alice", account.Username)
}

func TestServer_Authentication_DeviceScopeSurvivesOtherRequest(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleAdmin, false))
	owner, err := s.userManager.User("alice")
	require.NoError(t, err)
	token, err := s.userManager.CreateToken(owner.ID, "device", time.Unix(0, 0), netip.IPv4Unspecified(), false, user.TokenScopeDevice)
	require.NoError(t, err)
	device, _ := http.NewRequest("GET", "/v1/users", nil)
	device.RemoteAddr = "192.0.2.1:1234"
	device.Header.Set("Authorization", "Bearer "+token.Value)
	device, v, err := s.maybeAuthenticate(device)
	require.NoError(t, err)
	admin, _ := http.NewRequest("GET", "/v1/account", nil)
	admin.RemoteAddr = device.RemoteAddr
	admin.Header.Set("Authorization", util.BasicAuth("alice", "alice"))
	_, _, err = s.maybeAuthenticate(admin)
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	require.Equal(t, errHTTPForbidden, s.handleInternal(rr, device, v))
}

func TestServer_Authentication_AnonymousRequestStaysAnonymous(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
	anon, _ := http.NewRequest("GET", "/v1/account", nil)
	anon.RemoteAddr = "192.0.2.1:1234"
	anon, v, err := s.maybeAuthenticate(anon)
	require.NoError(t, err)
	auth, _ := http.NewRequest("GET", "/v1/account", nil)
	auth.RemoteAddr = anon.RemoteAddr
	auth.Header.Set("Authorization", util.BasicAuth("alice", "alice"))
	_, _, err = s.maybeAuthenticate(auth)
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	require.NoError(t, s.handleInternal(rr, anon, v))
	var account apiAccountResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &account))
	require.Equal(t, user.Everyone, account.Username)
	require.Empty(t, account.Tokens)
}

func TestServer_Authentication_AIScopeSurvivesOtherRequest(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
	owner, err := s.userManager.User("alice")
	require.NoError(t, err)
	token, err := s.userManager.CreateToken(owner.ID, "read only", time.Unix(0, 0), netip.IPv4Unspecified(), false, user.TokenScopeRead)
	require.NoError(t, err)
	limited, _ := http.NewRequest("POST", "/v1/ai/plan", nil)
	limited.RemoteAddr = "192.0.2.1:1234"
	limited.Header.Set("Authorization", "Bearer "+token.Value)
	limited, v, err := s.maybeAuthenticate(limited)
	require.NoError(t, err)
	unrestricted, _ := http.NewRequest("GET", "/v1/account", nil)
	unrestricted.RemoteAddr = limited.RemoteAddr
	unrestricted.Header.Set("Authorization", util.BasicAuth("alice", "alice"))
	_, _, err = s.maybeAuthenticate(unrestricted)
	require.NoError(t, err)
	handler := s.ensureAIScope(func(http.ResponseWriter, *http.Request, *visitor) error { return nil })
	require.Equal(t, errHTTPForbidden, handler(httptest.NewRecorder(), limited, v))
}

func TestServer_Authentication_AnonymousTokenUpdate(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	s := newTestServer(t, c)
	defer s.closeDatabases()
	r, _ := http.NewRequest("PATCH", "/v1/account/token", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r, v, err := s.maybeAuthenticate(r)
	require.NoError(t, err)
	require.Equal(t, errHTTPUnauthorized, s.handleAccountTokenUpdate(httptest.NewRecorder(), r, v))
}
