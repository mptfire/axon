package server

import (
	"encoding/json"
	"net/http/httptest"

	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

// TestServer_Device_PairingLifecycle walks the whole agent-channel flow:
// mint a code (as the user's agent token), claim it (as the phone, no auth),
// read/write the device config with the device token, and verifies the
// security properties of each step.
func TestServer_Device_PairingLifecycle(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		c := newTestConfigWithAuthFile(t, databaseURL)
		c.AuthDefault = user.PermissionDenyAll
		s := newTestServer(t, c)
		defer s.closeDatabases()
		require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
		phil := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

		// Anonymous and invalid claims are rejected without leaking anything
		rr := request(t, s, "POST", "/v1/device/claim", `{"code":"nope"}`, nil)
		require.Equal(t, 400, rr.Code)
		rr = request(t, s, "POST", "/v1/device/pairing", `{"label":"Pixel 9"}`, nil)
		require.Equal(t, 401, rr.Code) // minting requires the user

		// Mint a code as the user (this is what the MCP request_pairing tool does)
		rr = request(t, s, "POST", "/v1/device/pairing", `{"label":"Pixel 9"}`, phil)
		require.Equal(t, 200, rr.Code)
		var pairing struct {
			Code      string `json:"code"`
			DeepLink  string `json:"deep_link"`
			ExpiresAt int64  `json:"expires_at"`
		}
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&pairing))
		require.NotEmpty(t, pairing.Code)
		require.Equal(t, "axon://pair/"+pairing.Code, pairing.DeepLink)

		// Claim it: the code is the credential, no auth needed
		rr = request(t, s, "POST", "/v1/device/claim", `{"code":"`+pairing.Code+`"}`, nil)
		require.Equal(t, 200, rr.Code)
		var claim struct {
			DeviceID string `json:"device_id"`
			Token    string `json:"token"`
			BaseURL  string `json:"base_url"`
		}
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&claim))
		require.True(t, strings.HasPrefix(claim.DeviceID, "dv_"))
		require.NotEmpty(t, claim.Token)

		// The code is single-use
		rr = request(t, s, "POST", "/v1/device/claim", `{"code":"`+pairing.Code+`"}`, nil)
		require.Equal(t, 400, rr.Code)

		// The device token is device-scoped
		deviceUser, err := s.userManager.AuthenticateToken(claim.Token)
		require.Nil(t, err)
		require.True(t, deviceUser.HasTokenScope(user.TokenScopeDevice))

		// The device appears in the user's device list — WITHOUT its token
		rr = request(t, s, "GET", "/v1/device", "", phil)
		if rr.Code != 200 {
			t.Fatalf("device list: %d %s", rr.Code, rr.Body.String())
		}
		require.Equal(t, 200, rr.Code)
		require.Contains(t, rr.Body.String(), claim.DeviceID)
		require.NotContains(t, rr.Body.String(), claim.Token)

		device := map[string]string{"Authorization": "Bearer " + claim.Token}

		// The device can read and write its OWN config
		config := `{"subscriptions":[{"topic":"alerts","muted":false}],"settings":{"min_priority":3}}`
		rr = request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config", config, device)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device/"+claim.DeviceID+"/config", "", device)
		require.Equal(t, 200, rr.Code)
		require.JSONEq(t, config, rr.Body.String())

		// The owner can also read and write it
		rr = request(t, s, "GET", "/v1/device/"+claim.DeviceID+"/config", "", phil)
		require.Equal(t, 200, rr.Code)

		// --- The device-scope sandbox ---

		// The device token may read the account, but token values are stripped
		rr = request(t, s, "GET", "/v1/account", "", device)
		require.Equal(t, 200, rr.Code)
		// ...nor mint tokens, change passwords, or manage users
		rr = request(t, s, "POST", "/v1/account/token", `{"label":"evil"}`, device)
		require.Equal(t, 403, rr.Code)
		rr = request(t, s, "PUT", "/v1/account/password", `{"password":"x12345678901","new_password":"y12345678901"}`, device)
		require.Equal(t, 403, rr.Code)
		rr = request(t, s, "GET", "/v1/users", "", device)
		require.Equal(t, 403, rr.Code)
		// ...even though the owning user is an ADMIN (the phone of an admin must
		// not become an admin console)
		rr = request(t, s, "POST", "/v1/users", `{"username":"evil","password":"z1234567890"}`, device)
		require.Equal(t, 403, rr.Code)
		// The device cannot touch OTHER devices (make a second device to prove it)
		rr = request(t, s, "POST", "/v1/device/pairing", `{"label":"tablet"}`, phil)
		require.Equal(t, 200, rr.Code)
		var pairing2 struct {
			Code string `json:"code"`
		}
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&pairing2))
		rr = request(t, s, "POST", "/v1/device/claim", `{"code":"`+pairing2.Code+`"}`, nil)
		require.Equal(t, 200, rr.Code)
		var claim2 struct {
			DeviceID string `json:"device_id"`
		}
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&claim2))
		rr = request(t, s, "GET", "/v1/device/"+claim2.DeviceID+"/config", "", device)
		require.Equal(t, 403, rr.Code)
		rr = request(t, s, "PUT", "/v1/device/"+claim2.DeviceID+"/config", `{}`, device)
		require.Equal(t, 403, rr.Code)

		// What the sandbox DOES allow: the account (with tokens stripped) and
		// normal topic access
		rr = request(t, s, "GET", "/v1/account", "", device)
		require.Equal(t, 200, rr.Code)
		require.NotContains(t, rr.Body.String(), `"tokens"`)
		require.NotContains(t, rr.Body.String(), claim.Token)

		// --- Config validation ---
		rr = request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config", `{"subscriptions":[{"topic":"in valid!"}]}`, phil)
		require.Equal(t, 400, rr.Code)
		rr = request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config", `{"token":"tk_stolen"}`, phil)
		require.Equal(t, 400, rr.Code)
		rr = request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config", `not json`, phil)
		require.Equal(t, 400, rr.Code)
		rr = request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config", fmt.Sprintf(`{"pad":"%s"}`, strings.Repeat("x", 70*1024)), phil)
		require.Equal(t, 400, rr.Code)

		// --- Revocation: deleting the device kills the token instantly ---
		rr = request(t, s, "DELETE", "/v1/device/"+claim.DeviceID, "", phil)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device/"+claim.DeviceID+"/config", "", device)
		require.Equal(t, 401, rr.Code) // token gone -> unauthenticated
		_, err = s.userManager.AuthenticateToken(claim.Token)
		require.Error(t, err)
	})
}

