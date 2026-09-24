package server

import (
	"context"
	"net/http"
	"time"

	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/log"
)

// aiPingTimeout bounds the provider health check in the status endpoint.
const aiPingTimeout = 2 * time.Second

// ensureAIEnabled fails requests with 400 if the AI layer is not configured. Like the
// nil-if-unconfigured mailer/stripe/twilio guards, this keeps every AI route nil-safe.
func (s *Server) ensureAIEnabled(next handleFunc) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, v *visitor) error {
		if !s.ai.Enabled() {
			return errHTTPBadRequestAIDisabled
		}
		return next(w, r, v)
	}
}

// apiAIStatusResponse is the admin-facing view of the AI layer (GET /v1/ai/status).
type apiAIStatusResponse struct {
	Enabled  bool            `json:"enabled"`
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Healthy  bool            `json:"healthy"`
	Health   string          `json:"health_error,omitempty"`
	Usage    *ai.UsageReport `json:"usage"`
}

// handleAIStatus reports provider health and today's global token usage (admin only).
func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request, v *visitor) error {
	ctx, cancel := context.WithTimeout(r.Context(), aiPingTimeout)
	defer cancel()
	response := &apiAIStatusResponse{
		Enabled:  true,
		Provider: s.ai.ProviderName(),
		Model:    s.ai.DefaultModel(),
		Usage:    s.ai.Report(""),
	}
	if err := s.ai.Ping(ctx); err != nil {
		response.Health = err.Error()
	} else {
		response.Healthy = true
	}
	log.Tag(tagAI).Field("provider", response.Provider).Field("healthy", response.Healthy).Debug("AI status requested")
	return s.writeJSON(w, response)
}

// handleAIUsage reports today's token usage for the requesting visitor (GET /v1/ai/usage).
// It is intentionally available to anonymous visitors: AI features are rate-limited per
// visitor, so unauthenticated clients can have usage too.
func (s *Server) handleAIUsage(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return s.writeJSON(w, s.ai.Report(visitorID(v.ip, v.user, v.config)))
}
