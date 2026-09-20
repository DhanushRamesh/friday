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

// createIn : Submits a prompt, optionally continuing a conversation.
func createIn(t *testing.T, s *Server, conversationID, prompt, query string) taskView {
	t.Helper()
	body := map[string]string{"prompt": prompt}
	if conversationID != "" {
		body["conversation_id"] = conversationID
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

// A task created without naming a conversation starts one, and the caller is
// told which, so a follow-up can continue it.
func TestCreatingATaskStartsAConversation(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	created := createIn(t, s, "", "first question", "")

	if created.ConversationID == "" {
		t.Fatal("no conversation returned, so a follow-up has nothing to continue")
	}
	if !task.ValidConversationID(created.ConversationID) {
		t.Errorf("conversation id = %q, want a valid identifier", created.ConversationID)
	}
}

func TestTasksInTheSameConversationShareIt(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	first := createIn(t, s, "", "first question", "?wait=5s")
	second := createIn(t, s, first.ConversationID, "second question", "?wait=5s")

	if second.ConversationID != first.ConversationID {
		t.Errorf("conversation = %q, want %q", second.ConversationID, first.ConversationID)
	}
}

// The whole point: a provider must be told what came before, or a correction
// reaches it with nothing to correct.
func TestHistoryReachesTheProvider(t *testing.T) {
	recorder := &recordingProvider{}
	s, _, _, _ := newTaskServer(t, stubPinger{}, recorder)

	first := createIn(t, s, "", "List three programming languages.", "?wait=5s")
	createIn(t, s, first.ConversationID, "No, make it four.", "?wait=5s")

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

	createIn(t, s, first.ConversationID, "No, make it four.", "")

	superseded := awaitStatus(t, s, first.ID,
		string(task.StatusCancelled), string(task.StatusCompleted), string(task.StatusFailed))
	if superseded.Status != string(task.StatusCancelled) {
		t.Errorf("first task = %q, want cancelled once superseded", superseded.Status)
	}
}

// A prompt in one conversation must not disturb another.
func TestSupersedingIsScopedToOneConversation(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{},
		&provider.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 50 * time.Millisecond})

	other := createIn(t, s, "", "a separate question", "")
	awaitStatus(t, s, other.ID, string(task.StatusRunning))

	elsewhere := createIn(t, s, "", "an unrelated question", "")
	if elsewhere.ConversationID == other.ConversationID {
		t.Fatal("the two tasks share a conversation, so this proves nothing")
	}

	finished := awaitStatus(t, s, other.ID,
		string(task.StatusCompleted), string(task.StatusCancelled), string(task.StatusFailed))
	if finished.Status == string(task.StatusCancelled) {
		t.Error("a task was cancelled by a prompt in a different conversation")
	}
}

// A cancelled task still contributes its prompt, which is what makes the
// correction work: the question it corrects was cancelled as it arrived.
func TestASupersededPromptStaysInHistory(t *testing.T) {
	recorder := &recordingProvider{delay: 60 * time.Millisecond}
	s, _, _, _ := newTaskServer(t, stubPinger{}, recorder)

	first := createIn(t, s, "", "List three programming languages.", "")
	awaitStatus(t, s, first.ID, string(task.StatusRunning))

	second := createIn(t, s, first.ConversationID, "No, make it four.", "")
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

func TestUnknownConversationIsRejected(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, id := range []string{task.NewConversationID(), "not-an-id"} {
		body, _ := json.Marshal(map[string]string{"prompt": "hello", "conversation_id": id})
		rec := do(t, s, http.MethodPost, "/v1/tasks", string(body))
		if rec.Code != http.StatusNotFound {
			t.Errorf("conversation %q: status = %d, want 404", id, rec.Code)
		}
	}
}

func TestConversationCanBeReadBack(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	first := createIn(t, s, "", "first question", "?wait=5s")
	createIn(t, s, first.ConversationID, "second question", "?wait=5s")

	rec := do(t, s, http.MethodGet, "/v1/conversations/"+first.ConversationID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var detail conversationDetailResponse
	decodeInto(t, rec, &detail)

	if detail.Conversation.ID != first.ConversationID {
		t.Errorf("conversation id = %q", detail.Conversation.ID)
	}
	if len(detail.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2: %+v", len(detail.Tasks), detail.Tasks)
	}
	// Oldest first, so the exchange reads in the order it happened.
	if detail.Tasks[0].Prompt != "first question" {
		t.Errorf("first task = %q, want the earliest", detail.Tasks[0].Prompt)
	}
}

func TestConversationsCanBeListed(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	created := createIn(t, s, "", "a question", "?wait=5s")

	rec := do(t, s, http.MethodGet, "/v1/conversations", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var list listConversationsResponse
	decodeInto(t, rec, &list)

	var found bool
	for _, c := range list.Conversations {
		if c.ID == created.ConversationID {
			found = true
		}
	}
	if !found {
		t.Errorf("conversation %s missing from the listing", created.ConversationID)
	}
}

func TestUnknownConversationReadIsNotFound(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, id := range []string{task.NewConversationID(), "rubbish"} {
		rec := do(t, s, http.MethodGet, "/v1/conversations/"+id, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("conversation %q: status = %d, want 404", id, rec.Code)
		}
	}
}
