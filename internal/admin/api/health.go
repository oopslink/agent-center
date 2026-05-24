package api

import (
	"encoding/json"
	"net/http"
)

// healthHandler is the smallest possible smoke endpoint — used by
// the v2.2-A1 deploy verification (`curl --unix-socket ... /admin/health`)
// and by future client connectivity probes.
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"transport": "unix",
		"endpoint":  "admin",
	})
}
