//go:build nopayments

package server

const hasStripe = false

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

func (s *Server) handleAccountBillingWebhookSubscriptionUpdated(r *http.Request, v *visitor, event stripe.Event) error {
	return errHTTPNotFound
}

func (s *Server) handleAccountBillingWebhookSubscriptionDeleted(r *http.Request, v *visitor, event stripe.Event) error {
	return errHTTPNotFound
}
