package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"heckel.io/ntfy/v2/user"
)

const aiConsentLookupTimeout = 2 * time.Second

type apiAIConsentResponse struct {
	Consented     bool     `json:"consented"`
	Provider      string   `json:"provider"`
	Endpoint      string   `json:"endpoint,omitempty"`
	Models        []string `json:"models"`
	Configuration string   `json:"configuration"`
	CanManage     bool     `json:"can_manage"`
	GrantedAt     int64    `json:"granted_at,omitempty"`
}

type apiAIConsentRequest struct {
	Accepted      bool   `json:"accepted"`
	Configuration string `json:"configuration"`
}

func (s *Server) initAIConsent() error {
	if !s.ai.Enabled() || s.userManager == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiConsentLookupTimeout)
	defer cancel()
	key, err := s.userManager.AIConsentSecret(ctx)
	if err != nil {
		return err
	}
	s.aiConsentKey = key
	return nil
}

func (s *Server) aiConsentModels() []string {
	models := []string{s.ai.DefaultModel(), s.config.AIModelPlan, s.config.AIModelEnrich, s.config.AIModelDigest, s.config.AIModelChat, s.config.AIEmbeddingsModel}
	models = slices.DeleteFunc(models, func(model string) bool { return model == "" })
	slices.Sort(models)
	return slices.Compact(models)
}

func (s *Server) aiConsentConfiguration() string {
	identity, _ := json.Marshal(struct {
		Provider string
		Endpoint string
		Models   []string
	}{s.ai.ProviderName(), s.ai.ProviderBaseURL(), []string{s.ai.DefaultModel(), s.config.AIModelPlan, s.config.AIModelEnrich, s.config.AIModelDigest, s.config.AIModelChat, s.config.AIEmbeddingsModel}})
	mac := hmac.New(sha256.New, s.aiConsentKey)
	_, _ = mac.Write(identity)
	return fmt.Sprintf("%x", mac.Sum(nil))
}

// aiProviderOrigin deliberately omits credentials, path, query and fragment.
func aiProviderOrigin(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (s *Server) hasAIConsent(ctx context.Context, u *user.User) (bool, error) {
	if u == nil || s.userManager == nil {
		return false, nil
	}
	if len(s.aiConsentKey) != 32 {
		return false, errors.New("AI consent metadata unavailable")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, aiConsentLookupTimeout)
	defer cancel()
	consent, err := s.userManager.AIConsent(lookupCtx, u.ID)
	return err == nil && consent != nil && consent.ProviderConfig == s.aiConsentConfiguration(), err
}

func (s *Server) ensureAIConsent(next handleFunc) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, v *visitor) error {
		allowed, err := s.hasAIConsent(r.Context(), requestUser(r, v))
		if err != nil {
			return err // Fail closed on lookup errors, including cancellation.
		} else if !allowed {
			return errHTTPForbiddenAIConsentRequired
		}
		return next(w, r, v)
	}
}

func (s *Server) aiConsentOriginAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // Authenticated API clients need not be browser clients.
	}
	expected := aiProviderOrigin(s.config.BaseURL)
	if origin == expected || (s.config.AccessControlAllowOrigin != "*" && origin == s.config.AccessControlAllowOrigin) {
		return true
	}
	parsed, err := url.Parse(origin)
	return expected == "" && err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == r.Host
}

func (s *Server) handleAIConsent(w http.ResponseWriter, r *http.Request, v *visitor) error {
	u := requestUser(r, v)
	ctx, cancel := context.WithTimeout(r.Context(), aiConsentLookupTimeout)
	defer cancel()
	if r.Method != http.MethodGet {
		// Grant/revoke is an owner action. An AI-scoped agent cannot consent for them.
		if len(u.TokenScopes) != 0 || !s.aiConsentOriginAllowed(r) {
			return errHTTPForbidden
		}
		if r.Method == http.MethodPost {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !strings.EqualFold(mediaType, "application/json") {
				return errHTTPBadRequestAIRequest // Reject form/text/plain consent CSRF.
			}
			body, err := readJSONWithLimit[apiAIConsentRequest](r.Body, jsonBodyBytesLimit, false)
			if err != nil {
				return err
			} else if !body.Accepted {
				return errHTTPBadRequestAIRequest
			} else if body.Configuration != s.aiConsentConfiguration() {
				return errHTTPConflictAIProviderChanged
			}
			if err := s.userManager.GrantAIConsent(ctx, u.ID, body.Configuration); err != nil {
				return err
			}
		} else if err := s.userManager.RevokeAIConsent(ctx, u.ID); err != nil {
			return err
		}
	}
	consent, err := s.userManager.AIConsent(ctx, u.ID)
	if err != nil {
		return err
	}
	response := &apiAIConsentResponse{
		Provider: s.ai.ProviderName(), Endpoint: aiProviderOrigin(s.ai.ProviderBaseURL()),
		Models: s.aiConsentModels(), Configuration: s.aiConsentConfiguration(), CanManage: len(u.TokenScopes) == 0,
	}
	if consent != nil && consent.ProviderConfig == response.Configuration {
		response.Consented = true
		response.GrantedAt = consent.GrantedAt.Unix()
	}
	return s.writeJSON(w, response)
}
