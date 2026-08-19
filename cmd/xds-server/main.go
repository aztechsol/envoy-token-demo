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

	configpkg "envoy-xds-lua-demo/internal/config"
	xdspkg "envoy-xds-lua-demo/internal/xds"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// Cancelling ctx on Ctrl-C or SIGTERM also stops the gRPC server and file watcher.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// CONFIG_PATH is overridden in Docker; local runs use the checked-in example.
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "config/tokens.yaml"
	}

	source := configpkg.NewFileSource(ctx, path, logger)
	// Do not start xDS until there is a valid initial configuration for Envoy.
	initial, err := source.Load()
	if err != nil {
		logger.Error("initial configuration failed", "error", err)
		os.Exit(1)
	}

	cache, grpcServer := xdspkg.NewCache(ctx, logger)
	publisher := xdspkg.NewPublisher(cache, logger)
	var version uint64 = 1
	// Store the first Lua/ECDS snapshot so Envoy receives it when it connects.
	if err := publish(ctx, publisher, initial, version); err != nil {
		logger.Error("initial publication failed", "error", err)
		os.Exit(1)
	}
	logger.Info("xDS service starting", "config_path", path, "config_version", version, "token_count", len(initial.Tenants))

	health := &http.Server{Addr: ":8082", Handler: healthHandler(), ReadHeaderTimeout: 5 * time.Second}
	errs := make(chan error, 3)
	// The watcher sends each successfully reloaded config through this channel.
	updates := make(chan *configpkg.TenantConfig)

	// Serve Envoy's xDS (ADS/ECDS) requests.
	go func() {
		errs <- xdspkg.ServeGRPC(ctx, ":18000", grpcServer, logger)
	}()

	// Expose a simple Docker/orchestrator health endpoint.
	go func() {
		logger.Info("health server started", "address", health.Addr)
		err := health.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	// Watch tokens.yaml and send valid replacements to the select loop below.
	go func() {
		errs <- source.Watch(updates)
	}()

	for {
		select {
		case <-ctx.Done():
			// Stop accepting health checks before this process exits.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = health.Shutdown(shutdownCtx)
			cancel()
			return
		case err := <-errs:
			if err != nil {
				logger.Error("service stopped", "error", err)
				os.Exit(1)
			}
		case cfg := <-updates:
			// A saved, valid config becomes a new ECDS snapshot for connected Envoys.
			version++
			if err := publish(ctx, publisher, cfg, version); err != nil {
				logger.Error("publication failed; keeping last-known-good", "attempted_version", version, "error", err)
				continue
			}
		}
	}
}

func publish(ctx context.Context, publisher *xdspkg.Publisher, cfg *configpkg.TenantConfig, version uint64) error {
	// Envoy receives Lua as an ECDS resource, not as the YAML file directly.
	source, err := xdspkg.RenderLua(cfg)
	if err != nil {
		return err
	}
	return publisher.Publish(ctx, version, source, len(cfg.Tenants))
}

func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
