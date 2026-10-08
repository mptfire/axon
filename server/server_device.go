package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

// axon: agent channel — device pairing and server-relayed device config
// (docs/agents.md, "Direct AI control for the axon Android app").
//
// Routes:
//   POST   /v1/device/pairing           mint a one-time pairing code (auth: user)
//   POST   /v1/device/claim             claim a code -> device-scoped token (auth: the code)
//   GET    /v1/device                   list paired devices (auth: user)
//   DELETE /v1/device/{id}              unpair + revoke the device token (auth: user)
//   GET    /v1/device/{id}/config       read device config (auth: owner or the device itself)
//   PUT    /v1/device/{id}/config       write device config (auth: owner or the device itself)
//
// A device-scoped token is rejected on every account-management route (see
// rejectDeviceScope), so a device can sync topics and its own config but never
// mint tokens, change passwords, or touch other devices.

var (
	devicePathRegex       = regexp.MustCompile(`^/v1/device/([^/]+)$`)
	deviceConfigPathRegex = regexp.MustCompile(`^/v1/device/([^/]+)/config$`)
	deviceAppliedPathRegex = regexp.MustCompile(`^/v1/device/([^/]+)/applied$`)
	deviceIDRegex         = regexp.MustCompile(`^dv_[A-Za-z0-9]{4,32}$`)
)

type apiDeviceAppliedRequest struct {
	Version int64 `json:"version"`
}

type apiPairingRequest struct {
	Label string `json:"label"` // optional device label, e.g. "Pixel 9"
}

type apiPairingResponse struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
	DeepLink  string `json:"deep_link"` // axon://pair/<code>
}

type apiDeviceClaimRequest struct {
	Code  string `json:"code"`
	Key   string `json:"key"`   // build-time pairing key (private builds)
	Label string `json:"label"` // optional
}

type apiDeviceClaimResponse struct {
	DeviceID string `json:"device_id"`
	Token    string `json:"token"`
	BaseURL  string `json:"base_url"`
}

