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

	"k8s.io/klog/v2"

	"opscope/internal/bus"
	"opscope/internal/clusters"
	"opscope/internal/collector"
	"opscope/internal/graph"
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

	// client-go logs through klog, in its own format. Its lines go to
	// Opscope's log at debug level instead: the collectors already say what
	// matters (a cluster lost and back, a kind that can't be watched), and a
	// lost connection would otherwise print a line for every kind.
	klog.SetSlogLogger(slog.New(debugOnly{logger.Handler()}))

	cfg := server.Config{
		Version:     version,
		StaticDir:   getEnv("STATIC_DIR", "../frontend/dist"),
		VolumeUsage: os.Getenv("OPSCOPE_VOLUME_USAGE") == "true",
	}
	addr := ":" + getEnv("PORT", "8080")
	dataDir := getEnv("DATA_DIR", "../data")
	// Collectors keep a watch cache per cluster; OPSCOPE_LIVE=false turns
	// them off, and every page then calls the cluster as in v1.3.
	live := os.Getenv("OPSCOPE_LIVE") != "false"

	logger.Info("starting opscope", "addr", addr, "version", version, "static_dir", cfg.StaticDir, "data_dir", dataDir,
		"volume_usage", cfg.VolumeUsage, "live", live)

	// ctx is cancelled when the process gets SIGTERM (what Kubernetes and
	// `docker stop` send) or SIGINT (Ctrl+C in a terminal).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	manager := clusters.NewManager(dataDir)

	// One collector per cluster, started as each cluster is loaded below or
	// added in the UI, and stopped when it's removed or ctx is cancelled.
	collectors := collector.NewSet(ctx, live, bus.New(), logger)
	// The Graph engine follows the collectors' events, so it subscribes
	// before the first collector starts.
	graphs := graph.NewEngine(collectors, logger)
	go graphs.Run(ctx)
	collectors.Follow(manager)

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

	// Sample CPU and memory usage in the background, for the sparklines.
	// It stops when ctx is cancelled.
	history := metrics.NewHistory()
	go history.Collect(ctx, manager, collectors.Source, logger)

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(cfg, manager, collectors, graphs, history, logger),
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

// debugOnly passes every record on at debug level.
type debugOnly struct{ slog.Handler }

func (h debugOnly) Enabled(ctx context.Context, _ slog.Level) bool {
	return h.Handler.Enabled(ctx, slog.LevelDebug)
}

func (h debugOnly) Handle(ctx context.Context, r slog.Record) error {
	r.Level = slog.LevelDebug
	return h.Handler.Handle(ctx, r)
}

func (h debugOnly) WithAttrs(attrs []slog.Attr) slog.Handler {
	return debugOnly{h.Handler.WithAttrs(attrs)}
}

func (h debugOnly) WithGroup(name string) slog.Handler {
	return debugOnly{h.Handler.WithGroup(name)}
}

// getEnv returns the value of an environment variable, or a fallback if it is empty.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