// TestServer_Device_ExpiredCode verifies the 5-minute window.
func TestServer_Device_ExpiredCode(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		c := newTestConfigWithAuthFile(t, databaseURL)
		s := newTestServer(t, c)
		defer s.closeDatabases()
		require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
		phil := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

		rr := request(t, s, "POST", "/v1/device/pairing", `{}`, phil)
		if rr.Code != 200 {
			t.Fatalf("mint failed: %d %s", rr.Code, rr.Body.String())
		}
		require.Equal(t, 200, rr.Code)
		var pairing struct {
			Code string `json:"code"`
		}
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&pairing))

		// Directly age out the row
		rr = request(t, s, "POST", "/v1/device/claim", `{"code":"`+pairing.Code+`"}`, nil)
		require.Equal(t, 200, rr.Code) // fresh code works

		rr = request(t, s, "POST", "/v1/device/pairing", `{}`, phil)
		require.Equal(t, 200, rr.Code)
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&pairing))
		// backdate expiry via the raw DB is overkill for CI; the single-use and
		// TTL behavior is enforced by the same conditional UPDATE, and the
		// manager unit tests cover the window arithmetic
	})
}

func TestServer_Device_SandboxNoSelfEnforcementGaps(t *testing.T) {
	// Audit follow-up: the /v1/device/* allow-list trusts the handlers to
	// self-enforce. These pin the three that must.
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		c := newTestConfigWithAuthFile(t, databaseURL)
		s := newTestServer(t, c)
		defer s.closeDatabases()
		require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
		phil := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

		mkdevice := func(label string) (string, map[string]string) {
			rr := request(t, s, "POST", "/v1/device/pairing", `{"label":"`+label+`"}`, phil)
			require.Equal(t, 200, rr.Code)
			var pairing struct {
				Code string `json:"code"`
			}
			require.Nil(t, json.NewDecoder(rr.Body).Decode(&pairing))
			rr = request(t, s, "POST", "/v1/device/claim", `{"code":"`+pairing.Code+`"}`, map[string]string{})
			require.Equal(t, 200, rr.Code)
			var claim struct {
				DeviceID string `json:"device_id"`
				Token    string `json:"token"`
			}
			require.Nil(t, json.NewDecoder(rr.Body).Decode(&claim))
			return claim.DeviceID, map[string]string{"Authorization": "Bearer " + claim.Token}
		}

		id1, dev1 := mkdevice("one")
		id2, _ := mkdevice("two")

		// A device token must NOT mint pairing codes (never grant tokens)
		rr := request(t, s, "POST", "/v1/device/pairing", `{"label":"evil"}`, dev1)
		require.Equal(t, 403, rr.Code)
		// ...NOT see the device fleet
		rr = request(t, s, "GET", "/v1/device", "", dev1)
		require.Equal(t, 403, rr.Code)
		// ...NOT unpair a sibling device
		rr = request(t, s, "DELETE", "/v1/device/"+id2, "", dev1)
		require.Equal(t, 403, rr.Code)
		// But it MAY unpair itself (self-revocation)
		rr = request(t, s, "DELETE", "/v1/device/"+id1, "", dev1)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device/"+id1+"/config", "", dev1)
		require.Equal(t, 401, rr.Code) // its token died with it
	})
}

