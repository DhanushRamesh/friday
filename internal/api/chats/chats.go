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
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
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
	// ConversationID : The exchange to continue. Empty starts a new one.
	ConversationID string `json:"conversation_id,omitempty"`
}

// ListResponse : The body of a listing of chats.
type ListResponse struct {
	Chats []views.Summary `json:"chats"`
}

// Handler : Serves the chat endpoints.
type Handler struct {
	httpx.Responder
	repo     chat.Repository
	messages conversation.Repository
	runner   Runner
	events   Subscriber
}

// New : Builds the handler from the store, the runner that executes chats and
// the bus that carries what they say.
func New(logger *slog.Logger, repo chat.Repository, messages conversation.Repository, runner Runner, bus Subscriber) *Handler {
	return &Handler{
		Responder: httpx.Responder{Logger: logger},
		repo:      repo,
		messages:  messages,
		runner:    runner,
		events:    bus,
	}
}

// Mount : Registers the chat endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	// Reading and stopping only. A prompt is submitted at /api/chat, which
	// is the single way in whatever is asking.
	r.Route("/v1/chats", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Get("/{id}/steps", h.Steps)
		r.Post("/{id}/cancel", h.Cancel)
	})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	t, _ := h.loadChat(ctx, w, chi.URLParam(r, "id"))
	if t == nil {
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, views.OfChat(t))
}

// Steps : How one answer was made, in the order it happened.
//
// Everything here was recorded as the answer was produced. Nothing is
// re-derived: a search run now could disagree with the one the model was
// actually shown.
func (h *Handler) Steps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	t, _ := h.loadChat(ctx, w, chi.URLParam(r, "id"))
	if t == nil {
		return
	}

	var wrote []conversation.Message
	if h.messages != nil {
		var err error
		if wrote, err = h.messages.ByChat(ctx, t.ID); err != nil {
			h.Fail(ctx, w, "reading how the answer was made", err)
			return
		}
	}

	httpx.WriteJSON(ctx, w, http.StatusOK, views.OfTimeline(t, wrote))
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

// conversationFor : Returns the conversation a prompt belongs in.
//
// A prompt lands in the conversation this client is currently in, which is the
// point of holding one per client: a person may be speaking to a speaker in
// one room while typing at a laptop in another, and the two should not
// collide. Naming a conversation overrides that for one prompt without switching
// what the client is in.
func (h *Handler) conversationFor(ctx context.Context, c *authn.Caller, requested string) (string, error) {
	if requested == "" {
		return chat.ActiveConversation(ctx, h.repo, c.User.ID, c.Client.ID, c.Client.ActiveConversationID)
	}

	if !chat.ValidConversationID(requested) {
		return "", chat.ErrNotFound
	}
	conversation, err := h.repo.GetConversation(ctx, requested)
	if err != nil {
		return "", err
	}
	// Owned by the person, not the client, so any of their clients may use
	// any of their conversations.
	if conversation.UserID != c.User.ID {
		return "", chat.ErrNotOwned
	}
	return requested, nil
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
	if !h.ownedByCaller(ctx, t.ConversationID) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such chat.")
		return nil, chat.ErrNotOwned
	}
	return t, nil
}

// ownedByCaller : Reports whether a conversation belongs to the calling user. One
// predating users belongs to nobody and is hidden.
func (h *Handler) ownedByCaller(ctx context.Context, conversationID string) bool {
	c := authn.Of(ctx)
	if c == nil || conversationID == "" {
		return false
	}
	conversation, err := h.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return false
	}
	return conversation.UserID == c.User.ID
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
