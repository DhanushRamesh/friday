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

// main : Starts the server and exits non-zero if it cannot run.
func main() {
	if err := run(); err != nil {
		// Configuration is read before the logger exists, so this cannot be
		// a structured record.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// run : Loads configuration, opens the dependencies and serves until
// interrupted, returning the first error that prevents any of it.
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
	// So that packages logging without an injected logger use this format.
	slog.SetDefault(logger.Logger)

	// Config redacts the database password, so this is safe to emit.
	logger.Info("configuration loaded", slog.Any("config", cfg))

	// Opened here so an unusable database stops startup rather than failing
	// on the first request.
	db, err := storage.Open(context.Background(), cfg.Database, logger.Logger, storage.Options{
		// Statements carry user data, so they are recorded outside production only.
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

// serve : Starts the server and blocks until an interrupt arrives, then drains
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

// pinger : The part of a database handle that the readiness check requires.
type pinger interface {
	Ping(ctx context.Context) error
}

// routes : Builds the HTTP handler, applying the middleware stack in the
// order every request passes through it.
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

	// Liveness. Touches no dependencies, so a dependency outage cannot cause
	// a supervisor to restart a working process.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Readiness.
	r.Get("/ready", readyHandler(logger, db))

	return r
}

// readinessTimeout : Bounds the dependency checks made by readyHandler.
const readinessTimeout = 2 * time.Second

// readyHandler : Reports whether FRIDAY's dependencies are usable, answering
// 503 when any check fails.
func readyHandler(logger *slog.Logger, db pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		checks := map[string]string{"database": "ok"}
		status := http.StatusOK

		if err := db.Ping(ctx); err != nil {
			// The cause goes to the log, not to the caller.
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

// writeJSON : Writes body as a JSON response with the given status code.
func writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be reported, not fixed.
		logging.FromContext(ctx).ErrorContext(ctx, "failed to write response body",
			slog.Any("error", err))
	}
}

// version : Reports the VCS revision stamped into the binary by the Go toolchain.
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
