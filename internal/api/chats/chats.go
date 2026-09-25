// Package chats : Serves the prompts a user asks and the answers they get
// back.
//
// This is the module the rest of the server exists for. A prompt arrives, becomes
// a stored chat, and is handed to the runner; everything else here is about
// finding out what happened to it — by asking, by waiting, or by listening to
// the stream in stream.go.
package chats

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
)

const (
	// maxWait : The longest a create request will hold its connection open
	// when asked to wait for a result.
	maxWait = 60 * time.Second

	// waitPollInterval : How often a waiting request checks whether the chat
	// has finished.
	//
	// Polling is adequate for what wait is: a convenience for using the API
	// by hand. A client that needs messages as they happen uses the stream
	// instead, and is not served by this at all.
	waitPollInterval = 100 * time.Millisecond
)

// Runner : The part of the chat runner that the API requires.
//
// Taking an interface rather than the concrete runner keeps the handlers
// testable without executing anything.
type Runner interface {
	// Submit : Starts running a stored chat in the background.
	Submit(t *chat.Chat) error
	// Cancel : Stops a queued or running chat, reporting whether one was
	// found.
	Cancel(id string) bool
}

// Subscriber : Somewhere to listen for a chat's messages as they happen.
type Subscriber interface {
	// Subscribe : Returns a channel of a chat's events and a function that
	// ends the subscription.
	Subscribe(chatID string) (<-chan events.Event, func())
}

// CreateRequest : The body of a request to create a chat.
type CreateRequest struct {
	// Prompt : What the user asked for.
	Prompt string `json:"prompt"`
	// SessionID : The exchange to continue. Empty starts a new one.
	SessionID string `json:"session_id,omitempty"`
}

// ListResponse : The body of a listing of chats.
type ListResponse struct {
	Chats []views.Summary `json:"chats"`
}

// MessagesResponse : Everything a chat said while it ran.
type MessagesResponse struct {
	Messages []views.Message `json:"messages"`
}

// Handler : Serves the chat endpoints.
type Handler struct {
	httpx.Responder
	repo   chat.Repository
	runner Runner
	events Subscriber
}

// New : Builds the handler from the store, the runner that executes chats and
// the bus that carries what they say.
func New(logger *slog.Logger, repo chat.Repository, runner Runner, bus Subscriber) *Handler {
	return &Handler{
		Responder: httpx.Responder{Logger: logger},
		repo:      repo,
		runner:    runner,
		events:    bus,
	}
}

// Mount : Registers the chat endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/chats", func(r chi.Router) {
		r.Post("/", h.Create)
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Get("/{id}/messages", h.Messages)
		r.Get("/{id}/stream", h.Stream)
		r.Post("/{id}/cancel", h.Cancel)
	})
}

