package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// sseEvent : One parsed server-sent event.
type sseEvent struct {
	ID    string
	Event string
	Data  streamEvent
}

// readStream : Reads events from a live SSE response until it closes.
func readStream(t *testing.T, body *bufio.Reader) []sseEvent {
	t.Helper()

	var got []sseEvent
	var current sseEvent
	var haveData bool

	for {
		line, err := body.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if haveData {
					got = append(got, current)
				}
				current, haveData = sseEvent{}, false
			case strings.HasPrefix(line, "id: "):
				current.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				current.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				payload := strings.TrimPrefix(line, "data: ")
				if err := json.Unmarshal([]byte(payload), &current.Data); err != nil {
					t.Fatalf("unmarshal %q: %v", payload, err)
				}
				haveData = true
			case strings.HasPrefix(line, ":"):
				// A heartbeat comment.
			}
		}
		if err != nil {
			return got
		}
	}
}

// streamTask : Opens a stream against a live test server.
func streamTask(t *testing.T, s *Server, base, id string, headers map[string]string) []sseEvent {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/tasks/"+id+"/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenOf(s))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	return readStream(t, bufio.NewReader(resp.Body))
}

// liveServer : Runs the API on a real listener, which SSE needs in order to
// flush a response progressively.
func liveServer(t *testing.T, p provider.Provider) (*Server, string) {
	t.Helper()
	s, _, _, _ := newTaskServer(t, stubPinger{}, p)
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts.URL
}

// createTask : Submits a prompt and returns the created task.
func createTask(t *testing.T, s *Server, path, prompt string) taskView {
	t.Helper()
	rec := do(t, s, http.MethodPost, path, `{"prompt":`+quote(prompt)+`}`)
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}
	var view taskView
	decodeInto(t, rec, &view)
	return view
}

// quote : Renders a string as a JSON string.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// A client listening while a task runs hears each message as it is produced,
// then the answer, and the stream ends.
func TestStreamDeliversMessagesThenTheAnswer(t *testing.T) {
	updates := []string{"Let me take a look.", "Still working on it."}
	s, base := liveServer(t, &provider.Stub{Updates: updates, Delay: 40 * time.Millisecond})

	created := createTask(t, s, "/v1/tasks", "check my merge requests")
	got := streamTask(t, s, base, created.ID, nil)

	if len(got) < len(updates)+1 {
		t.Fatalf("got %d events, want %d updates and a final: %+v", len(got), len(updates), got)
	}

	for i, want := range updates {
		if got[i].Event != "update" {
			t.Errorf("event %d kind = %q, want update", i, got[i].Event)
		}
		if got[i].Data.Text != want {
			t.Errorf("event %d text = %q, want %q", i, got[i].Data.Text, want)
		}
		if got[i].ID == "" {
			t.Errorf("event %d has no id, so a client cannot resume from it", i)
		}
	}

	last := got[len(got)-1]
	if last.Event != "final" {
		t.Fatalf("last event = %q, want final", last.Event)
	}
	if !strings.Contains(last.Data.Text, "check my merge requests") {
		t.Errorf("final text = %q, want the answer", last.Data.Text)
	}
}

// A task that finished before anyone connected must still deliver everything
// it said and how it ended, rather than an empty stream.
func TestStreamReplaysAFinishedTask(t *testing.T) {
	updates := []string{"one", "two"}
	s, base := liveServer(t, &provider.Stub{Updates: updates})

	created := createTask(t, s, "/v1/tasks?wait=5s", "already done")
	if created.Status != string(task.StatusCompleted) {
		t.Fatalf("status = %q, want the task finished before streaming", created.Status)
	}

	got := streamTask(t, s, base, created.ID, nil)

	if len(got) != len(updates)+1 {
		t.Fatalf("got %d events, want %d replayed updates and a final: %+v", len(got), len(updates), got)
	}
	if got[len(got)-1].Event != "final" {
		t.Errorf("last event = %q, want final", got[len(got)-1].Event)
	}
}

// A reconnecting client resumes rather than hearing everything twice, which
// for a voice client would mean repeating itself.
func TestStreamResumesFromLastEventID(t *testing.T) {
	updates := []string{"one", "two", "three"}
	s, base := liveServer(t, &provider.Stub{Updates: updates})

	created := createTask(t, s, "/v1/tasks?wait=5s", "resume me")

	got := streamTask(t, s, base, created.ID, map[string]string{"Last-Event-ID": "2"})

	for _, ev := range got {
		if ev.Data.Seq > 0 && ev.Data.Seq <= 2 {
			t.Errorf("event %d was replayed despite Last-Event-ID: 2", ev.Data.Seq)
		}
	}
	if len(got) == 0 || got[len(got)-1].Event != "final" {
		t.Errorf("the outcome was not delivered: %+v", got)
	}
}

// A failed task ends the stream with the reason, which is what the user hears.
func TestStreamEndsWithTheFailure(t *testing.T) {
	const reason = "I could not reach GitLab."
	s, base := liveServer(t, &provider.Stub{Updates: []string{"trying"}, FailWith: reason})

	created := createTask(t, s, "/v1/tasks", "will fail")
	got := streamTask(t, s, base, created.ID, nil)

	last := got[len(got)-1]
	if last.Event != "error" {
		t.Fatalf("last event = %q, want error: %+v", last.Event, got)
	}
	if last.Data.Text != reason {
		t.Errorf("error text = %q, want %q", last.Data.Text, reason)
	}
}

// Cancelling ends the stream, which is what saying "stop" must do.
func TestStreamEndsWhenTheTaskIsCancelled(t *testing.T) {
	s, base := liveServer(t, &provider.Stub{
		Updates: []string{"a", "b", "c", "d", "e"},
		Delay:   60 * time.Millisecond,
	})

	created := createTask(t, s, "/v1/tasks", "stop me midway")

	done := make(chan []sseEvent, 1)
	go func() { done <- streamTask(t, s, base, created.ID, nil) }()

	time.Sleep(120 * time.Millisecond)
	rec := do(t, s, http.MethodPost, "/v1/tasks/"+created.ID+"/cancel", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel: status %d: %s", rec.Code, rec.Body)
	}

	select {
	case got := <-done:
		if len(got) == 0 {
			t.Fatal("stream produced nothing")
		}
		if last := got[len(got)-1].Event; last != "cancelled" {
			t.Errorf("last event = %q, want cancelled: %+v", last, got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stream did not close after the task was cancelled")
	}
}

func TestStreamOfAnUnknownTaskIsNotFound(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, id := range []string{task.NewID(), "nonsense"} {
		rec := do(t, s, http.MethodGet, "/v1/tasks/"+id+"/stream", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("stream %s: status = %d, want 404", id, rec.Code)
		}
	}
}

// Two listeners on one task both hear everything, as a phone and a desktop
// might.
func TestTwoListenersBothHearEverything(t *testing.T) {
	s, base := liveServer(t, &provider.Stub{Updates: []string{"one", "two"}, Delay: 50 * time.Millisecond})

	created := createTask(t, s, "/v1/tasks", "heard twice")

	first := make(chan []sseEvent, 1)
	second := make(chan []sseEvent, 1)
	go func() { first <- streamTask(t, s, base, created.ID, nil) }()
	go func() { second <- streamTask(t, s, base, created.ID, nil) }()

	for i, ch := range []chan []sseEvent{first, second} {
		select {
		case got := <-ch:
			if len(got) == 0 || got[len(got)-1].Event != "final" {
				t.Errorf("listener %d did not receive the answer: %+v", i, got)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("listener %d never finished", i)
		}
	}
}