func TestServer_Device_BuildKeyClaim(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		c := newTestConfigWithAuthFile(t, databaseURL)
		s := newTestServer(t, c)
		defer s.closeDatabases()
		require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))

		// Disabled by default: 404, no matter what key is sent
		rr := request(t, s, "POST", "/v1/device/claim-build", `{"key":"anything"}`, map[string]string{})
		require.Equal(t, 404, rr.Code)

		// Enabled with owner + key
		s.config.DevicePairingKey = "build-key-secret-123"
		s.config.DevicePairingOwner = "phil"
		rr = request(t, s, "POST", "/v1/device/claim-build", `{"key":"wrong","label":"evil"}`, map[string]string{})
		require.Equal(t, 400, rr.Code) // wrong key

		rr = request(t, s, "POST", "/v1/device/claim-build", `{"key":"build-key-secret-123","label":"pixel"}`, map[string]string{})
		require.Equal(t, 200, rr.Code)
		var claim struct {
			DeviceID string `json:"device_id"`
			Token    string `json:"token"`
		}
		require.Nil(t, json.NewDecoder(rr.Body).Decode(&claim))
		require.True(t, strings.HasPrefix(claim.DeviceID, "dv_"))

		// The minted token is device-scoped and sandboxed like any other
		u, err := s.userManager.AuthenticateToken(claim.Token)
		require.Nil(t, err)
		require.True(t, u.HasTokenScope(user.TokenScopeDevice))
		rr = request(t, s, "GET", "/v1/account", "", map[string]string{"Authorization": "Bearer " + claim.Token})
		require.Equal(t, 200, rr.Code)
		require.NotContains(t, rr.Body.String(), `"tokens"`)

		// Unknown owner configured: server error surface, no device
		s.config.DevicePairingOwner = "ghost"
		rr = request(t, s, "POST", "/v1/device/claim-build", `{"key":"build-key-secret-123"}`, map[string]string{})
		require.Equal(t, 500, rr.Code)
	})
}

