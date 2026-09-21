package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// Registering gives a client somewhere to talk at once, so a new client can
// ask something without first creating a conversation.
func TestRegisteringAClientGivesItAnActiveConversation(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodPost, "/v1/clients", `{"name":"my phone"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var client clientView
	decodeInto(t, rec, &client)

	if !task.ValidClientID(client.ID) {
		t.Errorf("id = %q, want a client identifier", client.ID)
	}
	if client.Name != "my phone" {
		t.Errorf("name = %q, want it echoed back", client.Name)
	}
	if !task.ValidConversationID(client.ActiveConversationID) {
		t.Errorf("active conversation = %q, want one ready to talk in", client.ActiveConversationID)
	}
}

// Every endpoint but registering needs a client, or one caller would see
// another's conversations.
func TestEndpointsRequireAClient(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	paths := []struct{ method, path string }{
		{http.MethodPost, "/v1/tasks"},
		{http.MethodGet, "/v1/tasks"},
		{http.MethodGet, "/v1/conversations"},
		{http.MethodPost, "/v1/conversations"},
		{http.MethodGet, "/v1/me"},
	}

	for _, p := range paths {
		req := httptest.NewRequest(p.method, p.path, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a client: status = %d, want 401", p.method, p.path, rec.Code)
		}
	}
}

func TestUnknownOrMalformedClientIsRefused(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, id := range []string{task.NewClientID(), "nonsense", "cli_short"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set(ClientHeader, id)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("client %q: status = %d, want 401", id, rec.Code)
		}
	}
}

// A prompt lands in the active conversation without the caller naming it.
func TestPromptLandsInTheActiveConversation(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodGet, "/v1/me", "")
	var me clientView
	decodeInto(t, rec, &me)

	created := createIn(t, s, "", "a question", "")
	if created.ConversationID != me.ActiveConversationID {
		t.Errorf("prompt landed in %s, want the active %s", created.ConversationID, me.ActiveConversationID)
	}
}

// Creating a conversation switches to it, since starting one almost always
// means wanting to talk in it.
func TestCreatingAConversationActivatesIt(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodPost, "/v1/conversations", `{"title":"work"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var created conversationView
	decodeInto(t, rec, &created)

	if created.Title != "work" {
		t.Errorf("title = %q, want work", created.Title)
	}
	if !created.Active {
		t.Error("a newly created conversation is not the active one")
	}

	landed := createIn(t, s, "", "a question", "")
	if landed.ConversationID != created.ID {
		t.Errorf("prompt landed in %s, want the new %s", landed.ConversationID, created.ID)
	}
}

// Switching back changes where a prompt lands.
func TestSwitchingTheActiveConversation(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	first := createIn(t, s, "", "in the first", "?wait=5s").ConversationID

	rec := do(t, s, http.MethodPost, "/v1/conversations", `{"title":"second"}`)
	var second conversationView
	decodeInto(t, rec, &second)

	if landed := createIn(t, s, "", "in the second", "?wait=5s"); landed.ConversationID != second.ID {
		t.Fatalf("prompt landed in %s, want %s", landed.ConversationID, second.ID)
	}

	// Switch back.
	rec = do(t, s, http.MethodPost, "/v1/conversations/"+first+"/activate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var activated conversationView
	decodeInto(t, rec, &activated)
	if !activated.Active {
		t.Error("the activated conversation does not report itself active")
	}

	if landed := createIn(t, s, "", "back in the first", "?wait=5s"); landed.ConversationID != first {
		t.Errorf("prompt landed in %s, want the reactivated %s", landed.ConversationID, first)
	}
}

// A client must not reach another's conversation, nor learn that it exists.
func TestOneClientCannotReachAnothers(t *testing.T) {
	s, _, repo, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	mine := createIn(t, s, "", "my question", "?wait=5s")

	// A second client, registered directly so the first stays the caller.
	stranger, err := task.NewClient("someone else")
	if err != nil {
		t.Fatalf("task.NewClient: %v", err)
	}
	if err := repo.CreateClient(t.Context(), stranger); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	theirs := task.NewConversation(stranger.ID, "")
	if err := repo.CreateConversation(t.Context(), theirs); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	cases := []struct{ method, path string }{
		{http.MethodGet, "/v1/conversations/" + theirs.ID},
		{http.MethodPost, "/v1/conversations/" + theirs.ID + "/activate"},
	}
	for _, c := range cases {
		rec := do(t, s, c.method, c.path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", c.method, c.path, rec.Code)
		}
	}

	// A prompt cannot be pushed into it either.
	rec := do(t, s, http.MethodPost, "/v1/tasks",
		`{"prompt":"sneak in","conversation_id":"`+theirs.ID+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("posting into another client's conversation: status = %d, want 404", rec.Code)
	}

	// Listing shows only the caller's own.
	rec = do(t, s, http.MethodGet, "/v1/conversations", "")
	var list listConversationsResponse
	decodeInto(t, rec, &list)
	for _, c := range list.Conversations {
		if c.ID == theirs.ID {
			t.Error("another client's conversation appeared in the listing")
		}
	}
	if len(list.Conversations) == 0 {
		t.Error("the caller's own conversations are missing")
	}
	_ = mine
}

// Listing tasks shows only the caller's own.
func TestTaskListingIsScopedToTheClient(t *testing.T) {
	s, _, repo, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	mine := createIn(t, s, "", "my question", "?wait=5s")

	stranger, _ := task.NewClient("someone else")
	_ = repo.CreateClient(t.Context(), stranger)
	theirs := task.NewConversation(stranger.ID, "")
	_ = repo.CreateConversation(t.Context(), theirs)
	strangersTask, _ := task.New(theirs.ID, "their question")
	_ = repo.Create(t.Context(), strangersTask)

	rec := do(t, s, http.MethodGet, "/v1/tasks", "")
	var list listTasksResponse
	decodeInto(t, rec, &list)

	var sawMine bool
	for _, summary := range list.Tasks {
		if summary.ID == strangersTask.ID {
			t.Error("another client's task appeared in the listing")
		}
		if summary.ID == mine.ID {
			sawMine = true
		}
	}
	if !sawMine {
		t.Error("the caller's own task is missing from the listing")
	}

	// Nor can it be read directly.
	if rec := do(t, s, http.MethodGet, "/v1/tasks/"+strangersTask.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("reading another client's task: status = %d, want 404", rec.Code)
	}
}

func TestClientNameTooLongIsRejected(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	long := make([]byte, task.MaxClientNameRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	rec := do(t, s, http.MethodPost, "/v1/clients", `{"name":"`+string(long)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// Registering without a body is allowed: a name is optional.
func TestRegisteringWithoutABody(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := do(t, s, http.MethodPost, "/v1/clients", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var client clientView
	decodeInto(t, rec, &client)
	if client.ActiveConversationID == "" {
		t.Error("no conversation to talk in")
	}
	_ = time.Now
}