// Create : Accepts a prompt, stores it as a chat and starts it running.
//
// It answers 202 at once. Given a wait parameter it holds the connection
// until the chat finishes or the wait elapses, answering 200 with the
// finished chat if it lands in time.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	wait, err := parseWait(r.URL.Query().Get("wait"))
	if err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	caller := authn.Of(ctx)
	sessionID, err := h.sessionFor(ctx, caller, req.SessionID)
	if err != nil {
		if errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrNotOwned) {
			httpx.WriteError(ctx, w, http.StatusNotFound, "No such session.")
			return
		}
		h.Fail(ctx, w, "resolving session", err)
		return
	}

	// Speaking again supersedes whatever is still running in this session.
	// Someone who talks over an answer wants the new thing, not both, and two
	// answers cannot be listened to at once.
	h.supersede(ctx, sessionID)

	// From the caller, as on the other endpoint. Which endpoint was used
	// says nothing about whether there was a way to confirm before acting.
	t, err := chat.New(sessionID, caller.Client.Channel, req.Prompt)
	switch {
	case errors.Is(err, chat.ErrEmptyPrompt):
		httpx.WriteError(ctx, w, http.StatusBadRequest, "A prompt is required.")
		return
	case errors.Is(err, chat.ErrPromptTooLong):
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That prompt is too long.")
		return
	case err != nil:
		h.Fail(ctx, w, "creating chat", err)
		return
	}

	if err := h.repo.Create(ctx, t); err != nil {
		h.Fail(ctx, w, "storing chat", err)
		return
	}
	if err := h.runner.Submit(t); err != nil {
		// The chat is stored but will never run, so say so rather than
		// leaving it pending for ever.
		h.Logger.ErrorContext(ctx, "cannot submit chat", slog.Any("error", err))
		if failErr := t.Fail("That could not be started."); failErr == nil {
			_ = h.repo.Update(ctx, t)
		}
		httpx.WriteError(ctx, w, http.StatusServiceUnavailable, "Not accepting work at the moment.")
		return
	}

	h.Logger.InfoContext(ctx, "chat accepted", slog.String("chat_id", t.ID))

	if wait > 0 {
		if finished := h.awaitChat(ctx, t.ID, wait); finished != nil {
			httpx.WriteJSON(ctx, w, http.StatusOK, views.OfChat(finished))
			return
		}
	}
	httpx.WriteJSON(ctx, w, http.StatusAccepted, views.OfChat(t))
}

// Get : Returns one chat, including its response once it has one.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	t, _ := h.loadChat(ctx, w, chi.URLParam(r, "id"))
	if t == nil {
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, views.OfChat(t))
}

// List : Returns recent chats, newest first, without their responses.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := chat.Filter{UserID: authn.Of(ctx).User.ID}
	if status := r.URL.Query().Get("status"); status != "" {
		if !chat.Status(status).Valid() {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "Unknown status: "+status)
			return
		}
		filter.Status = chat.Status(status)
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "The limit must be a positive whole number.")
			return
		}
		filter.Limit = limit
	}

	summaries, err := h.repo.List(ctx, filter)
	if err != nil {
		h.Fail(ctx, w, "listing chats", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Chats: views.OfSummaries(summaries)})
}

// Messages : Returns everything a chat said while it ran.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if t, _ := h.loadChat(ctx, w, id); t == nil {
		return
	}

	messages, err := h.repo.Messages(ctx, id)
	if err != nil {
		h.Fail(ctx, w, "reading chat messages", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, MessagesResponse{Messages: views.OfMessages(messages)})
}

// Cancel : Stops a chat that has not finished.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	t, _ := h.loadChat(ctx, w, id)
	if t == nil {
		return
	}
	if t.Status.IsTerminal() {
		httpx.WriteError(ctx, w, http.StatusConflict, "That chat has already finished.")
		return
	}

	// The runner holds every chat it has been given, queued or running, so
	// this is the usual path.
	if h.runner.Cancel(id) {
		h.Logger.InfoContext(ctx, "chat cancellation requested", slog.String("chat_id", id))
		httpx.WriteJSON(ctx, w, http.StatusAccepted, views.OfChat(t))
		return
	}

	// Nothing is working on it, which happens to a chat left pending by a
	// process that stopped. Stop it here instead.
	if err := t.Cancel(); err != nil {
		h.Fail(ctx, w, "cancelling chat", err)
		return
	}
	if err := h.repo.Update(ctx, t); err != nil {
		h.Fail(ctx, w, "cancelling chat", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusAccepted, views.OfChat(t))
}

