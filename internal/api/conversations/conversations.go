// Package conversations : Serves the threads a user talks in.
//
// A conversation is owned by the user, not by the client they are using, so every
// one of their clients can see and switch to any of them. Which conversation a
// given client is currently in is what keeps a speaker in the kitchen and a
// laptop in the study from talking over each other.
package conversations

import (
	"context"
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

// CreateRequest : The body of a request to start a conversation.
type CreateRequest struct {
	// Title : What to call it in a listing. Optional.
	Title string `json:"title,omitempty"`
	// Activate : Whether this client switches to it. Defaults to true.
	Activate bool `json:"activate,omitempty"`
}

// RenameRequest : The body of a request to rename a conversation.
type RenameRequest struct {
	// Title : What to call it. Empty clears the name rather than being
	// refused: a title given by mistake should be removable without deleting
	// the conversation.
	Title string `json:"title"`
}

// RemovedResponse : What is left after a conversation is put away or deleted.
type RemovedResponse struct {
	// Active : The conversation this client is now in.
	//
	// Removing the one it was using leaves it with nowhere to talk, so a
	// fresh conversation is started and named here. Removing any other conversation
	// leaves this as the one that was already active.
	Active views.Conversation `json:"active"`
}

// ListResponse : The body of a listing of conversations.
type ListResponse struct {
	Conversations []views.Conversation `json:"conversations"`
}

// DetailResponse : A conversation together with its chats.
type DetailResponse struct {
	Conversation views.Conversation `json:"conversation"`
	Chats        []views.Summary    `json:"chats"`
}

// Handler : Serves the conversation endpoints.
type Handler struct {
	httpx.Responder
	repo chat.Repository
}

// New : Builds the handler from the store holding the conversations.
func New(logger *slog.Logger, repo chat.Repository) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, repo: repo}
}

// Mount : Registers the conversation endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/conversations", func(r chi.Router) {
		r.Post("/", h.Create)
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Post("/{id}/activate", h.Activate)
		r.Post("/{id}/rename", h.Rename)
		r.Post("/{id}/archive", h.Archive)
		r.Post("/{id}/unarchive", h.Unarchive)
		r.Delete("/{id}", h.Delete)
	})
}

// Create : Starts a new conversation for the caller.
//
// It belongs to the user, so every one of their clients can see and switch to
// it. This client moves into it unless the caller asks otherwise, since
// starting a conversation almost always means wanting to talk in it.
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

	conversation := chat.NewConversation(c.User.ID, req.Title)
	if err := h.repo.CreateConversation(ctx, conversation); err != nil {
		h.Fail(ctx, w, "creating conversation", err)
		return
	}

	if req.Activate {
		if err := h.repo.SetActiveConversation(ctx, c.User.ID, c.Client.ID, conversation.ID); err != nil {
			h.Fail(ctx, w, "activating conversation", err)
			return
		}
	}

	h.Logger.InfoContext(ctx, "conversation created",
		slog.String("conversation_id", conversation.ID),
		slog.Bool("active", req.Activate))
	httpx.WriteJSON(ctx, w, http.StatusCreated, views.OfConversation(*conversation, req.Activate))
}

// List : Returns conversations, most recently used first.
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

	// The two listings are separate rather than one with a flag, because an
	// archived conversation is not a lesser version of a live one: it is never
	// where a prompt lands, and mixing them would put it in reach of code
	// that takes the first row.
	archived := r.URL.Query().Get("archived") == "true"

	c := authn.Of(ctx)
	list := h.repo.ListConversations
	if archived {
		list = h.repo.ListArchivedConversations
	}
	conversations, err := list(ctx, c.User.ID, limit)
	if err != nil {
		h.Fail(ctx, w, "listing conversations", err)
		return
	}

	out := make([]views.Conversation, len(conversations))
	for i, conversation := range conversations {
		out[i] = views.OfConversation(conversation, conversation.ID == c.Client.ActiveConversationID)
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Conversations: out})
}

// Get : Returns a conversation with the chats belonging to it, oldest first.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if !chat.ValidConversationID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	conversation, err := h.repo.GetConversation(ctx, id)
	if errors.Is(err, chat.ErrNotFound) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "reading conversation", err)
		return
	}
	c := authn.Of(ctx)
	if conversation.UserID != c.User.ID {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	summaries, err := h.repo.List(ctx, chat.Filter{ConversationID: id, Limit: chat.MaxListLimit})
	if err != nil {
		h.Fail(ctx, w, "listing conversation chats", err)
		return
	}

	// Oldest first, so the exchange reads in the order it happened. Sorted
	// here rather than relying on the order a listing happens to return:
	// identifiers are ULIDs, so sorting them sorts by creation time.
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })

	httpx.WriteJSON(ctx, w, http.StatusOK, DetailResponse{
		Conversation: views.OfConversation(*conversation, conversation.ID == c.Client.ActiveConversationID),
		Chats:        views.OfSummaries(summaries),
	})
}

