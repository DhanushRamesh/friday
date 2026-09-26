// Package reminders serves the endpoints for seeing and calling off what
// is waiting to be said.
//
// The assistant can already do both by being asked. This is for looking at
// the list, which is not a thing to do out loud.
package reminders

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// ListResponse : What a listing answers with.
type ListResponse struct {
	Reminders []views.Reminder `json:"reminders"`
}

// Handler : Serves the reminder endpoints.
type Handler struct {
	httpx.Responder
	store remind.Store
}

// New : Builds the handler from the store holding the reminders. A nil
// store serves empty listings, which is what a server with nowhere to keep
// them has.
func New(logger *slog.Logger, store remind.Store) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store}
}

// Mount : Registers the reminder endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/reminders", func(r chi.Router) {
		r.Get("/", h.List)
		r.Delete("/{id}", h.Cancel)
	})
}

// List : What is waiting to be said, soonest first.
//
// Only what is still coming, unless asked otherwise. A list of everything
// that ever fired is a log, and nobody opens a settings screen for one.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Reminders: []views.Reminder{}})
		return
	}

	states := []remind.Status{remind.Pending}
	if r.URL.Query().Get("all") == "true" {
		states = nil
	}

	found, err := h.store.List(ctx, authn.Of(ctx).User.ID, states...)
	if err != nil {
		h.Fail(ctx, w, "listing reminders", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Reminders: views.OfReminders(found)})
}

// Cancel : Calls one off.
//
// Answered as missing when it belongs to somebody else, so the existence
// of another person's reminder is not revealed either.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if h.store == nil || !remind.ValidID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such reminder.")
		return
	}

	user := authn.Of(ctx).User.ID
	existing, err := h.store.Get(ctx, user, id)
	if errors.Is(err, remind.ErrNotFound) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such reminder.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "reading reminder", err)
		return
	}

	if err := h.store.Cancel(ctx, user, id); err != nil {
		h.Fail(ctx, w, "cancelling reminder", err)
		return
	}

	existing.Status = remind.Cancelled
	httpx.WriteJSON(ctx, w, http.StatusOK, views.OfReminder(*existing))
}
