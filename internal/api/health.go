package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// readinessTimeout : Bounds the dependency checks made by handleReady.
const readinessTimeout = 2 * time.Second

// handleHealth : Reports that the process is running.
//
// It touches no dependencies, so that an outage in one cannot cause a
// supervisor to restart a working process.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady : Reports whether FRIDAY's dependencies are usable, answering
// 503 when any check fails.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	checks := map[string]string{"database": "ok"}
	status := http.StatusOK

	if err := s.db.Ping(ctx); err != nil {
		// The cause goes to the log, not to the caller.
		checks["database"] = "unreachable"
		status = http.StatusServiceUnavailable
		s.logger.ErrorContext(ctx, "readiness check failed",
			slog.String("check", "database"),
			slog.Any("error", err))
	}

	body := map[string]any{"status": "ready", "checks": checks}
	if status != http.StatusOK {
		body["status"] = "not ready"
	}
	writeJSON(ctx, w, status, body)
}
