package sessions_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/sessions"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
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

func TestASessionCanBeRenamed(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"frist"}`)
	var created views.Session
	e.Decode(t, rec, &created)

	rec = e.Do(t, http.MethodPost, "/v1/sessions/"+created.ID+"/rename",
		`{"title":"  the grocery list  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var renamed views.Session
	e.Decode(t, rec, &renamed)
	if renamed.Title != "the grocery list" {
		t.Errorf("title = %q, want it trimmed", renamed.Title)
	}

	// Read back, because the response could be right while the write was not.
	rec = e.Do(t, http.MethodGet, "/v1/sessions/"+created.ID, "")
	var detail sessions.DetailResponse
	e.Decode(t, rec, &detail)
	if detail.Session.Title != "the grocery list" {
		t.Errorf("stored title = %q, want the new one", detail.Session.Title)
	}
}

func TestRenamingToNothingClearsTheName(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"a mistake"}`)
	var created views.Session
	e.Decode(t, rec, &created)

	// Allowed rather than refused: a name given by mistake should be
	// removable without deleting the conversation under it.
	rec = e.Do(t, http.MethodPost, "/v1/sessions/"+created.ID+"/rename", `{"title":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var renamed views.Session
	e.Decode(t, rec, &renamed)
	if renamed.Title != "" {
		t.Errorf("title = %q, want it cleared", renamed.Title)
	}
}

func TestATitleTooLongIsRefused(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"fits"}`)
	var created views.Session
	e.Decode(t, rec, &created)

	long := strings.Repeat("a", chat.MaxTitleLen+1)
	rec = e.Do(t, http.MethodPost, "/v1/sessions/"+created.ID+"/rename",
		`{"title":"`+long+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}

	// Exactly the limit fits, so the boundary is not off by one.
	rec = e.Do(t, http.MethodPost, "/v1/sessions/"+created.ID+"/rename",
		`{"title":"`+strings.Repeat("a", chat.MaxTitleLen)+`"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("a title of exactly the limit was refused: %d %s", rec.Code, rec.Body)
	}
}

// A session that does not exist is answered as missing rather than as a bad
// request, so that a valid-looking identifier cannot be probed for existence.
func TestRenamingAMissingSessionIsNotFound(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost,
		"/v1/sessions/"+chat.NewSessionID()+"/rename", `{"title":"nowhere"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestArchivingHidesASessionAndUnarchivingBringsItBack(t *testing.T) {
	e := apitest.New(t)
	rec := e.Do(t, http.MethodPost, "/v1/sessions", `{"title":"old talk","activate":false}`)
	var s views.Session
	e.Decode(t, rec, &s)

	rec = e.Do(t, http.MethodPost, "/v1/sessions/"+s.ID+"/archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, want 200: %s", rec.Code, rec.Body)
	}

	if listed(t, e, "")[s.ID] {
		t.Error("an archived session is still in the ordinary listing")
	}
	if !listed(t, e, "?archived=true")[s.ID] {
		t.Error("an archived session is missing from the archived listing")
	}

	rec = e.Do(t, http.MethodPost, "/v1/sessions/"+s.ID+"/unarchive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if !listed(t, e, "")[s.ID] {
		t.Error("unarchiving did not bring the session back")
	}
}

// Archiving the session a prompt would land in has to move the client
// somewhere, or the next thing said goes into the conversation just put away.
func TestArchivingTheActiveSessionStartsAFreshOne(t *testing.T) {
	e := apitest.New(t)
	first := e.CreateIn(t, "", "a question", "?wait=5s")

	rec := e.Do(t, http.MethodPost, "/v1/sessions/"+first.SessionID+"/archive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out sessions.RemovedResponse
	e.Decode(t, rec, &out)

	if out.Active.ID == first.SessionID {
		t.Fatal("still active in the session that was archived")
	}
	if !out.Active.Active {
		t.Error("the replacement does not report itself active")
	}
	// Fresh rather than the most recent survivor: having put a conversation
	// away, being dropped into an older one reads as the wrong thing.
	if out.Active.Title != "" {
		t.Errorf("replacement title = %q, want an empty new session", out.Active.Title)
	}

	landed := e.CreateIn(t, "", "where does this go", "?wait=5s")
	if landed.SessionID != out.Active.ID {
		t.Errorf("prompt landed in %s, want the replacement %s",
			landed.SessionID, out.Active.ID)
	}
}

func TestDeletingASessionTakesItsChatsWithIt(t *testing.T) {
	e := apitest.New(t)
	created := e.CreateIn(t, "", "something to forget", "?wait=5s")

	rec := e.Do(t, http.MethodDelete, "/v1/sessions/"+created.SessionID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d, want 200: %s", rec.Code, rec.Body)
	}

	rec = e.Do(t, http.MethodGet, "/v1/sessions/"+created.SessionID, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("the session survived the delete: %d", rec.Code)
	}
	rec = e.Do(t, http.MethodGet, "/v1/chats/"+created.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("a chat outlived the session it belonged to: %d", rec.Code)
	}
}

func TestDeletingAMissingSessionIsNotFound(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodDelete, "/v1/sessions/"+chat.NewSessionID(), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// listed : The identifiers a listing returns, keyed for lookup.
func listed(t *testing.T, e *apitest.Env, query string) map[string]bool {
	t.Helper()
	rec := e.Do(t, http.MethodGet, "/v1/sessions"+query, "")
	var out sessions.ListResponse
	e.Decode(t, rec, &out)
	ids := make(map[string]bool, len(out.Sessions))
	for _, s := range out.Sessions {
		ids[s.ID] = true
	}
	return ids
}
