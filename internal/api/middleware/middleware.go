// Package middleware : Wraps every request with the concerns that are not any
// one endpoint's business.
//
// What is here applies to the whole surface — identifying a request,
// recording it, and surviving a handler that panics — so it is kept apart
// from the modules that serve individual resources.
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// quietPaths : Paths logged at debug rather than info. Health checks are
// polled continuously and would otherwise crowd out real traffic.
var quietPaths = map[string]bool{
	"/health": true,
	"/ready":  true,
}

// RequestContext : Attaches the request identifier to the context, so that
// records logged anywhere downstream carry it without the logger being passed
// through each call.
//
// It must run after chi's RequestID.
func RequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if id := chimw.GetReqID(ctx); id != "" {
			ctx = logging.WithAttrs(ctx, slog.String("request_id", id))
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestLogger : Returns middleware recording one entry per completed
// request, at a level derived from the response status so that alerting can
// key on level alone.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

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

// Recoverer : Returns middleware turning a panic in a handler into a 500 and
// a logged stack trace.
//
// chi provides one, but it writes unstructured text to stderr, which would
// leave panics as the only events absent from structured logs.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// ErrAbortHandler is a deliberate signal from net/http, not a
				// fault.
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
