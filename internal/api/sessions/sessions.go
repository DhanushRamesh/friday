// Package sessions : Serves the threads a user talks in.
//
// A session is owned by the user, not by the client they are using, so every
// one of their clients can see and switch to any of them. Which session a
// given client is currently in is what keeps a speaker in the kitchen and a
// laptop in the study from talking over each other.
package sessions

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// CreateRequest : The body of a request to start a session.
type CreateRequest struct {
	// Title : What to call it in a listing. Optional.
	Title string `json:"title,omitempty"`
	// Activate : Whether this client switches to it. Defaults to true.
	Activate bool `json:"activate,omitempty"`
}

// ListResponse : The body of a listing of sessions.
type ListResponse struct {
	Sessions []views.Session `json:"sessions"`
}

// DetailResponse : A session together with its chats.
type DetailResponse struct {
	Session views.Session   `json:"session"`
	Chats   []views.Summary `json:"chats"`
}

// Handler : Serves the session endpoints.
type Handler struct {
	httpx.Responder
	repo chat.Repository
}

// New : Builds the handler from the store holding the sessions.
func New(logger *slog.Logger, repo chat.Repository) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, repo: repo}
}

// Mount : Registers the session endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/sessions", func(r chi.Router) {
		r.Post("/", h.Create)
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Post("/{id}/activate", h.Activate)
	})
}

// Create : Starts a new session for the caller.
//
// It belongs to the user, so every one of their clients can see and switch to
// it. This client moves into it unless the caller asks otherwise, since
// starting a session almost always means wanting to talk in it.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)

	var req CreateRequest
	req.Activate = true
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
			return
		}
	}

	session := chat.NewSession(c.User.ID, req.Title)
	if err := h.repo.CreateSession(ctx, session); err != nil {
		h.Fail(ctx, w, "creating session", err)
		return
	}

	if req.Activate {
		if err := h.repo.SetActiveSession(ctx, c.User.ID, c.Client.ID, session.ID); err != nil {
			h.Fail(ctx, w, "activating session", err)
			return
		}
	}

	h.Logger.InfoContext(ctx, "session created",
		slog.String("session_id", session.ID),
		slog.Bool("active", req.Activate))
	httpx.WriteJSON(ctx, w, http.StatusCreated, views.OfSession(*session, req.Activate))
}

// List : Returns sessions, most recently used first.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "The limit must be a positive whole number.")
			return
		}
		limit = parsed
	}

	c := authn.Of(ctx)
	sessions, err := h.repo.ListSessions(ctx, c.User.ID, limit)
	if err != nil {
		h.Fail(ctx, w, "listing sessions", err)
		return
	}

	out := make([]views.Session, len(sessions))
	for i, session := range sessions {
		out[i] = views.OfSession(session, session.ID == c.Client.ActiveSessionID)
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Sessions: out})
}

// Get : Returns a session with the chats belonging to it, oldest first.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if !chat.ValidSessionID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such session.")
		return
	}

	session, err := h.repo.GetSession(ctx, id)
	if errors.Is(err, chat.ErrNotFound) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such session.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "reading session", err)
		return
	}
	c := authn.Of(ctx)
	if session.UserID != c.User.ID {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such session.")
		return
	}

	summaries, err := h.repo.List(ctx, chat.Filter{SessionID: id, Limit: chat.MaxListLimit})
	if err != nil {
		h.Fail(ctx, w, "listing session chats", err)
		return
	}

	// Oldest first, so the exchange reads in the order it happened. Sorted
	// here rather than relying on the order a listing happens to return:
	// identifiers are ULIDs, so sorting them sorts by creation time.
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })

	httpx.WriteJSON(ctx, w, http.StatusOK, DetailResponse{
		Session: views.OfSession(*session, session.ID == c.Client.ActiveSessionID),
		Chats:   views.OfSummaries(summaries),
	})
}

// Activate : Switches where a prompt from this client lands.
//
// Only this client moves. Another client of the same user stays where it was,
// which is what lets a speaker and a laptop hold separate threads.
func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidSessionID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such session.")
		return
	}

	err := h.repo.SetActiveSession(ctx, c.User.ID, c.Client.ID, id)
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		// Answered alike: telling one user that another's session exists
		// reveals more than it should.
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such session.")
		return
	case err != nil:
		h.Fail(ctx, w, "activating session", err)
		return
	}

	session, err := h.repo.GetSession(ctx, id)
	if err != nil {
		h.Fail(ctx, w, "reading session", err)
		return
	}

	h.Logger.InfoContext(ctx, "active session switched", slog.String("session_id", id))
	httpx.WriteJSON(ctx, w, http.StatusOK, views.OfSession(*session, true))
}
