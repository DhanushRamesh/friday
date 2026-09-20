package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/friday/internal/events"
	"github.com/DhanushRamesh/friday/internal/task"
)

// heartbeatInterval : How often a comment is sent on an idle stream.
//
// Proxies and mobile networks close a connection that has been silent, and a
// task can think for a long time without saying anything.
const heartbeatInterval = 15 * time.Second

// streamEvent : One event as it appears on the wire.
type streamEvent struct {
	Kind string    `json:"kind"`
	Seq  int       `json:"seq,omitempty"`
	Text string    `json:"text,omitempty"`
	At   time.Time `json:"at"`
}

// handleStreamTask : Sends a task's messages as they happen, as server-sent
// events, and closes the stream once the task has finished.
//
// This is the path a voice client uses: every message it receives is spoken,
// so messages must arrive as they are produced rather than all at once at the
// end.
//
// A client joining late is not left behind. Messages already recorded are
// replayed first, then live ones follow, and a client reconnecting after a
// dropped connection resumes from the Last-Event-ID header it was given.
func (s *Server) handleStreamTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.ErrorContext(ctx, "response writer cannot flush; streaming is impossible")
		writeError(ctx, w, http.StatusInternalServerError, "Streaming is not available.")
		return
	}

	if !task.ValidID(id) {
		writeError(ctx, w, http.StatusNotFound, "No such task.")
		return
	}

	// Subscribed before the task is read, so that a task finishing between
	// the two is still heard rather than falling into the gap.
	live, unsubscribe := s.events.Subscribe(id)
	defer unsubscribe()

	t, err := s.tasks.Get(ctx, id)
	if err != nil {
		if err == task.ErrNotFound {
			writeError(ctx, w, http.StatusNotFound, "No such task.")
			return
		}
		s.fail(ctx, w, "reading task", err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Tell nginx and similar not to buffer, which would defeat the point.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sent := lastEventID(r)

	// Everything the task has already said.
	stored, err := s.tasks.Messages(ctx, id)
	if err != nil {
		s.logger.ErrorContext(ctx, "cannot replay task messages", slog.Any("error", err))
	}
	for _, m := range stored {
		if m.Seq <= sent {
			continue
		}
		writeEvent(w, streamEvent{Kind: m.Kind, Seq: m.Seq, Text: m.Text, At: m.CreatedAt})
		sent = m.Seq
	}
	flusher.Flush()

	// A task that has already finished has nothing more to say.
	if t.Status.IsTerminal() {
		writeEvent(w, outcomeOf(t))
		flusher.Flush()
		return
	}

	s.logger.InfoContext(ctx, "streaming task", slog.String("task_id", id))
	s.followTask(ctx, w, flusher, live, sent)
}

// followTask : Writes live events until the task ends or the client leaves.
func (s *Server) followTask(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	live <-chan events.Event,
	sent int,
) {
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			// The client hung up, which for a voice client means the user
			// stopped listening. The task itself keeps running; stopping it
			// is a separate request.
			return

		case <-heartbeat.C:
			// A comment, which clients ignore and proxies count as traffic.
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()

		case ev, ok := <-live:
			if !ok {
				return
			}
			// A message already replayed must not be sent twice.
			if ev.Seq > 0 && ev.Seq <= sent {
				continue
			}
			if ev.Seq > sent {
				sent = ev.Seq
			}
			writeEvent(w, streamEvent{
				Kind: string(ev.Kind),
				Seq:  ev.Seq,
				Text: ev.Text,
				At:   ev.At,
			})
			flusher.Flush()

			if ev.Kind.Terminal() {
				return
			}
		}
	}
}

// outcomeOf : Renders how a finished task ended.
func outcomeOf(t *task.Task) streamEvent {
	ev := streamEvent{At: t.UpdatedAt}
	switch t.Status {
	case task.StatusCompleted:
		ev.Kind, ev.Text = string(events.KindFinal), t.Response
	case task.StatusFailed:
		ev.Kind, ev.Text = string(events.KindError), t.Error
	default:
		ev.Kind = string(events.KindCancelled)
	}
	return ev
}

// writeEvent : Writes one server-sent event.
//
// The identifier lets a client resume from where it left off after a dropped
// connection, which a phone on mobile data does often.
func writeEvent(w http.ResponseWriter, ev streamEvent) {
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	if ev.Seq > 0 {
		fmt.Fprintf(w, "id: %d\n", ev.Seq)
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, body)
}

// lastEventID : Returns the position a reconnecting client already reached.
func lastEventID(r *http.Request) int {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("last_event_id")
	}
	seq, err := strconv.Atoi(raw)
	if err != nil || seq < 0 {
		return 0
	}
	return seq
}
