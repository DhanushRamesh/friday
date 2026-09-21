package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/friday/internal/task"
)

// handleCreateSession : Starts a new session for the caller.
//
// It belongs to the user, so every one of their clients can see and switch to
// it. This client moves into it unless the caller asks otherwise, since
// starting a session almost always means wanting to talk in it.
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := callerOf(ctx)

	var req createSessionRequest
	req.Activate = true
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(ctx, w, http.StatusBadRequest, err.Error())
			return
		}
	}

	session := task.NewSession(c.user.ID, req.Title)
	if err := s.tasks.CreateSession(ctx, session); err != nil {
		s.fail(ctx, w, "creating session", err)
		return
	}

	if req.Activate {
		if err := s.tasks.SetActiveSession(ctx, c.user.ID, c.client.ID, session.ID); err != nil {
			s.fail(ctx, w, "activating session", err)
			return
		}
	}

	s.logger.InfoContext(ctx, "session created",
		slog.String("session_id", session.ID),
		slog.Bool("active", req.Activate))
	writeJSON(ctx, w, http.StatusCreated, viewOfSession(*session, req.Activate))
}

// handleActivateSession : Switches where a prompt from this client
// lands.
//
// Only this client moves. Another client of the same user stays where it was,
// which is what lets a speaker and a laptop hold separate threads.
func (s *Server) handleActivateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := callerOf(ctx)
	id := chi.URLParam(r, "id")

	if !task.ValidSessionID(id) {
		writeError(ctx, w, http.StatusNotFound, "No such session.")
		return
	}

	err := s.tasks.SetActiveSession(ctx, c.user.ID, c.client.ID, id)
	switch {
	case errors.Is(err, task.ErrNotFound), errors.Is(err, task.ErrNotOwned):
		// Answered alike: telling one user that another's session
		// exists reveals more than it should.
		writeError(ctx, w, http.StatusNotFound, "No such session.")
		return
	case err != nil:
		s.fail(ctx, w, "activating session", err)
		return
	}

	session, err := s.tasks.GetSession(ctx, id)
	if err != nil {
		s.fail(ctx, w, "reading session", err)
		return
	}

	s.logger.InfoContext(ctx, "active session switched", slog.String("session_id", id))
	writeJSON(ctx, w, http.StatusOK, viewOfSession(*session, true))
}

// supersede : Stops whatever is still running in a session.
//
// Speaking again means the previous answer is no longer wanted, and two
// cannot be listened to at once. A failure here is logged rather than
// refused: the new prompt matters more than tidying the old one, and a task
// left running still reaches a terminal status on its own.
func (s *Server) supersede(ctx context.Context, sessionID string) {
	unfinished, err := s.tasks.Unfinished(ctx, sessionID)
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
