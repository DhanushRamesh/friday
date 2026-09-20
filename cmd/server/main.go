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

	"github.com/DhanushRamesh/friday/internal/logging"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet, so report startup failures plainly.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	env := getenv("FRIDAY_ENV", "dev")

	format := logging.FormatJSON
	if env == "dev" {
		format = logging.FormatText
	}
	if f := os.Getenv("FRIDAY_LOG_FORMAT"); f != "" {
		format = logging.Format(f)
	}

	logger, err := logging.New(os.Stdout, logging.Config{
		Level:     getenv("FRIDAY_LOG_LEVEL", "info"),
		Format:    format,
		AddSource: env == "dev",
		Service:   "friday",
		Version:   version(),
		Env:       env,
	})
	if err != nil {
		return err
	}
	// Set the default so packages that log without an injected logger still
	// produce records in the configured format.
	slog.SetDefault(logger.Logger)

	addr := getenv("FRIDAY_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           routes(logger.Logger),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return serve(srv, logger)
}

// serve starts the server and blocks until an interrupt arrives, then drains
// in-flight requests before returning.
func serve(srv *http.Server, logger *logging.Logger) error {
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
		logger.Info("shutdown signal received, draining")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

func routes(logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	// Order matters: RequestID must precede requestContext, which must precede
	// anything that logs, so every record downstream carries the request ID.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestContext)
	r.Use(requestLogger(logger))
	r.Use(recoverer(logger))
	r.Use(middleware.Timeout(30 * time.Second))

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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
