// OpScope is a small, read-only Kubernetes dashboard.
//
// This file is the entry point: it reads configuration from environment
// variables, builds the HTTP server and starts listening.
package main

import (
	"log/slog"
	"net/http"
	"os"

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

	logger.Info("starting opscope", "addr", addr, "version", version, "static_dir", cfg.StaticDir)

	handler := server.New(cfg, logger)
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
