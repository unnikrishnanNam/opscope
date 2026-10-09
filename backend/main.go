// Opscope is a lightweight, read-only Kubernetes dashboard for day-to-day
// operations.
//
// This file is the entry point: it reads configuration from environment
// variables, loads the known clusters, starts the HTTP server, and shuts it
// down cleanly when asked to stop.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"opscope/internal/clusters"
	"opscope/internal/metrics"
	"opscope/internal/server"
)

// version is overwritten at build time with:
//
//	go build -ldflags "-X main.version=1.2.3"
var version = "dev"

// shutdownTimeout is how long running requests get to finish when Opscope
// is asked to stop. Kubernetes waits 30 seconds by default before killing a
// pod, so this fits well inside that.
const shutdownTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg := server.Config{
		Version:   version,
		StaticDir: getEnv("STATIC_DIR", "../frontend/dist"),
	}
	addr := ":" + getEnv("PORT", "8080")
	dataDir := getEnv("DATA_DIR", "../data")

	logger.Info("starting opscope", "addr", addr, "version", version, "static_dir", cfg.StaticDir, "data_dir", dataDir)

	manager := clusters.NewManager(dataDir)

	// 1. Clusters from the environment. A broken setting here is a mistake
	//    by whoever started Opscope, so we stop with a clear message instead
	//    of starting half-configured.
	if os.Getenv("OPSCOPE_IN_CLUSTER") == "true" {
		// Running as a pod: read the cluster we're in, with the pod's service account.
		cluster, err := manager.LoadInCluster(os.Getenv("OPSCOPE_CLUSTER_NAME"))
		if err != nil {
			logger.Error("can't use the in-cluster service account", "error", err)
			os.Exit(1)
		}
		logger.Info("loaded cluster from service account", "id", cluster.ID, "server", cluster.Server)
	}
	if path := os.Getenv("OPSCOPE_KUBECONFIG"); path != "" {
		cluster, err := manager.LoadFromFile(path, os.Getenv("OPSCOPE_CONTEXT"), os.Getenv("OPSCOPE_CLUSTER_NAME"))
		if err != nil {
			logger.Error("can't load OPSCOPE_KUBECONFIG", "path", path, "error", err)
			os.Exit(1)
		}
		logger.Info("loaded cluster from environment", "id", cluster.ID, "server", cluster.Server)
	}

	// 2. Clusters that were added in the UI earlier.
	for _, err := range manager.LoadSaved() {
		logger.Warn("skipping saved cluster", "error", err)
	}

	logger.Info("clusters ready", "count", len(manager.List()))

	// ctx is cancelled when the process gets SIGTERM (what Kubernetes and
	// `docker stop` send) or SIGINT (Ctrl+C in a terminal).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Sample CPU and memory usage in the background, for the sparklines.
	// It stops when ctx is cancelled.
	history := metrics.NewHistory()
	go history.Collect(ctx, manager, logger)

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(cfg, manager, history, logger),
		ReadHeaderTimeout: 10 * time.Second, // don't let a slow client hold a connection open forever
	}

	// ListenAndServe blocks, so it runs in its own goroutine; the main
	// goroutine waits for a stop signal (or for the server to fail).
	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe() }()

	select {
	case err := <-serverErr:
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	case <-ctx.Done():
	}

	// Graceful shutdown: stop accepting new connections and let running
	// requests finish. Followed log streams never finish on their own, so
	// whatever is still open after the timeout is closed.
	logger.Info("shutting down", "timeout", shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("closing connections that didn't finish in time", "error", err)
		srv.Close()
	}
	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error during shutdown", "error", err)
	}
	logger.Info("stopped")
}

// getEnv returns the value of an environment variable, or a fallback if it is empty.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
