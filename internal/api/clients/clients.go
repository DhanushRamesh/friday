// Package clients : Serves the credentials a user has issued to themselves.
//
// A client is one place a person talks to the server from — a phone, a speaker, a
// browser tab — and holds one token. This module is how they see what they
// have issued and take one back.
package clients

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/llm"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
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

// ModelsResponse : The models a client may be set to answer with.
type ModelsResponse struct {
	Models []views.Model `json:"models"`
	// Default : The model answering a client that has chosen none, so that
	// "server default" can be shown as the name of something rather than as
	// a word standing for an answer nobody is told.
	Default string `json:"default,omitempty"`
}

// ModelRequest : Choosing which model answers a client.
type ModelRequest struct {
	// Vendor, Model : What to set. An empty Model clears the choice and
	// leaves the server's configured one.
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
}

// PersonasResponse : The manners the assistant can answer in, and the one it
// is answering in now.
type PersonasResponse struct {
	Personas []views.Persona `json:"personas"`
	Current  string          `json:"current"`
	// AnnounceTitles : Whether a conversation's new name is said aloud on
	// voice. On unless it has been turned off.
	AnnounceTitles bool `json:"announce_titles"`
}

// PersonaRequest : Choosing the manner the assistant answers in.
type PersonaRequest struct {
	Persona string `json:"persona"`
	// AnnounceTitles : Whether a conversation's new name is said aloud.
	// Absent leaves it as it was.
	AnnounceTitles *bool `json:"announce_titles,omitempty"`
}

// Handler : Serves the client endpoints.
type Handler struct {
	httpx.Responder
	repo chat.Repository
	// models : What the configured provider can actually reach. Offering
	// anything else would offer a model the server cannot call.
	models []llm.Model
	// defaultModel : The identifier of the model answering a client that has
	// chosen none.
	defaultModel string
	// persona : The manner in use. Held in memory and shared with the
	// runner, so a change here is answered in by the next prompt.
	persona *persona.Setting
}

// New : Builds the handler from the store holding the clients, the models the
// provider in use can reach, and the one it falls back to.
func New(
	logger *slog.Logger,
	repo chat.Repository,
	models []llm.Model,
	defaultModel string,
	manner *persona.Setting,
) *Handler {
	return &Handler{
		Responder:    httpx.Responder{Logger: logger},
		repo:         repo,
		models:       models,
		defaultModel: defaultModel,
		persona:      manner,
	}
}

// Mount : Registers the client endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/clients", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/{id}/channel", h.SetChannel)
		r.Post("/{id}/model", h.SetModel)
		r.Delete("/{id}", h.Revoke)
	})
	r.Route("/v1/models", func(r chi.Router) {
		r.Get("/", h.Models)
	})
	r.Route("/v1/personas", func(r chi.Router) {
		r.Get("/", h.Personas)
		r.Post("/", h.SetPersona)
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

// List : Returns the caller's clients, the usable ones by default.
//
// ?revoked=true returns the revoked ones instead. They are kept out of the
// ordinary listing because a revoked client cannot authenticate, cannot be
// brought back, and signing in with its identifier registers a new one rather
// than reviving it — so it is a record of something that happened, not a row
// to act on, and it would otherwise accumulate for ever.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)

	revoked := r.URL.Query().Get("revoked") == "true"
	clients, err := h.repo.ListClients(ctx, c.User.ID, revoked)
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

// Models : Lists the models a client may be set to answer with.
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	out := make([]views.Model, 0, len(h.models))
	for _, m := range h.models {
		out = append(out, views.OfModel(m))
	}
	httpx.WriteJSON(r.Context(), w, http.StatusOK, ModelsResponse{
		Models:  out,
		Default: h.defaultModel,
	})
}

// SetModel : Chooses which model answers a client's prompts.
//
// A client may set its own, unlike its channel. The channel decides what the
// assistant is allowed to do about a prompt, so letting a client raise it
// would let it grant itself privileges; which model answers grants nothing,
// and being able to change it from the client you are sitting at is the
// obvious way to want to do it.
func (h *Handler) SetModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidClientID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such client.")
		return
	}

	var req ModelRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	model := chat.NewModel(req.Vendor, req.Model)
	if model.Chosen() && !h.offers(model) {
		httpx.WriteError(ctx, w, http.StatusBadRequest,
			"That is not a model this assistant can reach.")
		return
	}

	err := h.repo.SetClientModel(ctx, c.User.ID, id, model)
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such client.")
		return
	case err != nil:
		h.Fail(ctx, w, "setting client model", err)
		return
	}

	h.List(w, r)
}

// offers : Whether the provider in use can reach the given model.
//
// Checked here rather than left to fail at the moment somebody speaks, since
// a model that cannot be called is a setting that silently breaks every later
// prompt from that client.
func (h *Handler) offers(m chat.Model) bool {
	for _, known := range h.models {
		if strings.EqualFold(known.ID, m.ID) && strings.EqualFold(known.Vendor, m.Vendor) {
			return true
		}
	}
	return false
}

// Personas : Lists the manners the assistant can answer in.
func (h *Handler) Personas(w http.ResponseWriter, r *http.Request) {
	out := make([]views.Persona, 0)
	for _, p := range persona.All() {
		out = append(out, views.OfPersona(p))
	}
	httpx.WriteJSON(r.Context(), w, http.StatusOK, PersonasResponse{
		Personas:       out,
		Current:        h.current(),
		AnnounceTitles: h.announcingTitles(r.Context()),
	})
}

// SetPersona : Chooses the manner the assistant answers in.
//
// It takes effect on the next prompt, and is stored so that it survives a
// restart.
func (h *Handler) SetPersona(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req PersonaRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	if req.AnnounceTitles != nil {
		value := "on"
		if !*req.AnnounceTitles {
			value = "off"
		}
		if err := h.repo.SetSetting(ctx, conversation.AnnounceTitlesSetting, value); err != nil {
			h.Logger.ErrorContext(ctx, "cannot store whether to announce names", slog.Any("error", err))
		}
	}

	// Changing only the announcement is allowed: an empty persona then means
	// "leave the manner alone" rather than "choose nothing".
	if req.Persona == "" && req.AnnounceTitles != nil {
		h.Personas(w, r)
		return
	}

	if h.persona == nil || !h.persona.Set(req.Persona) {
		httpx.WriteError(ctx, w, http.StatusBadRequest,
			"That is not a manner this assistant knows.")
		return
	}

	// Stored after it is applied, not before: the manner in memory is what
	// answers, and a write that fails should not leave the assistant
	// speaking in a manner nobody chose. A failure here costs the choice its
	// permanence and nothing else, so it is logged rather than returned.
	if err := h.repo.SetSetting(ctx, persona.SettingName, h.persona.Current()); err != nil {
		h.Logger.ErrorContext(ctx, "cannot store the chosen manner",
			slog.String("persona", h.persona.Current()), slog.Any("error", err))
	}

	h.Personas(w, r)
}

// current : The manner in use, or the default when none is configured.
func (h *Handler) current() string {
	if h.persona == nil {
		return persona.Default
	}
	return h.persona.Current()
}

// announcingTitles : Whether a conversation's new name is said aloud.
//
// On unless it has been turned off, and on when the setting cannot be read:
// somebody who cannot see a listing has no other way of learning the name.
func (h *Handler) announcingTitles(ctx context.Context) bool {
	got, err := h.repo.Setting(ctx, conversation.AnnounceTitlesSetting)
	if err != nil {
		h.Logger.ErrorContext(ctx, "cannot read whether to announce names", slog.Any("error", err))
		return true
	}
	return got != "off"
}
