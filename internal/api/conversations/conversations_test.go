package conversations_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// Creating a conversation moves this client into it, since starting one almost
// always means wanting to talk in it.
func TestCreatingAConversationActivatesIt(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"groceries"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var created views.Conversation
	e.Decode(t, rec, &created)
	if created.Title != "groceries" {
		t.Errorf("title = %q, want it echoed back", created.Title)
	}
	if !created.Active {
		t.Error("the new conversation is not active, so a prompt would not land in it")
	}

	landed := e.CreateIn(t, "", "a question", "")
	if landed.ConversationID != created.ID {
		t.Errorf("prompt landed in %s, want the new conversation %s", landed.ConversationID, created.ID)
	}
}

// Asking not to activate leaves this client where it was, so a conversation can be
// prepared without interrupting what is being said now.
func TestAConversationCanBeCreatedWithoutActivating(t *testing.T) {
	e := apitest.New(t)
	before := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"later","activate":false}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var created views.Conversation
	e.Decode(t, rec, &created)
	if created.Active {
		t.Error("the conversation was activated despite activate:false")
	}

	landed := e.CreateIn(t, "", "another question", "?wait=5s")
	if landed.ConversationID != before.ConversationID {
		t.Errorf("prompt moved to %s, want it to stay in %s", landed.ConversationID, before.ConversationID)
	}
}

// Activating switches where this client's prompts land.
func TestActivatingAConversationMovesThisClient(t *testing.T) {
	e := apitest.New(t)
	first := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"second","activate":false}`)
	var second views.Conversation
	e.Decode(t, rec, &second)

	rec = e.Do(t, http.MethodPost, "/v1/conversations/"+second.ID+"/activate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var activated views.Conversation
	e.Decode(t, rec, &activated)
	if !activated.Active {
		t.Error("the activated conversation does not report itself active")
	}

	landed := e.CreateIn(t, "", "over here now", "?wait=5s")
	if landed.ConversationID != second.ID {
		t.Errorf("prompt landed in %s, want the activated %s", landed.ConversationID, second.ID)
	}
	if landed.ConversationID == first.ConversationID {
		t.Error("the prompt stayed in the first conversation")
	}
}

// A conversation is owned by the user, not the client, so another of their clients
// may switch to it. That is what lets a laptop pick up what a speaker started.
func TestAnotherClientCanActivateTheSameConversation(t *testing.T) {
	e := apitest.New(t)
	laptop := e.Login(t, "my laptop")

	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"shared"}`)
	var shared views.Conversation
	e.Decode(t, rec, &shared)

	rec = e.As(t, laptop.Token, http.MethodPost, "/v1/conversations/"+shared.ID+"/activate")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

