// Package server wires up all HTTP routes for Opscope.
//
// Routes starting with /api/ return JSON. Everything else is the React app.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"opscope/internal/clusters"
	"opscope/internal/metrics"
)

// Config holds the settings the server needs.
type Config struct {
	Version   string // shown by /api/health
	StaticDir string // folder with the built React app (index.html, assets/)
}

// New returns an http.Handler with every route registered.
func New(cfg Config, manager *clusters.Manager, history *metrics.History, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// API routes. Go 1.22+ lets us put the HTTP method and {placeholders}
	// in the pattern; handlers read placeholders with r.PathValue("id").
	mux.HandleFunc("GET /api/health", healthHandler(cfg.Version))

	mux.HandleFunc("GET /api/clusters", listClusters(manager))
	mux.HandleFunc("POST /api/clusters", addCluster(manager))
	mux.HandleFunc("POST /api/clusters/inspect", inspectKubeconfig())
	mux.HandleFunc("GET /api/clusters/{id}", getCluster(manager))
	mux.HandleFunc("DELETE /api/clusters/{id}", removeCluster(manager))

	// One route for every list: namespaces, pods, deployments, ...
	// The {resource} part picks the lister (see internal/resources).
	mux.HandleFunc("GET /api/clusters/{id}/{resource}", listResource(manager))

	// A fixed path segment beats a {placeholder}, so this route wins over
	// the one above for /api/clusters/{id}/overview.
	mux.HandleFunc("GET /api/clusters/{id}/overview", getOverview(manager))

	// Live CPU and memory usage from metrics-server.
	mux.HandleFunc("GET /api/clusters/{id}/metrics/nodes", getNodeMetrics(manager, history))
	mux.HandleFunc("GET /api/clusters/{id}/metrics/pods", getPodMetrics(manager))

	// One value of one secret, fetched only when the user clicks "Reveal".
	mux.HandleFunc("GET /api/clusters/{id}/secrets/{namespace}/{name}/{key}", getSecretValue(manager))

	// One object's details: with a namespace, or without for cluster-wide kinds.
	mux.HandleFunc("GET /api/clusters/{id}/{resource}/{namespace}/{name}", getDetail(manager))
	mux.HandleFunc("GET /api/clusters/{id}/{resource}/{name}", getDetail(manager))

	// A pod's logs, streamed as plain text.
	mux.HandleFunc("GET /api/clusters/{id}/pods/{namespace}/{name}/logs", streamLogs(manager))

	// Any other /api/ path is a 404 in JSON, so the frontend never gets
	// index.html back when it asked for data.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint: "+r.URL.Path)
	})

	// Everything else is the frontend.
	mux.Handle("/", staticHandler(cfg.StaticDir))

	return logRequests(logger, mux)
}

// writeJSON sends any value as a JSON response.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

// writeError sends an error in the shape {"error": "..."}.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeErrorDetail is like writeError but adds the raw error as "detail",
// for errors where the friendly message hides useful specifics.
func writeErrorDetail(w http.ResponseWriter, status int, message, detail string) {
	writeJSON(w, status, map[string]string{"error": message, "detail": detail})
}

// logRequests is a middleware: it wraps another handler and logs one line
// per request after it finishes.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration", time.Since(start).Round(time.Microsecond),
		)
	})
}

// statusRecorder remembers the status code a handler wrote, so the
// logging middleware can print it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap gives access to the original ResponseWriter. http.ResponseController
// uses it to reach the real writer's Flush, which streaming logs need.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
