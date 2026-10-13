package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func grantTestAIConsent(t *testing.T, s *Server, username string) {
	t.Helper()
	u, err := s.userManager.User(username)
	require.NoError(t, err)
	require.NoError(t, s.userManager.GrantAIConsent(context.Background(), u.ID, s.aiConsentConfiguration()))
}

func consentAuth(username string) map[string]string {
	return map[string]string{"Authorization": util.BasicAuth(username, username), "Content-Type": "application/json"}
}

func newConsentTestServer(t *testing.T) *Server {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled, c.AIProvider = true, "mock"
	s := newTestServer(t, c)
	t.Cleanup(func() { s.closeDatabases() })
	for _, name := range []string{"alice", "bob"} {
		require.NoError(t, s.userManager.AddUser(name, name, user.RoleAdmin, false))
	}
	return s
}

func consentStatus(t *testing.T, s *Server, auth map[string]string) apiAIConsentResponse {
	t.Helper()
	rr := request(t, s, "GET", apiAIConsentPath, "", auth)
	require.Equal(t, 200, rr.Code, rr.Body.String())
	var status apiAIConsentResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&status))
	return status
}

func TestServer_AIConsent_OwnerGrantRevokeAndCachedResult(t *testing.T) {
	s := newConsentTestServer(t)
	auth := consentAuth("alice")
	status := consentStatus(t, s, auth)
	require.False(t, status.Consented)
	require.True(t, status.CanManage)
	require.Len(t, status.Configuration, 64)
	var calls atomic.Int32
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		calls.Add(1)
		return &ai.Response{Text: `{"subscriptions":[{"topic":"alerts"}]}`}, nil
	})
	rr := request(t, s, "POST", apiAIConsentPath, `{"accepted":true,"configuration":"`+status.Configuration+`"}`, auth)
	require.Equal(t, 200, rr.Code, rr.Body.String())
	status = consentStatus(t, s, auth)
	require.True(t, status.Consented)
	require.Positive(t, status.GrantedAt)
	require.False(t, consentStatus(t, s, consentAuth("bob")).Consented)
	for i := 0; i < 2; i++ {
		rr = request(t, s, "POST", "/v1/ai/plan", `{"prompt":"wish"}`, auth)
		require.Equal(t, 200, rr.Code, rr.Body.String())
	}
	require.Equal(t, int32(1), calls.Load(), "second result should come from cache")
	rr = request(t, s, "DELETE", apiAIConsentPath, "", auth)
	require.Equal(t, 200, rr.Code)
	require.False(t, consentStatus(t, s, auth).Consented)
	rr = request(t, s, "POST", "/v1/ai/plan", `{"prompt":"wish"}`, auth)
	require.Equal(t, 403, rr.Code, "revocation must block cached responses too")
	require.Equal(t, int32(1), calls.Load())
	// A supplied account ID is ignored: the authenticated owner is authoritative.
	alice, err := s.userManager.User("alice")
	require.NoError(t, err)
	rr = request(t, s, "POST", apiAIConsentPath, `{"accepted":true,"configuration":"`+status.Configuration+`","user_id":"`+alice.ID+`"}`, consentAuth("bob"))
	require.Equal(t, 200, rr.Code)
	require.False(t, consentStatus(t, s, auth).Consented)
	require.True(t, consentStatus(t, s, consentAuth("bob")).Consented)
}

func TestServer_AIConsent_ScopedAgentCannotConsentForOwner(t *testing.T) {
	s := newConsentTestServer(t)
	s.ai.Mock().EnqueueText(`{"subscriptions":[{"topic":"alerts"}]}`)
	u, err := s.userManager.User("alice")
	require.NoError(t, err)
	for _, scope := range []string{user.TokenScopeAI, user.TokenScopeRead, user.TokenScopeDevice} {
		t.Run(scope, func(t *testing.T) {
			token, err := s.userManager.CreateToken(u.ID, scope, time.Unix(0, 0), netip.IPv4Unspecified(), false, scope)
			require.NoError(t, err)
			auth := map[string]string{"Authorization": util.BearerAuth(token.Value), "Content-Type": "application/json"}
			if scope == user.TokenScopeAI {
				status := consentStatus(t, s, auth)
				require.False(t, status.CanManage)
			}
			for _, method := range []string{"POST", "DELETE"} {
				rr := request(t, s, method, apiAIConsentPath, `{"accepted":true,"configuration":"`+s.aiConsentConfiguration()+`"}`, auth)
				require.Equal(t, 403, rr.Code, rr.Body.String())
			}
			grantTestAIConsent(t, s, "alice")
			rr := request(t, s, "POST", "/v1/ai/plan", `{"prompt":"wish"}`, auth)
			if scope == user.TokenScopeAI {
				require.Equal(t, 200, rr.Code, rr.Body.String())
			} else {
				require.Equal(t, 403, rr.Code)
			}
		})
	}
	rr := request(t, s, "GET", apiAIConsentPath, "", nil)
	require.Equal(t, 401, rr.Code)
}

