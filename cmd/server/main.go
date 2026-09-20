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

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           routes(logger.Logger, cfg.Server.RequestTimeout),
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

func routes(logger *slog.Logger, requestTimeout time.Duration) http.Handler {
	r := chi.NewRouter()

	// Order matters: RequestID must precede requestContext, which must precede
	// anything that logs, so every record downstream carries the request ID.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestContext)
	r.Use(requestLogger(logger))
	r.Use(recoverer(logger))
	r.Use(middleware.Timeout(requestTimeout))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return r
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
