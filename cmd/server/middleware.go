package main

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// quietPaths are logged at debug level. Health checks are polled continuously
// by load balancers and would otherwise drown out real traffic.
var quietPaths = map[string]bool{
	"/health": true,
	"/ready":  true,
}

// requestContext stamps the request ID onto the context so that every line
// logged while serving the request carries it, including lines written deep in
// the agent and tool layers that never see the *http.Request.
//
// It must run after chi's middleware.RequestID.
func requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if id := middleware.GetReqID(ctx); id != "" {
			ctx = logging.WithAttrs(ctx, slog.String("request_id", id))
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestLogger emits one record per completed request. Server errors log at
// error level and client errors at warn, so alerting can key off level alone.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			level := slog.LevelInfo
			switch {
			case ww.Status() >= http.StatusInternalServerError:
				level = slog.LevelError
			case ww.Status() >= http.StatusBadRequest:
				level = slog.LevelWarn
			case quietPaths[r.URL.Path]:
				level = slog.LevelDebug
			}

			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// recoverer converts a panic in a handler into a 500 and a logged stack trace.
//
// chi ships its own, but it writes the trace to stderr as unstructured text,
// which means a panic would be the one event missing from structured logs.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// ErrAbortHandler is a deliberate signal from the stdlib, not a bug.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}

				logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.Any("panic", rec),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}()

			next.ServeHTTP(w, r)
		})
	}
}
