package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/user"
)

func TestServer_ScopedToken_CannotEscalate(t *testing.T) {
	for _, scope := range []string{user.TokenScopeRead, user.TokenScopePublish, user.TokenScopeAI} {
		t.Run(scope, func(t *testing.T) {
			c := newTestConfigWithAuthFile(t, "")
			s := newTestServer(t, c)
			defer s.closeDatabases()
			require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleAdmin, false))
			u, err := s.userManager.User("alice")
			require.NoError(t, err)
			_, err = s.userManager.CreateToken(u.ID, "unrestricted", time.Unix(0, 0), netip.IPv4Unspecified(), false, "")
			require.NoError(t, err)
			token, err := s.userManager.CreateToken(u.ID, "agent", time.Unix(0, 0), netip.IPv4Unspecified(), false, scope)
			require.NoError(t, err)
			for _, path := range []string{"/v1/account/token", "/v1/account/login", "/v1/device/pairing", "/v1/users"} {
				t.Run(path, func(t *testing.T) {
					r, _ := http.NewRequest("POST", path, strings.NewReader("{}"))
					r.RemoteAddr = "192.0.2.1:1234"
					r.Header.Set("Authorization", "Bearer "+token.Value)
					r, v, err := s.maybeAuthenticate(r)
					require.NoError(t, err)
					require.Equal(t, errHTTPForbidden, s.handleInternal(httptest.NewRecorder(), r, v), path)
				})
			}
			r, _ := http.NewRequest("GET", "/v1/account", nil)
			r.RemoteAddr = "192.0.2.1:1234"
			r.Header.Set("Authorization", "Bearer "+token.Value)
			r, v, err := s.maybeAuthenticate(r)
			require.NoError(t, err)
			rr := httptest.NewRecorder()
			require.NoError(t, s.handleInternal(rr, r, v))
			var account apiAccountResponse
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &account))
			require.Equal(t, "alice", account.Username)
			require.Empty(t, account.Tokens)
		})
	}
}
