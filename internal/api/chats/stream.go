package chats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
)

// heartbeatInterval : How often a comment is sent on an idle stream.
//
// Proxies and mobile networks close a connection that has been silent, and a
// chat can think for a long time without saying anything.
const heartbeatInterval = 15 * time.Second

// Event : One event as it appears on the wire.
type Event struct {
	Kind string    `json:"kind"`
	Seq  int       `json:"seq,omitempty"`
	Text string    `json:"text,omitempty"`
	At   time.Time `json:"at"`
}

// Stream : Sends a chat's messages as they happen, as server-sent events, and
// closes the stream once the chat has finished.
//
// This is the path a voice client uses: every message it receives is spoken,
// so messages must arrive as they are produced rather than all at once at the
// end.
//
// A client joining late is not left behind. Messages already recorded are
// replayed first, then live ones follow, and a client reconnecting after a
// dropped connection resumes from the Last-Event-ID header it was given.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Logger.ErrorContext(ctx, "response writer cannot flush; streaming is impossible")
		httpx.WriteError(ctx, w, http.StatusInternalServerError, "Streaming is not available.")
		return
	}

	if !chat.ValidID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such chat.")
		return
	}

	// Subscribed before the chat is read, so that a chat finishing between
	// the two is still heard rather than falling into the gap.
	live, unsubscribe := h.events.Subscribe(id)
	defer unsubscribe()

	// Read through loadChat, as every other chat endpoint does, so that the
	// ownership check is one code path rather than one this could drift from.
	// Without it any authenticated caller could listen to another user's
	// conversation as it happened.
	t, _ := h.loadChat(ctx, w, id)
	if t == nil {
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

	// Everything the chat has already said.
	stored, err := h.repo.Messages(ctx, id)
	if err != nil {
		h.Logger.ErrorContext(ctx, "cannot replay chat messages", slog.Any("error", err))
	}
	for _, m := range stored {
		if m.Seq <= sent {
			continue
		}
		writeEvent(w, Event{Kind: m.Kind, Seq: m.Seq, Text: m.Text, At: m.CreatedAt})
		sent = m.Seq
	}
	flusher.Flush()

	// A chat that has already finished has nothing more to say.
	if t.Status.IsTerminal() {
		writeEvent(w, outcomeOf(t))
		flusher.Flush()
		return
	}

	h.Logger.InfoContext(ctx, "streaming chat", slog.String("chat_id", id))
	h.follow(ctx, w, flusher, live, sent)
}

// follow : Writes live events until the chat ends or the client leaves.
func (h *Handler) follow(
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
			// stopped listening. The chat itself keeps running; stopping it
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
			writeEvent(w, Event{
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

// outcomeOf : Renders how a finished chat ended.
func outcomeOf(t *chat.Chat) Event {
	ev := Event{At: t.UpdatedAt}
	switch t.Status {
	case chat.StatusCompleted:
		ev.Kind, ev.Text = string(events.KindFinal), t.Response
	case chat.StatusFailed:
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
func writeEvent(w http.ResponseWriter, ev Event) {
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