// sessionFor : Returns the session a prompt belongs in.
//
// A prompt lands in the session this client is currently in, which is the
// point of holding one per client: a person may be speaking to a speaker in
// one room while typing at a laptop in another, and the two should not
// collide. Naming a session overrides that for one prompt without switching
// what the client is in.
func (h *Handler) sessionFor(ctx context.Context, c *authn.Caller, requested string) (string, error) {
	if requested == "" {
		return chat.ActiveSession(ctx, h.repo, c.User.ID, c.Client.ID, c.Client.ActiveSessionID)
	}

	if !chat.ValidSessionID(requested) {
		return "", chat.ErrNotFound
	}
	session, err := h.repo.GetSession(ctx, requested)
	if err != nil {
		return "", err
	}
	// Owned by the person, not the client, so any of their clients may use
	// any of their sessions.
	if session.UserID != c.User.ID {
		return "", chat.ErrNotOwned
	}
	return requested, nil
}

// supersede : Stops whatever is still running in a session.
//
// Speaking again means the previous answer is no longer wanted, and two
// cannot be listened to at once. A failure here is logged rather than
// refused: the new prompt matters more than tidying the old one, and a chat
// left running still reaches a terminal status on its own.
func (h *Handler) supersede(ctx context.Context, sessionID string) {
	unfinished, err := h.repo.Unfinished(ctx, sessionID)
	if err != nil {
		h.Logger.ErrorContext(ctx, "cannot find chats to supersede", slog.Any("error", err))
		return
	}

	for _, id := range unfinished {
		if h.runner.Cancel(id) {
			h.Logger.InfoContext(ctx, "superseded by a new prompt", slog.String("chat_id", id))
			continue
		}
		// Nothing is working on it, which happens to a chat left behind by a
		// process that stopped. Stop it here instead.
		t, err := h.repo.Get(ctx, id)
		if err != nil || t.Status.IsTerminal() {
			continue
		}
		if err := t.Cancel(); err == nil {
			_ = h.repo.Update(ctx, t)
		}
	}
}

// loadChat : Reads a chat, writing the response itself when it cannot. It
// returns nil when the caller should stop.
func (h *Handler) loadChat(ctx context.Context, w http.ResponseWriter, id string) (*chat.Chat, error) {
	if !chat.ValidID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such chat.")
		return nil, nil
	}

	t, err := h.repo.Get(ctx, id)
	if errors.Is(err, chat.ErrNotFound) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such chat.")
		return nil, err
	}
	if err != nil {
		h.Fail(ctx, w, "reading chat", err)
		return nil, err
	}

	// One user must never read another's chat. Answered as missing rather
	// than forbidden, so the existence of it is not revealed either.
	if !h.ownedByCaller(ctx, t.SessionID) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such chat.")
		return nil, chat.ErrNotOwned
	}
	return t, nil
}

// ownedByCaller : Reports whether a session belongs to the calling user. One
// predating users belongs to nobody and is hidden.
func (h *Handler) ownedByCaller(ctx context.Context, sessionID string) bool {
	c := authn.Of(ctx)
	if c == nil || sessionID == "" {
		return false
	}
	session, err := h.repo.GetSession(ctx, sessionID)
	if err != nil {
		return false
	}
	return session.UserID == c.User.ID
}

// awaitChat : Waits for a chat to finish, returning it if it does within the
// given time and nil otherwise.
func (h *Handler) awaitChat(ctx context.Context, id string, wait time.Duration) *chat.Chat {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(waitPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline.C:
			return nil
		case <-ticker.C:
			t, err := h.repo.Get(ctx, id)
			if err != nil {
				// Report the chat as unfinished rather than failing the
				// request; it is still running and can be fetched later.
				h.Logger.ErrorContext(ctx, "cannot poll chat while waiting",
					slog.String("chat_id", id), slog.Any("error", err))
				return nil
			}
			if t.Status.IsTerminal() {
				return t
			}
		}
	}
}

// parseWait : Reads the wait parameter, which may be empty.
func parseWait(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	wait, err := time.ParseDuration(raw)
	if err != nil {
		return 0, errors.New("wait must be a duration such as 30s")
	}
	if wait < 0 {
		return 0, errors.New("wait must not be negative")
	}
	if wait > maxWait {
		wait = maxWait
	}
	return wait, nil
}