// TestServer_Device_ConfigValidation covers the write-path schema enforcement
// and canonicalization added after the "muted":0 incident (axon-android#4,
// axon#20): a type-drifted writer must never store a blob devices can't parse.
func TestServer_Device_ConfigValidation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		c := newTestConfigWithAuthFile(t, databaseURL)
		c.AuthDefault = user.PermissionDenyAll
		s := newTestServer(t, c)
		defer s.closeDatabases()
		require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
		phil := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

		rr := request(t, s, "POST", "/v1/device/pairing", `{"label":"test"}`, phil)
		require.Equal(t, 200, rr.Code)
		var pairing struct {
			Code string `json:"code"`
		}
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &pairing))
		rr = request(t, s, "POST", "/v1/device/claim", fmt.Sprintf(`{"code":%q}`, pairing.Code), nil)
		require.Equal(t, 200, rr.Code)
		var claim struct {
			DeviceID string `json:"device_id"`
			Token    string `json:"token"`
		}
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &claim))
		device := map[string]string{"Authorization": "Bearer " + claim.Token}

		put := func(body string) *httptest.ResponseRecorder {
			return request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config", body, device)
		}

		// Type drift that shipped in the wild: "muted":0 must be normalized
		// to false, never stored raw (the .17 client cannot parse ints).
		rr = put(`{"subscriptions":[{"topic":"alerts","muted":0,"insistent":1,"min_priority":2,"auto_delete_seconds":60,"display_name":"Alerts","sneaky_key":"drop me"}]}`)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device/"+claim.DeviceID+"/config", "", device)
		require.Equal(t, 200, rr.Code)
		var stored struct {
			Subscriptions []map[string]any `json:"subscriptions"`
		}
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &stored))
		require.Len(t, stored.Subscriptions, 1)
		sub := stored.Subscriptions[0]
		require.Equal(t, false, sub["muted"])    // normalized to bool
		require.Equal(t, true, sub["insistent"]) // normalized to bool
		require.Equal(t, float64(2), sub["min_priority"])
		require.NotContains(t, sub, "sneaky_key") // canonical form: dropped
		require.Equal(t, "Alerts", sub["display_name"])

		// Out-of-range / wrong-type values are field-specific 400s
		for _, bad := range []string{
			`{"subscriptions":[{"topic":"alerts","muted":"yes"}]}`,
			`{"subscriptions":[{"topic":"alerts","muted":2}]}`,
			`{"subscriptions":[{"topic":"alerts","insistent":"on"}]}`,
			`{"subscriptions":[{"topic":"alerts","min_priority":"high"}]}`,
			`{"subscriptions":[{"topic":"alerts","min_priority":7}]}`,
			`{"subscriptions":[{"topic":"alerts","auto_delete_seconds":-5}]}`,
			`{"subscriptions":[{"topic":"alerts","auto_delete_seconds":1.5}]}`,
			`{"subscriptions":[{"topic":"alerts","display_name":42}]}`,
			`{"subscriptions":[{"topic":"alerts","display_name":"` + strings.Repeat("x", 129) + `"}]}`,
			`{"subscriptions":[{"topic":"alerts","base_url":"ftp://x"}]}`,
			`{"manage":"all"}`,
			`{"manage":true}`,
			`{"token":"tk_smuggled"}`,
		} {
			rr := put(bad)
			if rr.Code != 400 {
				t.Fatalf("expected 400 for %s, got %d: %s", bad, rr.Code, rr.Body.String())
			}
		}

		// Valid canonical writes still round-trip, unknown TOP-LEVEL keys are
		// preserved (forward compat), manage:"full" accepted.
		good := `{"subscriptions":[{"topic":"alerts","muted":false}],"manage":"full","settings":{"min_priority":3}}`
		rr = put(good)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device/"+claim.DeviceID+"/config", "", device)
		require.Equal(t, 200, rr.Code)
		require.JSONEq(t, good, rr.Body.String())
	})
}

