//go:build nowebpush

package server

import (
	"net/http"
)

func (s *Server) handleWebPushUpdate(w http.ResponseWriter, r *http.Request, v *visitor) error {
	return errHTTPNotFound
}

func (s *Server) handleWebPushDelete(w http.ResponseWriter, r *http.Request, _ *visitor) error {
	return errHTTPNotFound
}

func (s *Server) publishToWebPushEndpoints(v *visitor, m *message) {
	// Nothing to see here
}

func (s *Server) pruneAndNotifyWebPushSubscriptions() {
	// Nothing to see here
}
