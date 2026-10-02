// OpScope is a small, read-only Kubernetes dashboard.
//
// This file is the entry point: it reads configuration from environment
// variables, loads the known clusters, builds the HTTP server and starts
// listening.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"opscope/internal/clusters"
	"opscope/internal/metrics"
	"opscope/internal/server"
)

// version is overwritten at build time with:
//
//	go build -ldflags "-X main.version=1.2.3"
var version = "dev"

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

	// 1. A cluster from the environment, if one is configured. A broken
	//    setting here is a mistake by whoever started OpScope, so we stop
	//    with a clear message instead of starting half-configured.
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

	// Sample CPU and memory usage in the background, for the sparklines.
	// context.Background() means "run until the program exits".
	history := metrics.NewHistory()
	go history.Collect(context.Background(), manager, logger)

	handler := server.New(cfg, manager, history, logger)
	if err := http.ListenAndServe(addr, handler); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// getEnv returns the value of an environment variable, or a fallback if it is empty.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
