package chats_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
)

// Where a prompt lands, and what happens to what was already running there,
// is decided when a chat is created, so it is covered here rather than with
// the conversation endpoints.

// A chat created without naming a conversation lands in one, and the caller is
// told which, so a follow-up can continue it.
func TestCreatingAChatLandsInAConversation(t *testing.T) {
	e := apitest.New(t)

	created := e.CreateIn(t, "", "first question", "")

	if created.ConversationID == "" {
		t.Fatal("no conversation returned, so a follow-up has nothing to continue")
	}
	if !chat.ValidConversationID(created.ConversationID) {
		t.Errorf("conversation id = %q, want a valid identifier", created.ConversationID)
	}
}

func TestChatsInTheSameConversationShareIt(t *testing.T) {
	e := apitest.New(t)

	first := e.CreateIn(t, "", "first question", "?wait=5s")
	second := e.CreateIn(t, first.ConversationID, "second question", "?wait=5s")

	if second.ConversationID != first.ConversationID {
		t.Errorf("conversation = %q, want %q", second.ConversationID, first.ConversationID)
	}
}

// The whole point: a provider must be told what came before, or a correction
// reaches it with nothing to correct.
func TestHistoryReachesTheProvider(t *testing.T) {
	recorder := &apitest.RecordingProvider{}
	e := apitest.NewWith(t, apitest.Options{Environment: recorder})

	first := e.CreateIn(t, "", "List three programming languages.", "?wait=5s")
	e.CreateIn(t, first.ConversationID, "No, make it four.", "?wait=5s")

	seen := recorder.LastHistory()
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

// The prompt being answered must not also arrive as the last thing the user
// said. Sent twice, a model reads a question it has not been asked yet as
// something already discussed.
func TestThePromptIsNotAlsoInItsOwnHistory(t *testing.T) {
	recorder := &apitest.RecordingProvider{}
	e := apitest.NewWith(t, apitest.Options{Environment: recorder})

	first := e.CreateIn(t, "", "List three programming languages.", "?wait=5s")
	e.CreateIn(t, first.ConversationID, "No, make it four.", "?wait=5s")

	if got := recorder.LastPrompt(); got != "No, make it four." {
		t.Fatalf("prompt = %q", got)
	}
	for _, turn := range recorder.LastHistory() {
		if strings.Contains(turn.Text, "No, make it four.") {
			t.Errorf("the prompt is repeated in its own history as %q: %s",
				turn.Role, turn.Text)
		}
	}
}

// Both speakers reach the model. What they said and nothing else: no time,
// no identifier, no decoration a model could mistake for something to copy.
func TestHistoryCarriesBothSpeakers(t *testing.T) {
	recorder := &apitest.RecordingProvider{}
	e := apitest.NewWith(t, apitest.Options{Environment: recorder})

	first := e.CreateIn(t, "", "List three programming languages.", "?wait=5s")
	e.CreateIn(t, first.ConversationID, "No, make it four.", "?wait=5s")

	var roles []environment.Role
	for _, turn := range recorder.LastHistory() {
		roles = append(roles, turn.Role)
		if strings.HasPrefix(turn.Text, "[") {
			t.Errorf("turn %q was decorated before sending: %s", turn.Role, turn.Text)
		}
	}

	if len(roles) != 2 || roles[0] != environment.RoleUser || roles[1] != environment.RoleAssistant {
		t.Errorf("roles = %v, want the question then the answer", roles)
	}
}

// Naming a conversation sends one prompt there without moving the client, so a
// speaker can answer a question from another thread and stay where it was.
func TestNamingAConversationDoesNotMoveTheClient(t *testing.T) {
	e := apitest.New(t)
	here := e.CreateIn(t, "", "a question here", "?wait=5s")

	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"elsewhere","activate":false}`)
	var elsewhere views.Conversation
	e.Decode(t, rec, &elsewhere)

	sent := e.CreateIn(t, elsewhere.ID, "a question over there", "?wait=5s")
	if sent.ConversationID != elsewhere.ID {
		t.Fatalf("prompt landed in %s, want the named %s", sent.ConversationID, elsewhere.ID)
	}

	// The next unnamed prompt goes back to where the client actually is.
	next := e.CreateIn(t, "", "and another here", "?wait=5s")
	if next.ConversationID != here.ConversationID {
		t.Errorf("client moved to %s, want it still in %s", next.ConversationID, here.ConversationID)
	}
}

// Speaking again supersedes what is still running: two answers cannot be
// listened to at once, and a correction means the first is no longer wanted.
func TestANewPromptSupersedesARunningChat(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{
		Environment: &environment.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 50 * time.Millisecond},
	})

	first := e.Ask(t, "", "List three programming languages.")

	e.CreateIn(t, first.ConversationID, "No, make it four.", "")

	superseded := e.AwaitStatus(t, first.ID,
		string(chat.StatusCancelled), string(chat.StatusCompleted), string(chat.StatusFailed))
	if superseded.Status != string(chat.StatusCancelled) {
		t.Errorf("first chat = %q, want cancelled once superseded", superseded.Status)
	}
}

// A prompt in one conversation must not disturb a chat running in another.
func TestSupersedingIsScopedToOneConversation(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{
		Environment: &environment.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 50 * time.Millisecond},
	})

	// A chat running in the conversation that is active to begin with.
	elsewhere := e.Ask(t, "", "a question over here")

	// A second conversation, which becomes the active one.
	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"another"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create conversation: status %d: %s", rec.Code, rec.Body)
	}
	var second views.Conversation
	e.Decode(t, rec, &second)
	if second.ID == elsewhere.ConversationID {
		t.Fatal("the new conversation is the old one, so this proves nothing")
	}

	// A prompt now lands in the second conversation.
	landed := e.CreateIn(t, "", "a question over there", "")
	if landed.ConversationID != second.ID {
		t.Fatalf("prompt landed in %s, want the newly activated %s", landed.ConversationID, second.ID)
	}

	// The chat in the other conversation must be untouched.
	finished := e.AwaitStatus(t, elsewhere.ID,
		string(chat.StatusCompleted), string(chat.StatusCancelled), string(chat.StatusFailed))
	if finished.Status == string(chat.StatusCancelled) {
		t.Error("a chat was cancelled by a prompt in a different conversation")
	}
}

// A cancelled chat still contributes its prompt, which is what makes the
// correction work: the question it corrects was cancelled as it arrived.
func TestASupersededPromptStaysInHistory(t *testing.T) {
	recorder := &apitest.RecordingProvider{Delay: 60 * time.Millisecond}
	e := apitest.NewWith(t, apitest.Options{Environment: recorder})

	first := e.Ask(t, "", "List three programming languages.")

	second := e.CreateIn(t, first.ConversationID, "No, make it four.", "")
	e.AwaitStatus(t, second.ID,
		string(chat.StatusCompleted), string(chat.StatusFailed), string(chat.StatusCancelled))

	var joined strings.Builder
	for _, turn := range recorder.LastHistory() {
		joined.WriteString(turn.Text + "\n")
	}
	if !strings.Contains(joined.String(), "List three programming languages.") {
		t.Errorf("the superseded prompt is missing from history:\n%s", joined.String())
	}
}

func TestUnknownConversationIsRejected(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewConversationID(), "not-an-id"} {
		body, _ := json.Marshal(map[string]any{
			"model":           "assistant",
			"messages":        []map[string]string{{"role": "user", "content": "hello"}},
			"conversation_id": id,
		})
		rec := e.Do(t, http.MethodPost, "/api/chat", string(body))
		if rec.Code != http.StatusNotFound {
			t.Errorf("conversation %q: status = %d, want 404", id, rec.Code)
		}
	}
}

// A prompt must not be sent into another user's conversation.
func TestAnotherUsersConversationCannotBeUsed(t *testing.T) {
	e := apitest.New(t)

	stranger, _ := chat.NewUser("stranger", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirs := chat.NewConversation(stranger.ID, "private")
	_ = e.Repo.CreateConversation(t.Context(), theirs)

	body, _ := json.Marshal(map[string]any{
		"model":           "assistant",
		"messages":        []map[string]string{{"role": "user", "content": "hello"}},
		"conversation_id": theirs.ID,
	})
	rec := e.Do(t, http.MethodPost, "/api/chat", string(body))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
