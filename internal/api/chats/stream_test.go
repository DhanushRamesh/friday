package chats_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/chats"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
)

// sseEvent : One parsed server-sent event.
type sseEvent struct {
	ID    string
	Event string
	Data  chats.Event
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

// streamChat : Opens a stream against a live test server and reads it to the
// end.
func streamChat(t *testing.T, e *apitest.Env, base, id string, headers map[string]string) []sseEvent {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/chats/"+id+"/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
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

// live : An environment served on a real listener, which SSE needs in order
// to flush a response progressively.
func live(t *testing.T, p provider.Provider) (*apitest.Env, string) {
	t.Helper()
	e := apitest.NewWith(t, apitest.Options{Provider: p})
	return e, e.Live(t)
}

// A client listening while a chat runs hears each message as it is produced,
// then the answer, and the stream ends.
func TestStreamDeliversMessagesThenTheAnswer(t *testing.T) {
	updates := []string{"Let me take a look.", "Still working on it."}
	e, base := live(t, &provider.Stub{Updates: updates, Delay: 40 * time.Millisecond})

	created := e.CreateChat(t, "/v1/chats", "check my merge requests")
	got := streamChat(t, e, base, created.ID, nil)

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

// A chat that finished before anyone connected must still deliver everything
// it said and how it ended, rather than an empty stream.
func TestStreamReplaysAFinishedChat(t *testing.T) {
	updates := []string{"one", "two"}
	e, base := live(t, &provider.Stub{Updates: updates})

	created := e.CreateChat(t, "/v1/chats?wait=5s", "already done")
	if created.Status != string(chat.StatusCompleted) {
		t.Fatalf("status = %q, want the chat finished before streaming", created.Status)
	}

	got := streamChat(t, e, base, created.ID, nil)

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
	e, base := live(t, &provider.Stub{Updates: updates})

	created := e.CreateChat(t, "/v1/chats?wait=5s", "resume me")

	got := streamChat(t, e, base, created.ID, map[string]string{"Last-Event-ID": "2"})

	for _, ev := range got {
		if ev.Data.Seq > 0 && ev.Data.Seq <= 2 {
			t.Errorf("event %d was replayed despite Last-Event-ID: 2", ev.Data.Seq)
		}
	}
	if len(got) == 0 || got[len(got)-1].Event != "final" {
		t.Errorf("the outcome was not delivered: %+v", got)
	}
}

// A failed chat ends the stream with the reason, which is what the user hears.
func TestStreamEndsWithTheFailure(t *testing.T) {
	const reason = "I could not reach GitLab."
	e, base := live(t, &provider.Stub{Updates: []string{"trying"}, FailWith: reason})

	created := e.CreateChat(t, "/v1/chats", "will fail")
	got := streamChat(t, e, base, created.ID, nil)

	last := got[len(got)-1]
	if last.Event != "error" {
		t.Fatalf("last event = %q, want error: %+v", last.Event, got)
	}
	if last.Data.Text != reason {
		t.Errorf("error text = %q, want %q", last.Data.Text, reason)
	}
}

// Cancelling ends the stream, which is what saying "stop" must do.
func TestStreamEndsWhenTheChatIsCancelled(t *testing.T) {
	e, base := live(t, &provider.Stub{
		Updates: []string{"a", "b", "c", "d", "e"},
		Delay:   60 * time.Millisecond,
	})

	created := e.CreateChat(t, "/v1/chats", "stop me midway")

	done := make(chan []sseEvent, 1)
	go func() { done <- streamChat(t, e, base, created.ID, nil) }()

	time.Sleep(120 * time.Millisecond)
	rec := e.Do(t, http.MethodPost, "/v1/chats/"+created.ID+"/cancel", "")
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
		t.Fatal("stream did not close after the chat was cancelled")
	}
}

func TestStreamOfAnUnknownChatIsNotFound(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewID(), "nonsense"} {
		rec := e.Get(t, "/v1/chats/"+id+"/stream")
		if rec.Code != http.StatusNotFound {
			t.Errorf("stream %s: status = %d, want 404", id, rec.Code)
		}
	}
}

// Two listeners on one chat both hear everything, as a phone and a desktop
// might.
func TestTwoListenersBothHearEverything(t *testing.T) {
	e, base := live(t, &provider.Stub{Updates: []string{"one", "two"}, Delay: 50 * time.Millisecond})

	created := e.CreateChat(t, "/v1/chats", "heard twice")

	first := make(chan []sseEvent, 1)
	second := make(chan []sseEvent, 1)
	go func() { first <- streamChat(t, e, base, created.ID, nil) }()
	go func() { second <- streamChat(t, e, base, created.ID, nil) }()

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
