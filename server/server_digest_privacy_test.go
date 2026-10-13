package server

import (
	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
	"testing"
)

func TestServer_AI_DigestDelivery_PrivateWithOpenDefault(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AuthDefault = user.PermissionReadWrite
	s := newTestServer(t, c)
	defer s.closeDatabases()
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
	require.NoError(t, s.userManager.AddUser("bob", "bob", user.RoleUser, false))
	owner, err := s.userManager.User("alice")
	require.NoError(t, err)
	require.NoError(t, s.ensureDigestDelivery(owner))
	require.NoError(t, s.deliverBriefing(owner, &ai.BriefingResult{Headline: "Private account history"}))
	topic := *owner.Prefs.Digest.Topic
	for _, auth := range []map[string]string{nil, {"Authorization": util.BasicAuth("bob", "bob")}} {
		rr := request(t, s, "GET", "/"+topic+"/json?poll=1", "", auth)
		require.Equal(t, 403, rr.Code, "digest must be private even when ordinary topics are public")
	}
	rr := request(t, s, "GET", "/"+topic+"/json?poll=1", "", map[string]string{"Authorization": util.BasicAuth("alice", "alice")})
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), "Private account history")
	rr = request(t, s, "PUT", "/"+topic, "forged briefing", nil)
	require.Equal(t, 403, rr.Code)
	rr = request(t, s, "PUT", "/normal-topic", "normal publish", nil)
	require.Equal(t, 200, rr.Code, "ordinary topic behavior is unchanged")
}

func TestServer_AI_DigestDelivery_HardensExistingTopicsWhenAIDisabled(t *testing.T) {
	c := newTestConfigWithAuthFile(t, "")
	c.AuthDefault = user.PermissionReadWrite
	s := newTestServer(t, c)
	require.NoError(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
	owner, err := s.userManager.User("alice")
	require.NoError(t, err)
	require.NoError(t, s.ensureDigestDelivery(owner))
	topic := *owner.Prefs.Digest.Topic
	// Simulate the legacy owner-only ACL, with no scheduled delivery to repair it.
	require.NoError(t, s.userManager.ResetAccess(user.Everyone, topic))
	s.closeDatabases()
	c.AIEnabled = false
	s = newTestServer(t, c)
	defer s.closeDatabases()
	rr := request(t, s, "GET", "/"+topic+"/json?poll=1", "", nil)
	require.Equal(t, 403, rr.Code)
	rr = request(t, s, "GET", "/"+topic+"/json?poll=1", "", map[string]string{"Authorization": util.BasicAuth("alice", "alice")})
	require.Equal(t, 200, rr.Code)
	rr = request(t, s, "PUT", "/ordinary-topic", "ordinary publish", nil)
	require.Equal(t, 200, rr.Code)
}
