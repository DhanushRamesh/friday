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
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
)

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
	theirChat, _ := chat.New(theirSession.ID, chat.ChannelDirect, "their private question")
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

	e.CreateIn(t, "", "list me", "")

	rec := e.Get(t, "/v1/chats")
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
	theirChat, _ := chat.New(theirSession.ID, chat.ChannelDirect, "their private question")
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

// Cancelling is how "stop" reaches the server while it is still speaking.
func TestCancelStopsARunningChat(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{
		Provider: &provider.Stub{Updates: []string{"a", "b", "c", "d"}, Delay: 40 * time.Millisecond},
	})

	// /api/chat answers only when the chat is done, so the prompt goes on
	// another goroutine and is stopped while it is still running. That is
	// what a client does too: it holds the response open and aborts it.
	go e.Do(t, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"a long job"}]}`)

	created := e.AwaitAny(t, string(chat.StatusRunning), string(chat.StatusPending))

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

	created := e.CreateIn(t, "", "finishes quickly", "")

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
	r := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(huge))
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
	rec = e.Do(t, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"will fail to save"}]}`)
	if rec.Code == http.StatusInternalServerError {
		if strings.Contains(rec.Body.String(), apitest.ErrStorage.Error()) {
			t.Errorf("internal error text reached the caller: %s", rec.Body)
		}
		if !strings.Contains(e.Logs.String(), apitest.ErrStorage.Error()) {
			t.Error("internal error not logged, so the failure is undiagnosable")
		}
	}
}
