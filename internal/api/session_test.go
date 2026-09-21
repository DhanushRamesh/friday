package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// createIn : Submits a prompt, optionally continuing a session.
func createIn(t *testing.T, s *Server, sessionID, prompt, query string) taskView {
	t.Helper()
	body := map[string]string{"prompt": prompt}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	encoded, _ := json.Marshal(body)

	rec := do(t, s, http.MethodPost, "/v1/tasks"+query, string(encoded))
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}
	var view taskView
	decodeInto(t, rec, &view)
	return view
}

// A task created without naming a session starts one, and the caller is
// told which, so a follow-up can continue it.
func TestCreatingATaskStartsASession(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	created := createIn(t, s, "", "first question", "")

	if created.SessionID == "" {
		t.Fatal("no session returned, so a follow-up has nothing to continue")
	}
	if !task.ValidSessionID(created.SessionID) {
		t.Errorf("session id = %q, want a valid identifier", created.SessionID)
	}
}

func TestTasksInTheSameSessionShareIt(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	first := createIn(t, s, "", "first question", "?wait=5s")
	second := createIn(t, s, first.SessionID, "second question", "?wait=5s")

	if second.SessionID != first.SessionID {
		t.Errorf("session = %q, want %q", second.SessionID, first.SessionID)
	}
}

// The whole point: a provider must be told what came before, or a correction
// reaches it with nothing to correct.
func TestHistoryReachesTheProvider(t *testing.T) {
	recorder := &recordingProvider{}
	s, _, _, _ := newTaskServer(t, stubPinger{}, recorder)

	first := createIn(t, s, "", "List three programming languages.", "?wait=5s")
	createIn(t, s, first.SessionID, "No, make it four.", "?wait=5s")

	seen := recorder.lastHistory()
	if len(seen) == 0 {
		t.Fatal("the provider was given no history for the second prompt")
	}

	var joined strings.Builder
	for _, turn := range seen {
		joined.WriteString(string(turn.Role) + ": " + turn.Text + "\n")
	}
	if !strings.Contains(joined.String(), "List three programming languages.") {
		t.Errorf("history does not carry the first prompt:\n%s", joined.String())
	}
}

// Speaking again supersedes what is still running: two answers cannot be
// listened to at once, and a correction means the first is no longer wanted.
func TestANewPromptSupersedesARunningTask(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{},
		&provider.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 50 * time.Millisecond})

	first := createIn(t, s, "", "List three programming languages.", "")
	awaitStatus(t, s, first.ID, string(task.StatusRunning))

	createIn(t, s, first.SessionID, "No, make it four.", "")

	superseded := awaitStatus(t, s, first.ID,
		string(task.StatusCancelled), string(task.StatusCompleted), string(task.StatusFailed))
	if superseded.Status != string(task.StatusCancelled) {
		t.Errorf("first task = %q, want cancelled once superseded", superseded.Status)
	}
}

// A prompt in one session must not disturb a task running in another.
func TestSupersedingIsScopedToOneSession(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{},
		&provider.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 50 * time.Millisecond})

	// A task running in the session that is active to begin with.
	elsewhere := createIn(t, s, "", "a question over here", "")
	awaitStatus(t, s, elsewhere.ID, string(task.StatusRunning))

	// A second session, which becomes the active one.
	rec := do(t, s, http.MethodPost, "/v1/sessions", `{"title":"another"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session: status %d: %s", rec.Code, rec.Body)
	}
	var second sessionView
	decodeInto(t, rec, &second)
	if second.ID == elsewhere.SessionID {
		t.Fatal("the new session is the old one, so this proves nothing")
	}

	// A prompt now lands in the second session.
	landed := createIn(t, s, "", "a question over there", "")
	if landed.SessionID != second.ID {
		t.Fatalf("prompt landed in %s, want the newly activated %s", landed.SessionID, second.ID)
	}

	// The task in the other session must be untouched.
	finished := awaitStatus(t, s, elsewhere.ID,
		string(task.StatusCompleted), string(task.StatusCancelled), string(task.StatusFailed))
	if finished.Status == string(task.StatusCancelled) {
		t.Error("a task was cancelled by a prompt in a different session")
	}
}

// A cancelled task still contributes its prompt, which is what makes the
// correction work: the question it corrects was cancelled as it arrived.
func TestASupersededPromptStaysInHistory(t *testing.T) {
	recorder := &recordingProvider{delay: 60 * time.Millisecond}
	s, _, _, _ := newTaskServer(t, stubPinger{}, recorder)

	first := createIn(t, s, "", "List three programming languages.", "")
	awaitStatus(t, s, first.ID, string(task.StatusRunning))

	second := createIn(t, s, first.SessionID, "No, make it four.", "")
	awaitStatus(t, s, second.ID,
		string(task.StatusCompleted), string(task.StatusFailed), string(task.StatusCancelled))

	var joined strings.Builder
	for _, turn := range recorder.lastHistory() {
		joined.WriteString(turn.Text + "\n")
	}
	if !strings.Contains(joined.String(), "List three programming languages.") {
		t.Errorf("the superseded prompt is missing from history:\n%s", joined.String())
	}
}

func TestUnknownSessionIsRejected(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, id := range []string{task.NewSessionID(), "not-an-id"} {
		body, _ := json.Marshal(map[string]string{"prompt": "hello", "session_id": id})
		rec := do(t, s, http.MethodPost, "/v1/tasks", string(body))
		if rec.Code != http.StatusNotFound {
			t.Errorf("session %q: status = %d, want 404", id, rec.Code)
		}
	}
}

func TestSessionCanBeReadBack(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	first := createIn(t, s, "", "first question", "?wait=5s")
	createIn(t, s, first.SessionID, "second question", "?wait=5s")

	rec := do(t, s, http.MethodGet, "/v1/sessions/"+first.SessionID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var detail sessionDetailResponse
	decodeInto(t, rec, &detail)

	if detail.Session.ID != first.SessionID {
		t.Errorf("session id = %q", detail.Session.ID)
	}
	if len(detail.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2: %+v", len(detail.Tasks), detail.Tasks)
	}
	// Oldest first, so the exchange reads in the order it happened.
	if detail.Tasks[0].Prompt != "first question" {
		t.Errorf("first task = %q, want the earliest", detail.Tasks[0].Prompt)
	}
}

func TestSessionsCanBeListed(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	created := createIn(t, s, "", "a question", "?wait=5s")

	rec := do(t, s, http.MethodGet, "/v1/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var list listSessionsResponse
	decodeInto(t, rec, &list)

	var found bool
	for _, c := range list.Sessions {
		if c.ID == created.SessionID {
			found = true
		}
	}
	if !found {
		t.Errorf("session %s missing from the listing", created.SessionID)
	}
}

func TestUnknownSessionReadIsNotFound(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, id := range []string{task.NewSessionID(), "rubbish"} {
		rec := do(t, s, http.MethodGet, "/v1/sessions/"+id, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("session %q: status = %d, want 404", id, rec.Code)
		}
	}
}
