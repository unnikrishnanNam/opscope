// Package server wires up all HTTP routes for OpScope.
//
// Routes starting with /api/ return JSON. Everything else is the React app.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Config holds the settings the server needs.
type Config struct {
	Version   string // shown by /api/health
	StaticDir string // folder with the built React app (index.html, assets/)
}

// New returns an http.Handler with every route registered.
func New(cfg Config, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// API routes. Go 1.22+ lets us put the HTTP method in the pattern.
	mux.HandleFunc("GET /api/health", healthHandler(cfg.Version))

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
