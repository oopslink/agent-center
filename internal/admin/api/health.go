package api

import (
	"encoding/json"
	"net/http"
)

// healthHandler is the smallest possible smoke endpoint — used by
// the v2.2-A1 deploy verification (`curl --unix-socket ... /admin/health`)
// and by future client connectivity probes. v2.3-7a (task #27) extends
// the response to reflect the actual transport (unix vs tcp) so an
// operator can confirm which leg they're hitting.
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	transport := "unix"
	if r.TLS != nil {
		transport = "tcp"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"transport": transport,
		"endpoint":  "admin",
	})
}
