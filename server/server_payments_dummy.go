//go:build nopayments

package server

import (
	"net/http"
)

type stripeAPI interface {
	CancelSubscription(id string) (string, error)
}

func newStripeAPI() stripeAPI {
	return nil
}

func (s *Server) fetchStripePrices() (map[string]int64, error) {
	return nil, errHTTPNotFound
}

func (s *Server) handleBillingTiersGet(w http.ResponseWriter, _ *http.Request, _ *visitor) error {
	return errHTTPNotFound
}

func (s *Server) handleAccountBillingSubscriptionCreate(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return errHTTPNotFound
}

func (s *Server) handleAccountBillingSubscriptionCreateSuccess(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return errHTTPNotFound
}

func (s *Server) handleAccountBillingSubscriptionUpdate(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return errHTTPNotFound
}

func (s *Server) handleAccountBillingSubscriptionDelete(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return errHTTPNotFound
}

func (s *Server) handleAccountBillingPortalSessionCreate(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return errHTTPNotFound
}

func (s *Server) handleAccountBillingWebhook(_ http.ResponseWriter, r *http.Request, v *visitor) error {
	return errHTTPNotFound
}
