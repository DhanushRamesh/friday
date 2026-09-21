package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/friday/internal/task"
)

// handleCreateConversation : Starts a new conversation for the caller.
//
// It belongs to the user, so every one of their devices can see and switch to
// it. This device moves into it unless the caller asks otherwise, since
// starting a conversation almost always means wanting to talk in it.
func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := callerOf(ctx)

	var req createConversationRequest
	req.Activate = true
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(ctx, w, http.StatusBadRequest, err.Error())
			return
		}
	}

	conversation := task.NewConversation(c.user.ID, req.Title)
	if err := s.tasks.CreateConversation(ctx, conversation); err != nil {
		s.fail(ctx, w, "creating conversation", err)
		return
	}

	if req.Activate {
		if err := s.tasks.SetActiveConversation(ctx, c.user.ID, c.device.ID, conversation.ID); err != nil {
			s.fail(ctx, w, "activating conversation", err)
			return
		}
	}

	s.logger.InfoContext(ctx, "conversation created",
		slog.String("conversation_id", conversation.ID),
		slog.Bool("active", req.Activate))
	writeJSON(ctx, w, http.StatusCreated, viewOfConversation(*conversation, req.Activate))
}

// handleActivateConversation : Switches where a prompt from this device
// lands.
//
// Only this device moves. Another device of the same user stays where it was,
// which is what lets a speaker and a laptop hold separate threads.
func (s *Server) handleActivateConversation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := callerOf(ctx)
	id := chi.URLParam(r, "id")

	if !task.ValidConversationID(id) {
		writeError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	err := s.tasks.SetActiveConversation(ctx, c.user.ID, c.device.ID, id)
	switch {
	case errors.Is(err, task.ErrNotFound), errors.Is(err, task.ErrNotOwned):
		// Answered alike: telling one user that another's conversation
		// exists reveals more than it should.
		writeError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	case err != nil:
		s.fail(ctx, w, "activating conversation", err)
		return
	}

	conversation, err := s.tasks.GetConversation(ctx, id)
	if err != nil {
		s.fail(ctx, w, "reading conversation", err)
		return
	}

	s.logger.InfoContext(ctx, "active conversation switched", slog.String("conversation_id", id))
	writeJSON(ctx, w, http.StatusOK, viewOfConversation(*conversation, true))
}

// supersede : Stops whatever is still running in a conversation.
//
// Speaking again means the previous answer is no longer wanted, and two
// cannot be listened to at once. A failure here is logged rather than
// refused: the new prompt matters more than tidying the old one, and a task
// left running still reaches a terminal status on its own.
func (s *Server) supersede(ctx context.Context, conversationID string) {
	unfinished, err := s.tasks.Unfinished(ctx, conversationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "cannot find tasks to supersede", slog.Any("error", err))
		return
	}

	for _, id := range unfinished {
		if s.runner.Cancel(id) {
			s.logger.InfoContext(ctx, "superseded by a new prompt", slog.String("task_id", id))
			continue
		}
		// Nothing is working on it, which happens to a task left behind by a
		// process that stopped. Stop it here instead.
		t, err := s.tasks.Get(ctx, id)
		if err != nil || t.Status.IsTerminal() {
			continue
		}
		if err := t.Cancel(); err == nil {
			_ = s.tasks.Update(ctx, t)
		}
	}
}
