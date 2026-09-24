package chats_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/chats"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
)

func TestCreateChatAcceptsAndRuns(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{
		Provider: &provider.Stub{Updates: []string{"one", "two"}},
	})

	rec := e.Do(t, http.MethodPost, "/v1/chats", `{"prompt":"check my merge requests"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}

	var created views.Chat
	e.Decode(t, rec, &created)
	if !chat.ValidID(created.ID) {
		t.Errorf("id = %q, want a chat identifier", created.ID)
	}
	if created.Status != string(chat.StatusPending) && created.Status != string(chat.StatusRunning) {
		t.Errorf("status = %q, want pending or running", created.Status)
	}
	if created.Prompt != "check my merge requests" {
		t.Errorf("prompt = %q, want it echoed back", created.Prompt)
	}

	done := e.AwaitStatus(t, created.ID, string(chat.StatusCompleted), string(chat.StatusFailed))
	if done.Status != string(chat.StatusCompleted) {
		t.Fatalf("status = %q (%s), want completed", done.Status, done.Error)
	}
	if !strings.Contains(done.Response, "check my merge requests") {
		t.Errorf("response = %q, want it to reflect the prompt", done.Response)
	}
}

// wait holds the connection until the chat finishes, so a caller using the
// API by hand gets an answer without writing a poll loop.
func TestCreateChatWithWaitReturnsTheFinishedChat(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Provider: &provider.Stub{Updates: []string{"one"}}})

	rec := e.Do(t, http.MethodPost, "/v1/chats?wait=5s", `{"prompt":"answer me now"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var view views.Chat
	e.Decode(t, rec, &view)
	if view.Status != string(chat.StatusCompleted) {
		t.Errorf("status = %q, want completed", view.Status)
	}
	if view.Response == "" {
		t.Error("no response returned despite waiting")
	}
	if view.FinishedAt == nil {
		t.Error("no finish time on a completed chat")
	}
}

// A wait too short to cover the chat returns the unfinished chat rather than
// failing, so the caller can fetch it later.
func TestCreateChatWithShortWaitReturnsPending(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{
		Provider: &provider.Stub{Updates: []string{"a", "b"}, Delay: 200 * time.Millisecond},
	})

	rec := e.Do(t, http.MethodPost, "/v1/chats?wait=150ms", `{"prompt":"slow job"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 for an unfinished chat: %s", rec.Code, rec.Body)
	}

	var view views.Chat
	e.Decode(t, rec, &view)
	if view.Status == string(chat.StatusCompleted) {
		t.Error("chat completed within a wait shorter than its runtime")
	}
}

func TestCreateChatRejectsBadRequests(t *testing.T) {
	e := apitest.New(t)

	cases := map[string]struct {
		path, body string
	}{
		"empty prompt":    {"/v1/chats", `{"prompt":""}`},
		"whitespace only": {"/v1/chats", `{"prompt":"   "}`},
		"missing prompt":  {"/v1/chats", `{}`},
		"not json":        {"/v1/chats", `not json at all`},
		"unknown field":   {"/v1/chats", `{"prompt":"hi","model":"gpt"}`},
		"two objects":     {"/v1/chats", `{"prompt":"hi"}{"prompt":"again"}`},
		"bad wait":        {"/v1/chats?wait=soon", `{"prompt":"hi"}`},
		"negative wait":   {"/v1/chats?wait=-5s", `{"prompt":"hi"}`},
		"prompt too long": {"/v1/chats", `{"prompt":"` + strings.Repeat("a", chat.MaxPromptRunes+1) + `"}`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := e.Do(t, http.MethodPost, tc.path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			var body httpx.ErrorResponse
			e.Decode(t, rec, &body)
			if body.Error == "" {
				t.Error("no explanation returned")
			}
		})
	}
}

// An empty body is a common mistake and must say so rather than crash.
func TestCreateChatRejectsEmptyBody(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/v1/chats", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestGetUnknownChatIsNotFound(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewID(), "not-an-id", "chat_garbage"} {
		if rec := e.Get(t, "/v1/chats/"+id); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", id, rec.Code)
		}
	}
}

// One user must never read another's chat, and must not learn that it exists
// either, so it is answered as missing rather than forbidden.
func TestAnotherUsersChatIsHidden(t *testing.T) {
	e := apitest.New(t)

	stranger, _ := chat.NewUser("stranger", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirSession := chat.NewSession(stranger.ID, "private")
	_ = e.Repo.CreateSession(t.Context(), theirSession)
	theirChat, _ := chat.New(theirSession.ID, "their private question")
	_ = e.Repo.Create(t.Context(), theirChat)

	for _, path := range []string{
		"/v1/chats/" + theirChat.ID,
		"/v1/chats/" + theirChat.ID + "/messages",
		"/v1/chats/" + theirChat.ID + "/stream",
	} {
		if rec := e.Get(t, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
	rec := e.Do(t, http.MethodPost, "/v1/chats/"+theirChat.ID+"/cancel", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cancel: status = %d, want 404", rec.Code)
	}
}

func TestListReturnsChatsWithoutResponses(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/v1/chats?wait=5s", `{"prompt":"list me"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}

	rec = e.Get(t, "/v1/chats")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	// The listing must not carry response bodies at all.
	if strings.Contains(rec.Body.String(), `"response"`) {
		t.Errorf("listing carries response bodies: %s", rec.Body)
	}

	var list chats.ListResponse
	e.Decode(t, rec, &list)
	if len(list.Chats) == 0 {
		t.Fatal("listing is empty")
	}
}

// A listing must show only the caller's own chats.
func TestListIsScopedToTheUser(t *testing.T) {
	e := apitest.New(t)

	stranger, _ := chat.NewUser("stranger", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirSession := chat.NewSession(stranger.ID, "private")
	_ = e.Repo.CreateSession(t.Context(), theirSession)
	theirChat, _ := chat.New(theirSession.ID, "their private question")
	_ = e.Repo.Create(t.Context(), theirChat)

	var list chats.ListResponse
	e.Decode(t, e.Get(t, "/v1/chats"), &list)
	for _, s := range list.Chats {
		if s.ID == theirChat.ID {
			t.Error("another user's chat appears in the listing")
		}
	}
}

func TestListRejectsBadParameters(t *testing.T) {
	e := apitest.New(t)

	for _, path := range []string{"/v1/chats?status=banana", "/v1/chats?limit=0", "/v1/chats?limit=lots"} {
		if rec := e.Get(t, path); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestMessagesAreReadableAfterARun(t *testing.T) {
	updates := []string{"Let me take a look.", "Still working on it."}
	e := apitest.NewWith(t, apitest.Options{Provider: &provider.Stub{Updates: updates}})

	created := e.CreateChat(t, "/v1/chats?wait=5s", "talk to me")

	rec := e.Get(t, "/v1/chats/"+created.ID+"/messages")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body chats.MessagesResponse
	e.Decode(t, rec, &body)
	if len(body.Messages) != len(updates) {
		t.Fatalf("got %d messages, want %d: %+v", len(body.Messages), len(updates), body.Messages)
	}
	for i, want := range updates {
		if body.Messages[i].Text != want {
			t.Errorf("message %d = %q, want %q", i, body.Messages[i].Text, want)
		}
		if body.Messages[i].Seq != i+1 {
			t.Errorf("message %d has seq %d, want %d", i, body.Messages[i].Seq, i+1)
		}
	}
}

// Cancelling is how "stop" reaches FRIDAY while it is still speaking.
func TestCancelStopsARunningChat(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{
		Provider: &provider.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 40 * time.Millisecond},
	})

	created := e.CreateChat(t, "/v1/chats", "a long job")

	rec := e.Do(t, http.MethodPost, "/v1/chats/"+created.ID+"/cancel", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel: status = %d, want 202: %s", rec.Code, rec.Body)
	}

	done := e.AwaitStatus(t, created.ID,
		string(chat.StatusCancelled), string(chat.StatusCompleted), string(chat.StatusFailed))
	if done.Status != string(chat.StatusCancelled) {
		t.Errorf("status = %q, want cancelled", done.Status)
	}
}

func TestCancelAFinishedChatConflicts(t *testing.T) {
	e := apitest.New(t)

	created := e.CreateChat(t, "/v1/chats?wait=5s", "finishes quickly")

	rec := e.Do(t, http.MethodPost, "/v1/chats/"+created.ID+"/cancel", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for a finished chat: %s", rec.Code, rec.Body)
	}
}

func TestCancelUnknownChatIsNotFound(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/v1/chats/"+chat.NewID()+"/cancel", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// A body beyond the limit must be refused rather than read into memory.
func TestOversizedBodyRejected(t *testing.T) {
	e := apitest.New(t)

	huge := bytes.Repeat([]byte("a"), httpx.MaxRequestBody+1024)
	r := httptest.NewRequest(http.MethodPost, "/v1/chats", bytes.NewReader(huge))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+e.Token)

	if rec := e.Serve(r); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// Internal failures must not reach the caller, whose message may be spoken.
func TestInternalFailureIsNotRevealed(t *testing.T) {
	e := apitest.New(t)
	e.Repo.FailUpdates(apitest.ErrStorage)

	rec := e.Get(t, "/v1/chats")
	if rec.Code != http.StatusOK {
		t.Fatalf("list should still work: %d", rec.Code)
	}

	// Force a failure the handler cannot recover from.
	rec = e.Do(t, http.MethodPost, "/v1/chats", `{"prompt":"will fail to save"}`)
	if rec.Code == http.StatusInternalServerError {
		if strings.Contains(rec.Body.String(), apitest.ErrStorage.Error()) {
			t.Errorf("internal error text reached the caller: %s", rec.Body)
		}
		if !strings.Contains(e.Logs.String(), apitest.ErrStorage.Error()) {
			t.Error("internal error not logged, so the failure is undiagnosable")
		}
	}
}