type apiDeviceResponse struct {
	ID             string `json:"id"`
	Label          string `json:"label,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	LastSeen       int64  `json:"last_seen"`
	ConfigVersion  int64  `json:"config_version"`
	AppliedVersion int64  `json:"applied_version"`
	AppliedAt      int64  `json:"applied_at"`
}

// handleDevicePairingCreate mints a one-time pairing code. The caller is an
// authenticated user (usually an agent acting for them via MCP): the code is
// then handed to the human, who taps axon://pair/<code> on the phone. The
// human tap is the consent step — minting alone grants nothing.
func (s *Server) handleDevicePairingCreate(w http.ResponseWriter, r *http.Request, v *visitor) error {
	u := v.User()
	if isDeviceScopedToken(u) {
		return errHTTPForbidden // A paired device must never grant further tokens
	}
	req, err := readJSONWithLimit[apiPairingRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	} else if len(req.Label) > 64 {
		return errHTTPBadRequestDeviceLabelTooLong
	}
	pc, err := s.userManager.CreatePairingCode(u.ID, req.Label)
	if err != nil {
		return err
	}
	logvr(v, r).Tag(tagDevice).Field("device_pairing_label", req.Label).Info("Device pairing code issued")
	return s.writeJSON(w, &apiPairingResponse{
		Code:      pc.Code,
		ExpiresAt: pc.ExpiresAt.Unix(),
		DeepLink:  "axon://pair/" + pc.Code,
	})
}

// handleDeviceClaim exchanges a pairing code for a device-scoped token. No
// authentication: the code is the credential, it is one-time, and it expires
// within minutes. This is the endpoint the app's pairing screen calls.
func (s *Server) handleDeviceClaim(w http.ResponseWriter, r *http.Request, v *visitor) error {
	req, err := readJSONWithLimit[apiDeviceClaimRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	} else if len(req.Code) == 0 || len(req.Code) > 128 || len(req.Label) > 64 {
		return errHTTPBadRequest
	}
	dev, token, err := s.userManager.ClaimPairingCode(req.Code, req.Label, v.ip)
	if err != nil {
		if errors.Is(err, user.ErrPairingCodeInvalid) {
			return errHTTPBadRequestDevicePairingInvalid
		} else if errors.Is(err, user.ErrTooManyDevices) {
			return errHTTPTooManyRequestsLimitDevices
		}
		return err
	}
	logv(v).Tag(tagDevice).Field("device_id", dev.ID).Info("Device paired")
	return s.writeJSON(w, &apiDeviceClaimResponse{
		DeviceID: dev.ID,
		Token:    token,
		BaseURL:  s.config.BaseURL,
	})
}

// handleDeviceClaimBuild pairs a device using the build-time key (private app
// builds). No auth: the key is the credential, exactly like a pairing code but
// standing, because it lives inside the operator's own APK. Constant-time
// compared against the configured key; disabled unless both key and owner are
// configured. Rate-limited like the code claim.
func (s *Server) handleDeviceClaimBuild(w http.ResponseWriter, r *http.Request, v *visitor) error {
	if s.config.DevicePairingKey == "" || s.config.DevicePairingOwner == "" {
		return errHTTPNotFound // feature not enabled on this server
	}
	req, err := readJSONWithLimit[apiDeviceClaimRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	} else if len(req.Key) == 0 || len(req.Key) > 256 || len(req.Label) > 64 {
		return errHTTPBadRequest
	}
	if subtle.ConstantTimeCompare([]byte(req.Key), []byte(s.config.DevicePairingKey)) != 1 {
		logv(v).Tag(tagDevice).Warn("Build-key pairing rejected (wrong key)")
		return errHTTPBadRequestDevicePairingInvalid
	}
	dev, token, err := s.userManager.ClaimBuildKey(s.config.DevicePairingOwner, req.Label, v.ip)
	if err != nil {
		if errors.Is(err, user.ErrTooManyDevices) {
			return errHTTPTooManyRequestsLimitDevices
		}
		return err
	}
	logv(v).Tag(tagDevice).Field("device_id", dev.ID).Info("Device paired via build key")
	return s.writeJSON(w, &apiDeviceClaimResponse{
		DeviceID: dev.ID,
		Token:    token,
		BaseURL:  s.config.BaseURL,
	})
}

// handleDeviceAppliedAck records the config version a device has applied
// (axon#22): closes the "did the phone apply it?" loop. The device itself (or
// its owner) posts {"version": N}; staleness vs config_version is the alert
// signal. A stale ack (version < current) is stored as-is — it is truthful.
func (s *Server) handleDeviceAppliedAck(w http.ResponseWriter, r *http.Request, v *visitor) error {
	matches := deviceAppliedPathRegex.FindStringSubmatch(r.URL.Path)
	if len(matches) != 2 || !deviceIDRegex.MatchString(matches[1]) {
		return errHTTPInternalErrorInvalidPath
	}
	req, err := readJSONWithLimit[apiDeviceAppliedRequest](r.Body, jsonBodyBytesLimit, false)
	if err != nil {
		return err
	} else if req.Version < 1 || req.Version > math.MaxInt32 {
		return errHTTPBadRequestDeviceConfigInvalid.Wrap("version must be a positive integer")
	}
	dev, err := s.deviceFromRequest(r, v, matches[1])
	if err != nil {
		return err
	}
	if err := s.userManager.AckDeviceApplied(dev.UserID, dev.ID, req.Version); err != nil {
		return err
	}
	logvr(v, r).Tag(tagDevice).Field("device_id", dev.ID).Field("applied_version", req.Version).Info("Device applied config")
	return s.writeJSON(w, newSuccessResponse())
}

// handleDevicesList lists the user's paired devices. Tokens are never included.
func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request, v *visitor) error {
	u := v.User()
	if isDeviceScopedToken(u) {
		return errHTTPForbidden // Devices know their own ID; the fleet view is the owner's
	}
	devices, err := s.userManager.Devices(u.ID)
	if err != nil {
		return err
	}
	response := make([]*apiDeviceResponse, 0, len(devices))
	for _, dev := range devices {
		response = append(response, &apiDeviceResponse{
			ID:             dev.ID,
			Label:          dev.Label,
			CreatedAt:      dev.CreatedAt.Unix(),
			UpdatedAt:      dev.UpdatedAt.Unix(),
			LastSeen:       dev.LastSeen.Unix(),
			ConfigVersion:  dev.ConfigVersion,
			AppliedVersion: dev.AppliedVersion,
			AppliedAt:      dev.AppliedAt.Unix(),
		})
	}
	return s.writeJSON(w, response)
}

// handleDeviceDelete unpairs a device and instantly revokes its token.
func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request, v *visitor) error {
	matches := devicePathRegex.FindStringSubmatch(r.URL.Path)
	if len(matches) != 2 || !deviceIDRegex.MatchString(matches[1]) {
		return errHTTPInternalErrorInvalidPath
	}
	u := v.User()
	if isDeviceScopedToken(u) {
		// A device may unpair itself (self-revocation), never a sibling
		dev, err := s.userManager.DeviceByToken(u.Token)
		if err != nil {
			return errHTTPUnauthorized
		} else if dev.ID != matches[1] {
			return errHTTPForbidden
		}
	}
	if err := s.userManager.DeleteDevice(u.ID, matches[1]); err != nil {
		if errors.Is(err, user.ErrDeviceNotFound) {
			return errHTTPNotFound
		}
		return err
	}
	logvr(v, r).Tag(tagDevice).Field("device_id", matches[1]).Info("Device unpaired (token revoked)")
	return s.writeJSON(w, newSuccessResponse())
}

// deviceFromRequest resolves the device the request targets, enforcing the
// access rule: an unrestricted (or admin) token of the owning user may address
// any of their devices; a device-scoped token may only address itself.
func (s *Server) deviceFromRequest(r *http.Request, v *visitor, deviceID string) (*user.Device, error) {
	u := v.User()
	if !deviceIDRegex.MatchString(deviceID) {
		return nil, errHTTPInternalErrorInvalidPath
	}
	if isDeviceScopedToken(u) {
		dev, err := s.userManager.DeviceByToken(u.Token)
		if err != nil {
			return nil, errHTTPUnauthorized // dangling device token: treat as unauthenticated
		} else if dev.ID != deviceID || dev.UserID != u.ID {
			return nil, errHTTPForbidden
		}
		return dev, nil
	}
	dev, err := s.userManager.DeviceByID(u.ID, deviceID)
	if err != nil {
		if errors.Is(err, user.ErrDeviceNotFound) {
			return nil, errHTTPNotFound
		}
		return nil, err
	}
	return dev, nil
}

// handleDeviceConfigGet returns the device's config blob (the app polls this
// on sync; the agent channel writes it).
func (s *Server) handleDeviceConfigGet(w http.ResponseWriter, r *http.Request, v *visitor) error {
	matches := deviceConfigPathRegex.FindStringSubmatch(r.URL.Path)
	if len(matches) != 2 {
		return errHTTPInternalErrorInvalidPath
	}
	dev, err := s.deviceFromRequest(r, v, matches[1])
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write([]byte(dev.Config))
	if err == nil && isDeviceScopedToken(v.User()) {
		_ = s.userManager.TouchDevice(dev.ID) // device synced; status only, failure harmless
	}
	return err
}

// handleDeviceConfigPut stores a new config blob. The agent channel: agents
// (or the user) describe desired subscriptions/settings; the app applies them
// on sync. The blob is validated for size and topic-name shape only — actual
// message access still goes through the normal topic ACLs at use time.
func (s *Server) handleDeviceConfigPut(w http.ResponseWriter, r *http.Request, v *visitor) error {
	matches := deviceConfigPathRegex.FindStringSubmatch(r.URL.Path)
	if len(matches) != 2 {
		return errHTTPInternalErrorInvalidPath
	}
	dev, err := s.deviceFromRequest(r, v, matches[1])
	if err != nil {
		return err
	}
	body, err := util.Peek(r.Body, user.DeviceConfigMaxBytes+1)
	if err != nil {
		return err
	} else if body.LimitReached {
		return errHTTPBadRequestDeviceConfigTooLarge
	}
	configBytes, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return errHTTPBadRequestDeviceConfigInvalid.Wrap("config must be a JSON object")
	}
	// Validate AND normalize: writers have shipped type drift (e.g. "muted":0
	// instead of false) that poisoned device sync silently (axon-android#4).
	// The stored blob becomes the canonical form — typed flags, unknown
	// per-subscription keys dropped — so every device parses the same thing.
	if err := validateDeviceConfig(config); err != nil {
		return err
	}
	canonical, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := s.userManager.ChangeDeviceConfig(dev.UserID, dev.ID, string(canonical)); err != nil {
		return err
	}
	logvr(v, r).Tag(tagDevice).Field("device_id", dev.ID).Info("Device config updated (agent channel)")
	return s.writeJSON(w, newSuccessResponse())
}

// validForeignBaseURL accepts http(s) URLs without credentials in them.
func validForeignBaseURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if parsed.User != nil || strings.Contains(u, "@") {
		return false // no credentials in URLs, ever
	}
	return true
}

// validateDeviceConfig enforces the structural contract the app relies on and
// normalizes the blob to a canonical form: subscriptions[].topic must be valid
// topic names on this server, flag fields must be bool or 0/1 (normalized to
// bool), numeric fields whole numbers in range, and the blob must not smuggle
// credential-looking fields. Unknown per-subscription keys are dropped so a
// sloppy writer cannot poison device parsers (axon-android#4).
func validateDeviceConfig(config map[string]any) error {
	subs, ok := config["subscriptions"]
	if ok {
		list, isList := subs.([]any)
		if !isList {
			return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions must be an array")
		}
		if len(list) > 200 {
			return errHTTPBadRequestDeviceConfigInvalid.Wrap("too many subscriptions")
		}
		for i, s := range list {
			sub, isMap := s.(map[string]any)
			if !isMap {
				return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscription must be an object")
			}
			topic, _ := sub["topic"].(string)
			if !topicRegex.MatchString(topic) {
				return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions[%d].invalid topic name", i)
			}
			if baseURL, present := sub["base_url"]; present {
				// The channel may also manage subscriptions on OTHER ntfy
				// servers the phone uses (the agent's whole-subscription view).
				// Shape-checked only: message access on foreign servers happens
				// with whatever credentials the app has stored per server.
				url, isString := baseURL.(string)
				if !isString {
					return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions[%d].base_url must be a string", i)
				} else if url != "" && !validForeignBaseURL(url) {
					return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions[%d].invalid base_url", i)
				}
			}
			if err := normalizeConfigFlag(sub, "muted", i); err != nil {
				return err
			}
			if err := normalizeConfigFlag(sub, "insistent", i); err != nil {
				return err
			}
			if err := wholeNumberInRange(sub, "min_priority", 0, 5, i); err != nil {
				return err
			}
			if err := wholeNumberInRange(sub, "auto_delete_seconds", 0, math.MaxInt32, i); err != nil {
				return err
			}
			if display, present := sub["display_name"]; present && display != nil {
				ds, isString := display.(string)
				if !isString {
					return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions[%d].display_name must be a string or null", i)
				} else if len(ds) > 128 {
					return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions[%d].display_name too long (max 128)", i)
				}
			}
			for k := range sub {
				switch k {
				case "topic", "base_url", "muted", "min_priority", "auto_delete_seconds", "insistent", "display_name":
				default:
					delete(sub, k) // canonical form: unknown keys never reach devices
				}
			}
		}
	}
	if manage, present := config["manage"]; present {
		ms, isString := manage.(string)
		if !isString || (ms != "full" && ms != "") {
			return errHTTPBadRequestDeviceConfigInvalid.Wrap("manage must be \"full\" or empty")
		}
	}
	for _, forbidden := range []string{"token", "password", "secret"} {
		if _, present := config[forbidden]; present {
			return errHTTPBadRequestDeviceConfigInvalid.Wrap("credential fields not allowed in device config")
		}
	}
	return nil
}

// normalizeConfigFlag accepts a flag written as bool or 0/1 and rewrites the
// map in place to a bool. Anything else is a field-specific 400.
func normalizeConfigFlag(sub map[string]any, field string, i int) error {
	v, present := sub[field]
	if !present {
		return nil
	}
	switch t := v.(type) {
	case bool:
		return nil
	case float64:
		if t == 0 || t == 1 {
			sub[field] = t == 1
			return nil
		}
	}
	return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions[%d].%s must be a boolean or 0/1", i, field)
}

// wholeNumberInRange accepts whole numbers in [min,max] (JSON numbers decode
// as float64) and rejects bools, fractions, and out-of-range values.
func wholeNumberInRange(sub map[string]any, field string, min, max float64, i int) error {
	v, present := sub[field]
	if !present {
		return nil
	}
	t, ok := v.(float64)
	if !ok || t != math.Trunc(t) || t < min || t > max {
		return errHTTPBadRequestDeviceConfigInvalid.Wrap("subscriptions[%d].%s must be an integer between %d and %d", i, field, int64(min), int64(max))
	}
	return nil
}

// isDeviceScopedToken reports whether the authenticated principal is a device
// token from the pairing flow (scopes contain "device" and are restricted).
// Unrestricted credentials (passwords, admin tokens) have empty TokenScopes and
// are NOT device tokens — note HasTokenScope answers the opposite question
// ("may this credential use scope X"), so it cannot be used here.
func isDeviceScopedToken(u *user.User) bool {
	return u != nil && len(u.TokenScopes) > 0 && util.Contains(u.TokenScopes, user.TokenScopeDevice)
}

// deviceScopeAllowedPath is the allow-list of the device-scope sandbox: paths a
// device-scoped token may reach. Topic paths (everything outside /v1/), health,
// the app config, the user's subscription sync, webpush registration, and the
// device endpoints themselves (which enforce device-to-device isolation).
func deviceScopeAllowedPath(path string) bool {
	if !strings.HasPrefix(path, "/v1/") {
		return true // topics, health, docs, static, root app
	}
	switch {
	case path == "/v1/account/subscription":
		return true // subscription sync (no secrets in the response)
	case path == "/v1/account":
		return true // allowed for GET; token values are stripped for device callers
	case path == "/v1/device" || strings.HasPrefix(path, "/v1/device/"):
		return true // self-enforced inside the device handlers
	case strings.HasPrefix(path, "/v1/webpush"):
		return true // push registration for the app
	case path == "/v1/health" || path == "/v1/settings" || path == "/v1/config" || path == "/v1/config.js":
		return true
	}
	return false
}