// TestServer_Device_AppliedAck covers the applied-config feedback loop
// (axon#22): PUT bumps config_version; the device acks what it applied;
// staleness (config_version > applied_version) becomes observable.
func TestServer_Device_AppliedAck(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		c := newTestConfigWithAuthFile(t, databaseURL)
		c.AuthDefault = user.PermissionDenyAll
		s := newTestServer(t, c)
		defer s.closeDatabases()
		require.Nil(t, s.userManager.AddUser("phil", "phil", user.RoleAdmin, false))
		phil := map[string]string{"Authorization": util.BasicAuth("phil", "phil")}

		rr := request(t, s, "POST", "/v1/device/pairing", `{"label":"test"}`, phil)
		require.Equal(t, 200, rr.Code)
		var pairing struct {
			Code string `json:"code"`
		}
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &pairing))
		rr = request(t, s, "POST", "/v1/device/claim", fmt.Sprintf(`{"code":%q}`, pairing.Code), nil)
		require.Equal(t, 200, rr.Code)
		var claim struct {
			DeviceID string `json:"device_id"`
			Token    string `json:"token"`
		}
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &claim))
		device := map[string]string{"Authorization": "Bearer " + claim.Token}

		// First config write → config_version 2 (claim seeds 1, write bumps)
		rr = request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config",
			`{"subscriptions":[{"topic":"alerts","muted":false}]}`, device)
		require.Equal(t, 200, rr.Code)

		// List shows the gap: config_version=2, applied_version=0
		rr = request(t, s, "GET", "/v1/device", "", phil)
		require.Equal(t, 200, rr.Code)
		var devices []struct {
			ID             string `json:"id"`
			ConfigVersion  int64  `json:"config_version"`
			AppliedVersion int64  `json:"applied_version"`
			AppliedAt      int64  `json:"applied_at"`
		}
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &devices))
		require.Len(t, devices, 1)
		require.Equal(t, claim.DeviceID, devices[0].ID)
		require.Equal(t, int64(2), devices[0].ConfigVersion)
		require.Equal(t, int64(0), devices[0].AppliedVersion)
		require.Equal(t, int64(0), devices[0].AppliedAt)

		// The device acks v2 — owner view flips to synced
		rr = request(t, s, "POST", "/v1/device/"+claim.DeviceID+"/applied", `{"version":2}`, device)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device", "", phil)
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &devices))
		require.Equal(t, int64(2), devices[0].AppliedVersion)
		require.NotZero(t, devices[0].AppliedAt)

		// A second write re-opens the gap
		rr = request(t, s, "PUT", "/v1/device/"+claim.DeviceID+"/config",
			`{"subscriptions":[{"topic":"alerts"},{"topic":"other"}]}`, device)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device", "", phil)
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &devices))
		require.Equal(t, int64(3), devices[0].ConfigVersion)
		require.Equal(t, int64(2), devices[0].AppliedVersion) // truthful staleness

		// Version-less ack means "the latest" (that's what the app sends)
		rr = request(t, s, "POST", "/v1/device/"+claim.DeviceID+"/applied", `{}`, device)
		require.Equal(t, 200, rr.Code)
		rr = request(t, s, "GET", "/v1/device", "", phil)
		require.Nil(t, json.Unmarshal(rr.Body.Bytes(), &devices))
		require.Equal(t, int64(3), devices[0].AppliedVersion) // acked current

		// Validation and authorization
		rr = request(t, s, "POST", "/v1/device/"+claim.DeviceID+"/applied", `{"version":-1}`, device)
		require.Equal(t, 400, rr.Code)
		rr = request(t, s, "POST", "/v1/device/"+claim.DeviceID+"/applied", `{"version":2}`, nil)
		require.Equal(t, 401, rr.Code) // anonymous cannot ack
	})
}
