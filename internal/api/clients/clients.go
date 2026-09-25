// Package clients : Serves the credentials a user has issued to themselves.
//
// A client is one place a person talks to the server from — a phone, a speaker, a
// browser tab — and holds one token. This module is how they see what they
// have issued and take one back.
package clients

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// ListResponse : The body of a listing of clients.
type ListResponse struct {
	Clients []views.Client `json:"clients"`
}

// MeResponse : Who is calling and from what.
type MeResponse struct {
	User   views.User   `json:"user"`
	Client views.Client `json:"client"`
}

// Handler : Serves the client endpoints.
type Handler struct {
	httpx.Responder
	repo chat.Repository
}

// New : Builds the handler from the store holding the clients.
func New(logger *slog.Logger, repo chat.Repository) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, repo: repo}
}

// Mount : Registers the client endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/clients", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/{id}/channel", h.SetChannel)
		r.Delete("/{id}", h.Revoke)
	})
	r.Route("/v1/me", func(r chi.Router) {
		r.Get("/", h.Me)
	})
}

// Me : Returns the user, the client in use, and where a prompt from it will
// land.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	httpx.WriteJSON(ctx, w, http.StatusOK, MeResponse{
		User:   views.OfUser(c.User),
		Client: views.OfClient(*c.Client, true),
	})
}

// List : Returns the caller's clients, revoked ones included so that a
// revocation is visible rather than silently absent.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)

	clients, err := h.repo.ListClients(ctx, c.User.ID)
	if err != nil {
		h.Fail(ctx, w, "listing clients", err)
		return
	}

	out := make([]views.Client, len(clients))
	for i, d := range clients {
		out[i] = views.OfClient(d, d.ID == c.Client.ID)
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Clients: out})
}

// ChannelRequest : The body of a request to change a client's channel.
type ChannelRequest struct {
	// Channel : "voice" or "direct".
	Channel string `json:"channel"`
}

// SetChannel : Changes how a client's prompts are treated.
//
// A client says what it is when it registers, and some cannot. Home Assistant
// is handed a token through a configuration screen with no field for it, so
// its client registers as direct — the answer that grants more — and there
// would otherwise be no way to correct that short of the database.
func (h *Handler) SetChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidClientID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such client.")
		return
	}

	// Not its own. This is authenticated by the very token whose privileges
	// it would raise, so a client allowed to set its own channel could
	// promote itself out of whatever the channel restricts — which defeats
	// the point of having it. Correcting one is done from another client,
	// which is the realistic flow anyway: Home Assistant cannot call this at
	// all, and the browser is where its channel gets fixed.
	if id == c.Client.ID {
		httpx.WriteError(ctx, w, http.StatusForbidden,
			"A client cannot change its own channel. Use another one.")
		return
	}

	var req ChannelRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	err := h.repo.SetClientChannel(ctx, c.User.ID, id, chat.Channel(req.Channel))
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such client.")
		return
	case errors.Is(err, chat.ErrUnknownChannel):
		httpx.WriteError(ctx, w, http.StatusBadRequest,
			`The channel must be "voice" or "direct".`)
		return
	case err != nil:
		h.Fail(ctx, w, "setting client channel", err)
		return
	}

	h.Logger.InfoContext(ctx, "client channel changed",
		slog.String("changed_client_id", id), slog.String("channel", req.Channel))
	h.List(w, r)
}

// Revoke : Stops one of the caller's clients authenticating.
//
// Any of a user's clients may revoke any other, which is the point: a phone
// left in a taxi is revoked from the laptop at home.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidClientID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such client.")
		return
	}

	err := h.repo.RevokeClient(ctx, c.User.ID, id)
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such client.")
		return
	case err != nil:
		h.Fail(ctx, w, "revoking client", err)
		return
	}

	h.Logger.InfoContext(ctx, "client revoked", slog.String("revoked_client_id", id))
	w.WriteHeader(http.StatusNoContent)
}
