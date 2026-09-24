package sessions_test

import (
	"net/http"
	"testing"

	"github.com/DhanushRamesh/friday/internal/api/apitest"
	"github.com/DhanushRamesh/friday/internal/api/sessions"
	"github.com/DhanushRamesh/friday/internal/api/views"
	"github.com/DhanushRamesh/friday/internal/chat"
)

// Creating a session moves this client into it, since starting one almost
// always means wanting to talk in it.
func TestCreatingASessionActivatesIt(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"groceries"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var created views.Session
	e.Decode(t, rec, &created)
	if created.Title != "groceries" {
		t.Errorf("title = %q, want it echoed back", created.Title)
	}
	if !created.Active {
		t.Error("the new session is not active, so a prompt would not land in it")
	}

	landed := e.CreateIn(t, "", "a question", "")
	if landed.SessionID != created.ID {
		t.Errorf("prompt landed in %s, want the new session %s", landed.SessionID, created.ID)
	}
}

// Asking not to activate leaves this client where it was, so a session can be
// prepared without interrupting what is being said now.
func TestASessionCanBeCreatedWithoutActivating(t *testing.T) {
	e := apitest.New(t)
	before := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"later","activate":false}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var created views.Session
	e.Decode(t, rec, &created)
	if created.Active {
		t.Error("the session was activated despite activate:false")
	}

	landed := e.CreateIn(t, "", "another question", "?wait=5s")
	if landed.SessionID != before.SessionID {
		t.Errorf("prompt moved to %s, want it to stay in %s", landed.SessionID, before.SessionID)
	}
}

// Activating switches where this client's prompts land.
func TestActivatingASessionMovesThisClient(t *testing.T) {
	e := apitest.New(t)
	first := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"second","activate":false}`)
	var second views.Session
	e.Decode(t, rec, &second)

	rec = e.Do(t, http.MethodPost, "/v1/sessions/"+second.ID+"/activate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var activated views.Session
	e.Decode(t, rec, &activated)
	if !activated.Active {
		t.Error("the activated session does not report itself active")
	}

	landed := e.CreateIn(t, "", "over here now", "?wait=5s")
	if landed.SessionID != second.ID {
		t.Errorf("prompt landed in %s, want the activated %s", landed.SessionID, second.ID)
	}
	if landed.SessionID == first.SessionID {
		t.Error("the prompt stayed in the first session")
	}
}

// A session is owned by the user, not the client, so another of their clients
// may switch to it. That is what lets a laptop pick up what a speaker started.
func TestAnotherClientCanActivateTheSameSession(t *testing.T) {
	e := apitest.New(t)
	laptop := e.Login(t, "my laptop")

	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"shared"}`)
	var shared views.Session
	e.Decode(t, rec, &shared)

	rec = e.As(t, laptop.Token, http.MethodPost, "/v1/sessions/"+shared.ID+"/activate")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

func TestSessionCanBeReadBack(t *testing.T) {
	e := apitest.New(t)

	first := e.CreateIn(t, "", "first question", "?wait=5s")
	e.CreateIn(t, first.SessionID, "second question", "?wait=5s")

	rec := e.Get(t, "/v1/sessions/"+first.SessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var detail sessions.DetailResponse
	e.Decode(t, rec, &detail)

	if detail.Session.ID != first.SessionID {
		t.Errorf("session id = %q", detail.Session.ID)
	}
	if len(detail.Chats) != 2 {
		t.Fatalf("got %d chats, want 2: %+v", len(detail.Chats), detail.Chats)
	}
	// Oldest first, so the exchange reads in the order it happened.
	if detail.Chats[0].Prompt != "first question" {
		t.Errorf("first chat = %q, want the earliest", detail.Chats[0].Prompt)
	}
}

func TestSessionsCanBeListed(t *testing.T) {
	e := apitest.New(t)

	created := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Get(t, "/v1/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var list sessions.ListResponse
	e.Decode(t, rec, &list)

	var found bool
	for _, c := range list.Sessions {
		if c.ID == created.SessionID {
			found = true
			if !c.Active {
				t.Error("the session in use is not marked active")
			}
		}
	}
	if !found {
		t.Errorf("session %s missing from the listing", created.SessionID)
	}
}

func TestListRejectsABadLimit(t *testing.T) {
	e := apitest.New(t)

	for _, path := range []string{"/v1/sessions?limit=0", "/v1/sessions?limit=lots"} {
		if rec := e.Get(t, path); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestUnknownSessionReadIsNotFound(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewSessionID(), "rubbish"} {
		if rec := e.Get(t, "/v1/sessions/"+id); rec.Code != http.StatusNotFound {
			t.Errorf("session %q: status = %d, want 404", id, rec.Code)
		}
	}
}

func TestActivatingAnUnknownSessionIsNotFound(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewSessionID(), "rubbish"} {
		rec := e.Do(t, http.MethodPost, "/v1/sessions/"+id+"/activate", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("session %q: status = %d, want 404", id, rec.Code)
		}
	}
}

// Telling one user that another's session exists reveals more than it should,
// so it is answered as missing rather than forbidden.
func TestAnotherUsersSessionIsHidden(t *testing.T) {
	e := apitest.New(t)

	stranger, _ := chat.NewUser("stranger", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirs := chat.NewSession(stranger.ID, "private")
	_ = e.Repo.CreateSession(t.Context(), theirs)

	if rec := e.Get(t, "/v1/sessions/"+theirs.ID); rec.Code != http.StatusNotFound {
		t.Errorf("read: status = %d, want 404", rec.Code)
	}
	rec := e.Do(t, http.MethodPost, "/v1/sessions/"+theirs.ID+"/activate", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("activate: status = %d, want 404", rec.Code)
	}

	var list sessions.ListResponse
	e.Decode(t, e.Get(t, "/v1/sessions"), &list)
	for _, c := range list.Sessions {
		if c.ID == theirs.ID {
			t.Error("another user's session appears in the listing")
		}
	}
}
