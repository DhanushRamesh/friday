// Package health : Answers the two questions a supervisor and a load balancer
// ask.
//
// They are deliberately different: liveness says the process is running, and
// readiness says it can do useful work. Conflating them makes a database
// outage restart a perfectly good process.
package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/friday/internal/api/httpx"
)

// readinessTimeout : Bounds the dependency checks made by the readiness
// endpoint.
const readinessTimeout = 2 * time.Second

// Pinger : The part of a database handle that the readiness check requires.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler : Serves the liveness and readiness endpoints.
type Handler struct {
	httpx.Responder
	db Pinger
}

// New : Builds the handler from the dependencies it reports on.
func New(logger *slog.Logger, db Pinger) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, db: db}
}

// Mount : Registers the endpoints on r. They sit at the root rather than
// under a version prefix, because what probes them is infrastructure that
// should not have to track FRIDAY's API version.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/health", h.Live)
	r.Get("/ready", h.Ready)
}

// Live : Reports that the process is running.
//
// It touches no dependencies, so that an outage in one cannot cause a
// supervisor to restart a working process.
func (h *Handler) Live(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready : Reports whether FRIDAY's dependencies are usable, answering 503
// when any check fails.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	checks := map[string]string{"database": "ok"}
	status := http.StatusOK

	if err := h.db.Ping(ctx); err != nil {
		// The cause goes to the log, not to the caller.
		checks["database"] = "unreachable"
		status = http.StatusServiceUnavailable
		h.Logger.ErrorContext(ctx, "readiness check failed",
			slog.String("check", "database"),
			slog.Any("error", err))
	}

	body := map[string]any{"status": "ready", "checks": checks}
	if status != http.StatusOK {
		body["status"] = "not ready"
	}
	httpx.WriteJSON(ctx, w, status, body)
}
