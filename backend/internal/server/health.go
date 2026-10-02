package server

import "net/http"

// healthResponse is the JSON body returned by GET /api/health.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// healthHandler reports that the server is up. Later it is also used for
// Kubernetes liveness and readiness probes.
func healthHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: version})
	}
}
