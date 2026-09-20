package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/friday/internal/task"
)

const (
	// maxRequestBody : The largest request body accepted. A prompt is text
	// typed or spoken by one person, so anything beyond this is a mistake or
	// an attack.
	maxRequestBody = 1 << 20

	// maxWait : The longest a create request will hold its connection open
	// when asked to wait for a result.
	maxWait = 60 * time.Second

	// waitPollInterval : How often a waiting request checks whether the task
	// has finished.
	//
	// Polling is adequate for what wait is: a convenience for using the API
	// by hand. A client that needs messages as they happen uses the stream
	// instead, and is not served by this at all.
	waitPollInterval = 100 * time.Millisecond
)

// handleCreateTask : Accepts a prompt, stores it as a task and starts it
// running.
//
// It answers 202 at once. Given a wait parameter it holds the connection until
// the task finishes or the wait elapses, answering 200 with the finished task
// if it lands in time.
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req createTaskRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	wait, err := parseWait(r.URL.Query().Get("wait"))
	if err != nil {
		writeError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := task.New(req.Prompt)
	switch {
	case errors.Is(err, task.ErrEmptyPrompt):
		writeError(ctx, w, http.StatusBadRequest, "A prompt is required.")
		return
	case errors.Is(err, task.ErrPromptTooLong):
		writeError(ctx, w, http.StatusBadRequest, "That prompt is too long.")
		return
	case err != nil:
		s.fail(ctx, w, "creating task", err)
		return
	}

	if err := s.tasks.Create(ctx, t); err != nil {
		s.fail(ctx, w, "storing task", err)
		return
	}
	if err := s.runner.Submit(t); err != nil {
		// The task is stored but will never run, so say so rather than
		// leaving it pending for ever.
		s.logger.ErrorContext(ctx, "cannot submit task", slog.Any("error", err))
		if failErr := t.Fail("FRIDAY could not start this."); failErr == nil {
			_ = s.tasks.Update(ctx, t)
		}
		writeError(ctx, w, http.StatusServiceUnavailable, "FRIDAY is not accepting work at the moment.")
		return
	}

	s.logger.InfoContext(ctx, "task accepted", slog.String("task_id", t.ID))

	if wait > 0 {
		if finished := s.awaitTask(ctx, t.ID, wait); finished != nil {
			writeJSON(ctx, w, http.StatusOK, viewOf(finished))
			return
		}
	}
	writeJSON(ctx, w, http.StatusAccepted, viewOf(t))
}

// handleGetTask : Returns one task, including its response once it has one.
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	t, err := s.loadTask(ctx, w, id)
	if t == nil {
		_ = err
		return
	}
	writeJSON(ctx, w, http.StatusOK, viewOf(t))
}

// handleListTasks : Returns recent tasks, newest first, without their
// responses.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := task.Filter{}
	if status := r.URL.Query().Get("status"); status != "" {
		if !task.Status(status).Valid() {
			writeError(ctx, w, http.StatusBadRequest, "Unknown status: "+status)
			return
		}
		filter.Status = task.Status(status)
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			writeError(ctx, w, http.StatusBadRequest, "The limit must be a positive whole number.")
			return
		}
		filter.Limit = limit
	}

	summaries, err := s.tasks.List(ctx, filter)
	if err != nil {
		s.fail(ctx, w, "listing tasks", err)
		return
	}

	views := make([]summaryView, len(summaries))
	for i, sum := range summaries {
		views[i] = viewOfSummary(sum)
	}
	writeJSON(ctx, w, http.StatusOK, listTasksResponse{Tasks: views})
}

// handleGetTaskMessages : Returns everything a task said while it ran.
func (s *Server) handleGetTaskMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if t, _ := s.loadTask(ctx, w, id); t == nil {
		return
	}

	messages, err := s.tasks.Messages(ctx, id)
	if err != nil {
		s.fail(ctx, w, "reading task messages", err)
		return
	}
	writeJSON(ctx, w, http.StatusOK, map[string]any{"messages": viewOfMessages(messages)})
}

// handleCancelTask : Stops a task that has not finished.
func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	t, _ := s.loadTask(ctx, w, id)
	if t == nil {
		return
	}
	if t.Status.IsTerminal() {
		writeError(ctx, w, http.StatusConflict, "That task has already finished.")
		return
	}

	// The runner holds every task it has been given, queued or running, so
	// this is the usual path.
	if s.runner.Cancel(id) {
		s.logger.InfoContext(ctx, "task cancellation requested", slog.String("task_id", id))
		writeJSON(ctx, w, http.StatusAccepted, viewOf(t))
		return
	}

	// Nothing is working on it, which happens to a task left pending by a
	// process that stopped. Stop it here instead.
	if err := t.Cancel(); err != nil {
		s.fail(ctx, w, "cancelling task", err)
		return
	}
	if err := s.tasks.Update(ctx, t); err != nil {
		s.fail(ctx, w, "cancelling task", err)
		return
	}
	writeJSON(ctx, w, http.StatusAccepted, viewOf(t))
}

// loadTask : Reads a task, writing the response itself when it cannot. It
// returns nil when the caller should stop.
func (s *Server) loadTask(ctx context.Context, w http.ResponseWriter, id string) (*task.Task, error) {
	if !task.ValidID(id) {
		writeError(ctx, w, http.StatusNotFound, "No such task.")
		return nil, nil
	}

	t, err := s.tasks.Get(ctx, id)
	if errors.Is(err, task.ErrNotFound) {
		writeError(ctx, w, http.StatusNotFound, "No such task.")
		return nil, err
	}
	if err != nil {
		s.fail(ctx, w, "reading task", err)
		return nil, err
	}
	return t, nil
}

// awaitTask : Waits for a task to finish, returning it if it does within the
// given time and nil otherwise.
func (s *Server) awaitTask(ctx context.Context, id string, wait time.Duration) *task.Task {
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
			t, err := s.tasks.Get(ctx, id)
			if err != nil {
				// Report the task as unfinished rather than failing the
				// request; it is still running and can be fetched later.
				s.logger.ErrorContext(ctx, "cannot poll task while waiting",
					slog.String("task_id", id), slog.Any("error", err))
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

// decodeJSON : Reads a JSON body, rejecting one that is malformed, oversized
// or carries fields the request does not define.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(into); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return errors.New("A request body is required.")
		case errors.As(err, &maxErr):
			return errors.New("That request is too large.")
		default:
			return errors.New("The request body is not valid JSON.")
		}
	}
	// A second value means the body held more than one object.
	if dec.More() {
		return errors.New("The request body must hold a single object.")
	}
	return nil
}