func TestConversationCanBeReadBack(t *testing.T) {
	e := apitest.New(t)

	first := e.CreateIn(t, "", "first question", "?wait=5s")
	e.CreateIn(t, first.ConversationID, "second question", "?wait=5s")

	rec := e.Get(t, "/v1/conversations/"+first.ConversationID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var detail conversations.DetailResponse
	e.Decode(t, rec, &detail)

	if detail.Conversation.ID != first.ConversationID {
		t.Errorf("conversation id = %q", detail.Conversation.ID)
	}
	if len(detail.Chats) != 2 {
		t.Fatalf("got %d chats, want 2: %+v", len(detail.Chats), detail.Chats)
	}
	// Oldest first, so the exchange reads in the order it happened.
	if detail.Chats[0].Prompt != "first question" {
		t.Errorf("first chat = %q, want the earliest", detail.Chats[0].Prompt)
	}
}

func TestConversationsCanBeListed(t *testing.T) {
	e := apitest.New(t)

	created := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Get(t, "/v1/conversations")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var list conversations.ListResponse
	e.Decode(t, rec, &list)

	var found bool
	for _, c := range list.Conversations {
		if c.ID == created.ConversationID {
			found = true
			if !c.Active {
				t.Error("the conversation in use is not marked active")
			}
		}
	}
	if !found {
		t.Errorf("conversation %s missing from the listing", created.ConversationID)
	}
}

func TestListRejectsABadLimit(t *testing.T) {
	e := apitest.New(t)

	for _, path := range []string{"/v1/conversations?limit=0", "/v1/conversations?limit=lots"} {
		if rec := e.Get(t, path); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestUnknownConversationReadIsNotFound(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewConversationID(), "rubbish"} {
		if rec := e.Get(t, "/v1/conversations/"+id); rec.Code != http.StatusNotFound {
			t.Errorf("conversation %q: status = %d, want 404", id, rec.Code)
		}
	}
}

func TestActivatingAnUnknownConversationIsNotFound(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewConversationID(), "rubbish"} {
		rec := e.Do(t, http.MethodPost, "/v1/conversations/"+id+"/activate", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("conversation %q: status = %d, want 404", id, rec.Code)
		}
	}
}

// Telling one user that another's conversation exists reveals more than it should,
// so it is answered as missing rather than forbidden.
func TestAnotherUsersConversationIsHidden(t *testing.T) {
	e := apitest.New(t)

	stranger, _ := chat.NewUser("stranger", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirs := chat.NewConversation(stranger.ID, "private")
	_ = e.Repo.CreateConversation(t.Context(), theirs)

	if rec := e.Get(t, "/v1/conversations/"+theirs.ID); rec.Code != http.StatusNotFound {
		t.Errorf("read: status = %d, want 404", rec.Code)
	}
	rec := e.Do(t, http.MethodPost, "/v1/conversations/"+theirs.ID+"/activate", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("activate: status = %d, want 404", rec.Code)
	}

	var list conversations.ListResponse
	e.Decode(t, e.Get(t, "/v1/conversations"), &list)
	for _, c := range list.Conversations {
		if c.ID == theirs.ID {
			t.Error("another user's conversation appears in the listing")
		}
	}
}

func TestAConversationCanBeRenamed(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"frist"}`)
	var created views.Conversation
	e.Decode(t, rec, &created)

	rec = e.Do(t, http.MethodPost, "/v1/conversations/"+created.ID+"/rename",
		`{"title":"  the grocery list  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var renamed views.Conversation
	e.Decode(t, rec, &renamed)
	if renamed.Title != "the grocery list" {
		t.Errorf("title = %q, want it trimmed", renamed.Title)
	}

	// Read back, because the response could be right while the write was not.
	rec = e.Do(t, http.MethodGet, "/v1/conversations/"+created.ID, "")
	var detail conversations.DetailResponse
	e.Decode(t, rec, &detail)
	if detail.Conversation.Title != "the grocery list" {
		t.Errorf("stored title = %q, want the new one", detail.Conversation.Title)
	}
}

func TestRenamingToNothingClearsTheName(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"a mistake"}`)
	var created views.Conversation
	e.Decode(t, rec, &created)

	// Allowed rather than refused: a name given by mistake should be
	// removable without deleting the conversation under it.
	rec = e.Do(t, http.MethodPost, "/v1/conversations/"+created.ID+"/rename", `{"title":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var renamed views.Conversation
	e.Decode(t, rec, &renamed)
	if renamed.Title != "" {
		t.Errorf("title = %q, want it cleared", renamed.Title)
	}
}

func TestATitleTooLongIsRefused(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"fits"}`)
	var created views.Conversation
	e.Decode(t, rec, &created)

	long := strings.Repeat("a", chat.MaxTitleLen+1)
	rec = e.Do(t, http.MethodPost, "/v1/conversations/"+created.ID+"/rename",
		`{"title":"`+long+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}

	// Exactly the limit fits, so the boundary is not off by one.
	rec = e.Do(t, http.MethodPost, "/v1/conversations/"+created.ID+"/rename",
		`{"title":"`+strings.Repeat("a", chat.MaxTitleLen)+`"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("a title of exactly the limit was refused: %d %s", rec.Code, rec.Body)
	}
}

// A conversation that does not exist is answered as missing rather than as a bad
// request, so that a valid-looking identifier cannot be probed for existence.
func TestRenamingAMissingConversationIsNotFound(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost,
		"/v1/conversations/"+chat.NewConversationID()+"/rename", `{"title":"nowhere"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestArchivingHidesAConversationAndUnarchivingBringsItBack(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/conversations", `{"title":"old talk","activate":false}`)
	var s views.Conversation
	e.Decode(t, rec, &s)

	rec = e.Do(t, http.MethodPost, "/v1/conversations/"+s.ID+"/archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, want 200: %s", rec.Code, rec.Body)
	}

	if listed(t, e, "")[s.ID] {
		t.Error("an archived conversation is still in the ordinary listing")
	}
	if !listed(t, e, "?archived=true")[s.ID] {
		t.Error("an archived conversation is missing from the archived listing")
	}

	rec = e.Do(t, http.MethodPost, "/v1/conversations/"+s.ID+"/unarchive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if !listed(t, e, "")[s.ID] {
		t.Error("unarchiving did not bring the conversation back")
	}
}

// Archiving the conversation a prompt would land in has to move the client
// somewhere, or the next thing said goes into the conversation just put away.
func TestArchivingTheActiveConversationStartsAFreshOne(t *testing.T) {
	e := apitest.New(t)
	first := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Do(t, http.MethodPost, "/v1/conversations/"+first.ConversationID+"/archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out conversations.RemovedResponse
	e.Decode(t, rec, &out)

	if out.Active.ID == first.ConversationID {
		t.Fatal("still active in the conversation that was archived")
	}
	if !out.Active.Active {
		t.Error("the replacement does not report itself active")
	}
	// Fresh rather than the most recent survivor: having put a conversation
	// away, being dropped into an older one reads as the wrong thing.
	if out.Active.Title != "" {
		t.Errorf("replacement title = %q, want an empty new conversation", out.Active.Title)
	}

	landed := e.CreateIn(t, "", "where does this go", "?wait=5s")
	if landed.ConversationID != out.Active.ID {
		t.Errorf("prompt landed in %s, want the replacement %s",
			landed.ConversationID, out.Active.ID)
	}
}

func TestDeletingAConversationTakesItsChatsWithIt(t *testing.T) {
	e := apitest.New(t)
	created := e.CreateIn(t, "", "something to forget", "?wait=5s")

	rec := e.Do(t, http.MethodDelete, "/v1/conversations/"+created.ConversationID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, want 200: %s", rec.Code, rec.Body)
	}

	rec = e.Do(t, http.MethodGet, "/v1/conversations/"+created.ConversationID, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("the conversation survived the delete: %d", rec.Code)
	}
	rec = e.Do(t, http.MethodGet, "/v1/chats/"+created.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("a chat outlived the conversation it belonged to: %d", rec.Code)
	}
}

func TestDeletingAMissingConversationIsNotFound(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodDelete, "/v1/conversations/"+chat.NewConversationID(), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// listed : The identifiers a listing returns, keyed for lookup.
func listed(t *testing.T, e *apitest.Env, query string) map[string]bool {
	t.Helper()
	rec := e.Do(t, http.MethodGet, "/v1/conversations"+query, "")
	var out conversations.ListResponse
	e.Decode(t, rec, &out)
	ids := make(map[string]bool, len(out.Conversations))
	for _, s := range out.Conversations {
		ids[s.ID] = true
	}
	return ids
}