func TestServer_AIConsent_RejectsStaleAcceptanceAndCrossOriginWrites(t *testing.T) {
	s := newConsentTestServer(t)
	configuration := s.aiConsentConfiguration()
	for _, tc := range []struct {
		name, body, contentType, origin string
		want int
	}{
		{"not accepted", `{"accepted":false,"configuration":"`+configuration+`"}`, "application/json", "", 400},
		{"stale disclosure", `{"accepted":true,"configuration":"old"}`, "application/json", "", 409},
		{"form CSRF", `{"accepted":true,"configuration":"`+configuration+`"}`, "application/x-www-form-urlencoded", "", 400},
		{"text CSRF", `{"accepted":true,"configuration":"`+configuration+`"}`, "text/plain", "", 400},
		{"cross origin", `{"accepted":true,"configuration":"`+configuration+`"}`, "application/json", "https://evil.example", 403},
		{"null origin", `{"accepted":true,"configuration":"`+configuration+`"}`, "application/json", "null", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := consentAuth("alice")
			auth["Content-Type"], auth["Origin"] = tc.contentType, tc.origin
			rr := request(t, s, "POST", apiAIConsentPath, tc.body, auth)
			require.Equal(t, tc.want, rr.Code, rr.Body.String())
			require.False(t, consentStatus(t, s, consentAuth("alice")).Consented)
		})
	}
	auth := consentAuth("alice")
	auth["Origin"] = s.config.BaseURL
	rr := request(t, s, "POST", apiAIConsentPath, `{"accepted":true,"configuration":"`+configuration+`"}`, auth)
	require.Equal(t, 200, rr.Code, rr.Body.String())
	auth["Origin"] = "https://evil.example"
	rr = request(t, s, "DELETE", apiAIConsentPath, "", auth)
	require.Equal(t, 403, rr.Code)
	require.True(t, consentStatus(t, s, consentAuth("alice")).Consented)
}

func TestServer_AIConsent_DisclosureAndConfigurationChanges(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AIEnabled, c.AIProvider = true, "openai"
	c.AIBaseURL, c.AIAPIKey = "https://secret-user:secret-pass@provider.example/private/path?key=secret-query", "secret-api-key"
	c.AIModel, c.AIModelChat, c.AIEmbeddingsModel = "model-default", "model-chat", "model-embed"
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleAdmin, false))
	auth := consentAuth("alice")
	status := consentStatus(t, s, auth)
	require.Equal(t, "https://provider.example", status.Endpoint)
	require.Equal(t, []string{"model-chat", "model-default", "model-embed"}, status.Models)
	rr := request(t, s, "GET", apiAIConsentPath, "", auth)
	for _, secret := range []string{"secret-user", "secret-pass", "secret-query", "secret-api-key", "/private/path"} {
		require.NotContains(t, rr.Body.String(), secret)
	}
	grantTestAIConsent(t, s, "alice")
	makeClient := func(endpoint, key string) *ai.Client {
		client, err := ai.New(&ai.Config{Provider: "openai", BaseURL: endpoint, APIKey: key, Model: c.AIModel})
		require.NoError(t, err)
		return client
	}
	s.ai = makeClient(c.AIBaseURL, "rotated-api-key")
	require.Equal(t, status.Configuration, s.aiConsentConfiguration())
	require.NoError(t, s.initAIConsent())
	require.Equal(t, status.Configuration, s.aiConsentConfiguration())
	// Changing feature assignments must invalidate consent even when the model set is unchanged.
	c.AIModelDigest, c.AIModelChat = "model-chat", ""
	require.False(t, consentStatus(t, s, auth).Consented)
	c.AIModelDigest, c.AIModelChat = "", "model-chat"
	c.AIModelChat = "new-model"
	require.False(t, consentStatus(t, s, auth).Consented)
	c.AIModelChat = "model-chat"
	s.ai = makeClient("https://other-provider.example/v1", c.AIAPIKey)
	require.False(t, consentStatus(t, s, auth).Consented)
	rr = request(t, s, "POST", apiAIConsentPath, `{"accepted":true,"configuration":"`+status.Configuration+`"}`, auth)
	require.Equal(t, 409, rr.Code)
}

