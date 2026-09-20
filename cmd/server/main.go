package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/DhanushRamesh/friday/internal/config"
	"github.com/DhanushRamesh/friday/internal/logging"
	"github.com/DhanushRamesh/friday/internal/storage"
)

func main() {
	if err := run(); err != nil {
		// Configuration is read before the logger exists, so a startup failure
		// has nowhere structured to go and is reported plainly instead.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	logger, err := logging.New(os.Stdout, logging.Config{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
		Service:   "friday",
		Version:   version(),
		Env:       string(cfg.Env),
	})
	if err != nil {
		return err
	}
	// Set the default so packages that log without an injected logger still
	// produce records in the configured format.
	slog.SetDefault(logger.Logger)

	// Record the effective configuration once at startup. Config redacts the
	// database password, so this is safe to emit. The first question about any
	// misbehaving deployment is which settings it actually loaded.
	logger.Info("configuration loaded", slog.Any("config", cfg))

	// Fail at startup rather than on the first request. A server that accepts
	// traffic it cannot serve is harder to diagnose than one that refuses to
	// start and says why.
	db, err := storage.Open(context.Background(), cfg.Database, logger.Logger, storage.Options{
		// Statements carry user messages and tool output once tasks exist, so
		// they are recorded only on a developer machine.
		LogStatements: !cfg.Env.IsProduction(),
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("closing database", slog.Any("error", err))
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           routes(logger.Logger, cfg.Server.RequestTimeout, db),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	return serve(srv, logger, cfg.Server.ShutdownTimeout)
}

// serve starts the server and blocks until an interrupt arrives, then drains
// in-flight requests before returning.
func serve(srv *http.Server, logger *logging.Logger, shutdownTimeout time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "friday listening",
			slog.String("addr", srv.Addr),
			slog.String("log_level", logger.Level().String()),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining",
			slog.Duration("grace_period", shutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

// pinger is the part of the database handle the readiness check needs. Taking
// an interface keeps routes testable without a live database.
type pinger interface {
	Ping(ctx context.Context) error
}

func routes(logger *slog.Logger, requestTimeout time.Duration, db pinger) http.Handler {
	r := chi.NewRouter()

	// Order matters: RequestID must precede requestContext, which must precede
	// anything that logs, so every record downstream carries the request ID.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestContext)
	r.Use(requestLogger(logger))
	r.Use(recoverer(logger))
	r.Use(middleware.Timeout(requestTimeout))

	// Liveness: is the process up. This must not touch dependencies, or a
	// database blip would have the supervisor restart a healthy server.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Readiness: can the process actually serve traffic.
	r.Get("/ready", readyHandler(logger, db))

	return r
}

// readinessTimeout bounds the dependency checks. It is short because a load
// balancer polling this will not wait, and a slow answer is a failed one.
const readinessTimeout = 2 * time.Second

func readyHandler(logger *slog.Logger, db pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		checks := map[string]string{"database": "ok"}
		status := http.StatusOK

		if err := db.Ping(ctx); err != nil {
			// The caller gets a plain word; the detail stays in the log.
			checks["database"] = "unreachable"
			status = http.StatusServiceUnavailable
			logger.ErrorContext(ctx, "readiness check failed",
				slog.String("check", "database"),
				slog.Any("error", err))
		}

		body := map[string]any{"status": "ready", "checks": checks}
		if status != http.StatusOK {
			body["status"] = "not ready"
		}
		writeJSON(ctx, w, status, body)
	}
}

func writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be reported, not fixed.
		logging.FromContext(ctx).ErrorContext(ctx, "failed to write response body",
			slog.Any("error", err))
	}
}

// version reports the VCS revision stamped into the binary by the Go toolchain.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "dev"
}