// Rename : Changes what a conversation is called.
//
// A POST rather than a PATCH, so the API keeps to the three methods the
// cross-origin rules already allow, and so it reads like the action beside
// it: /activate changes which conversation is current, /rename changes its name.
func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidConversationID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	var req RenameRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	err := h.repo.RenameConversation(ctx, c.User.ID, id, req.Title)
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		// Answered alike, as elsewhere: telling one user that another's
		// conversation exists reveals more than it should.
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	case errors.Is(err, chat.ErrTitleTooLong):
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That name is too long.")
		return
	case err != nil:
		h.Fail(ctx, w, "renaming conversation", err)
		return
	}

	conversation, err := h.repo.GetConversation(ctx, id)
	if err != nil {
		h.Fail(ctx, w, "reading conversation", err)
		return
	}

	h.Logger.InfoContext(ctx, "conversation renamed", slog.String("conversation_id", id))
	httpx.WriteJSON(ctx, w, http.StatusOK,
		views.OfConversation(*conversation, conversation.ID == c.Client.ActiveConversationID))
}

// Archive : Puts a conversation away, keeping everything said in it.
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, true)
}

// Unarchive : Brings an archived conversation back into the listing.
func (h *Handler) Unarchive(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, false)
}

// setArchived : The work behind Archive and Unarchive.
func (h *Handler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidConversationID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	err := h.repo.SetConversationArchived(ctx, c.User.ID, id, archived)
	if !h.resolved(ctx, w, err, "archiving conversation") {
		return
	}

	if !archived {
		conversation, err := h.repo.GetConversation(ctx, id)
		if err != nil {
			h.Fail(ctx, w, "reading conversation", err)
			return
		}
		h.Logger.InfoContext(ctx, "conversation unarchived", slog.String("conversation_id", id))
		httpx.WriteJSON(ctx, w, http.StatusOK, views.OfConversation(*conversation, false))
		return
	}

	h.Logger.InfoContext(ctx, "conversation archived", slog.String("conversation_id", id))
	h.writeActive(ctx, w, c, id)
}

// Delete : Removes a conversation and everything said in it.
//
// There is no undo. Archive is the reversible one, and is what a client
// should offer first.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidConversationID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	err := h.repo.DeleteConversation(ctx, c.User.ID, id)
	if !h.resolved(ctx, w, err, "deleting conversation") {
		return
	}

	h.Logger.InfoContext(ctx, "conversation deleted", slog.String("conversation_id", id))
	h.writeActive(ctx, w, c, id)
}

// writeActive : Answers with the conversation this client is now in, starting one
// when the conversation just removed was the one it was using.
//
// A fresh conversation rather than the most recent surviving one: having just put
// a conversation away, being dropped into an unrelated older one reads as the
// wrong thing happening.
func (h *Handler) writeActive(ctx context.Context, w http.ResponseWriter, c *authn.Caller, removed string) {
	if c.Client.ActiveConversationID != removed {
		conversation, err := h.repo.GetConversation(ctx, c.Client.ActiveConversationID)
		if err == nil {
			httpx.WriteJSON(ctx, w, http.StatusOK,
				RemovedResponse{Active: views.OfConversation(*conversation, true)})
			return
		}
		// Falls through to a new one: the client was pointed at something
		// that is no longer readable, which is the same problem.
	}

	fresh := chat.NewConversation(c.User.ID, "")
	if err := h.repo.CreateConversation(ctx, fresh); err != nil {
		h.Fail(ctx, w, "starting a replacement conversation", err)
		return
	}
	if err := h.repo.SetActiveConversation(ctx, c.User.ID, c.Client.ID, fresh.ID); err != nil {
		h.Fail(ctx, w, "activating the replacement conversation", err)
		return
	}

	h.Logger.InfoContext(ctx, "replacement conversation started",
		slog.String("conversation_id", fresh.ID))
	httpx.WriteJSON(ctx, w, http.StatusOK,
		RemovedResponse{Active: views.OfConversation(*fresh, true)})
}

// resolved : Reports whether an ownership-checked write succeeded, answering
// the caller when it did not.
//
// Not found and not owned are answered alike, as elsewhere: telling one user
// that another's conversation exists reveals more than it should.
func (h *Handler) resolved(ctx context.Context, w http.ResponseWriter, err error, doing string) bool {
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return false
	case err != nil:
		h.Fail(ctx, w, doing, err)
		return false
	}
	return true
}

// Activate : Switches where a prompt from this client lands.
//
// Only this client moves. Another client of the same user stays where it was,
// which is what lets a speaker and a laptop hold separate threads.
func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := authn.Of(ctx)
	id := chi.URLParam(r, "id")

	if !chat.ValidConversationID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	err := h.repo.SetActiveConversation(ctx, c.User.ID, c.Client.ID, id)
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		// Answered alike: telling one user that another's conversation exists
		// reveals more than it should.
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	case err != nil:
		h.Fail(ctx, w, "activating conversation", err)
		return
	}

	conversation, err := h.repo.GetConversation(ctx, id)
	if err != nil {
		h.Fail(ctx, w, "reading conversation", err)
		return
	}

	h.Logger.InfoContext(ctx, "active conversation switched", slog.String("conversation_id", id))
	httpx.WriteJSON(ctx, w, http.StatusOK, views.OfConversation(*conversation, true))
}