func TestServer_AIConsent_EnrichmentAndSchedulerRequireOwnerConsent(t *testing.T) {
	s := newConsentTestServer(t)
	s.config.AIEnrichmentEnabled = true
	s.config.AIEnrichTopics = []string{"alerts"}
	var calls atomic.Int32
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		calls.Add(1)
		return &ai.Response{Text: `{"summary":"AI title","headline":"Daily","sections":[]}`}, nil
	})
	auth := consentAuth("alice")
	for _, publisher := range []map[string]string{nil, auth} {
		rr := request(t, s, "PUT", "/alerts", strings.Repeat("private alert ", 20), publisher)
		require.Equal(t, 200, rr.Code)
		require.Empty(t, toMessages(t, rr.Body.String())[0].Title)
	}
	rr := request(t, s, "POST", "/v1/account/subscription", `{"base_url":"`+s.config.BaseURL+`","topic":"alerts"}`, auth)
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PATCH", "/v1/account/settings", fmt.Sprintf(`{"digest":{"enabled":true,"hour":%d}}`, time.Now().UTC().Hour()), auth)
	require.Equal(t, 200, rr.Code)
	s.sendDueBriefings()
	u, err := s.userManager.User("alice")
	require.NoError(t, err)
	require.Nil(t, u.Prefs.Digest.LastDaily)
	require.Zero(t, calls.Load())
	grantTestAIConsent(t, s, "alice")
	u, err = s.userManager.User("alice")
	require.NoError(t, err)
	pubToken, err := s.userManager.CreateToken(u.ID, "publisher", time.Unix(0, 0), netip.IPv4Unspecified(), false, user.TokenScopePublish)
	require.NoError(t, err)
	for _, publisher := range []map[string]string{nil, consentAuth("bob"), {"Authorization": util.BearerAuth(pubToken.Value)}} {
		rr = request(t, s, "PUT", "/alerts", strings.Repeat("unconsented private alert ", 20), publisher)
		require.Equal(t, 200, rr.Code)
		require.Empty(t, toMessages(t, rr.Body.String())[0].Title)
	}
	require.Zero(t, calls.Load(), "other requests on the same visitor must not borrow Alice's consent")
	rr = request(t, s, "PUT", "/alerts", strings.Repeat("distinct consenting alert ", 20), auth)
	require.Equal(t, 200, rr.Code)
	require.Equal(t, "AI title", toMessages(t, rr.Body.String())[0].Title)
	s.sendDueBriefings()
	require.Equal(t, int32(2), calls.Load())
	u, err = s.userManager.User("alice")
	require.NoError(t, err)
	require.NotNil(t, u.Prefs.Digest.LastDaily)
	require.NoError(t, s.userManager.RevokeAIConsent(context.Background(), u.ID))
	require.ErrorIs(t, s.sendDailyBriefing(u, time.Now().UTC().Format(digestDayFormat)), errHTTPForbiddenAIConsentRequired)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	allowed, err := s.hasAIConsent(ctx, u)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, allowed)
	require.Equal(t, int32(2), calls.Load())
}

func TestServer_AIConsent_DatabaseFailureDoesNotForwardContent(t *testing.T) {
	s := newConsentTestServer(t)
	grantTestAIConsent(t, s, "alice")
	var calls atomic.Int32
	s.ai.Mock().SetHandler(func(req *ai.Request) (*ai.Response, error) {
		calls.Add(1)
		return &ai.Response{Text: `{"subscriptions":[{"topic":"alerts"}]}`}, nil
	})
	u, err := s.userManager.User("alice")
	require.NoError(t, err)
	require.Equal(t, 200, request(t, s, "GET", apiAIConsentPath, "", consentAuth("alice")).Code)
	require.NoError(t, s.userManager.Close())
	allowed, err := s.hasAIConsent(context.Background(), u)
	require.Error(t, err)
	require.False(t, allowed)
	rr := request(t, s, "POST", "/v1/ai/plan", `{"prompt":"private wish"}`, consentAuth("alice"))
	require.GreaterOrEqual(t, rr.Code, 400)
	require.Zero(t, calls.Load())
}
