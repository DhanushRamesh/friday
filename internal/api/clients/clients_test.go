package clients_test

import (
	"net/http"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/clients"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// Me reports who is calling, from what, and where a prompt will land.
func TestMeDescribesTheCaller(t *testing.T) {
	e := apitest.New(t)

	rec := e.Get(t, "/v1/me")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var out clients.MeResponse
	e.Decode(t, rec, &out)

	if out.User.Username != apitest.Username {
		t.Errorf("username = %q, want %q", out.User.Username, apitest.Username)
	}
	if out.Client.ID != e.Client.ID {
		t.Errorf("client = %q, want the one that called %q", out.Client.ID, e.Client.ID)
	}
	if !out.Client.Current {
		t.Error("the calling client is not marked current")
	}
	if out.Client.ActiveSessionID == "" {
		t.Error("no active session reported, so a caller cannot tell where a prompt lands")
	}
}

// A lost phone is revoked from another client, which is the reason clients
// exist separately from the user at all.
func TestAnyClientCanRevokeAnother(t *testing.T) {
	e := apitest.New(t)

	phone := e.Login(t, "my phone")
	laptop := e.Login(t, "my laptop")

	if rec := e.As(t, phone.Token, http.MethodGet, "/v1/me"); rec.Code != http.StatusOK {
		t.Fatalf("the phone did not work to begin with: %d", rec.Code)
	}

	// Revoke the phone from the laptop.
	rec := e.As(t, laptop.Token, http.MethodDelete, "/v1/clients/"+phone.Client.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d, want 204: %s", rec.Code, rec.Body)
	}

	if rec := e.As(t, phone.Token, http.MethodGet, "/v1/me"); rec.Code != http.StatusUnauthorized {
		t.Errorf("the revoked phone still works: status = %d", rec.Code)
	}
	if rec := e.As(t, laptop.Token, http.MethodGet, "/v1/me"); rec.Code != http.StatusOK {
		t.Errorf("the laptop stopped working too: status = %d", rec.Code)
	}
}

// One user's client must not revoke another user's.
func TestAClientCannotRevokeAnotherUsers(t *testing.T) {
	e := apitest.New(t)
	mine := e.Login(t, "mine")

	stranger, _ := chat.NewUser("stranger", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirClient, _ := chat.NewClient(stranger.ID, "theirs", "their-hash")
	_ = e.Repo.CreateClient(t.Context(), theirClient)

	rec := e.As(t, mine.Token, http.MethodDelete, "/v1/clients/"+theirClient.ID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestRevokingAnUnknownClientIsNotFound(t *testing.T) {
	e := apitest.New(t)

	for _, id := range []string{chat.NewClientID(), "not-an-id"} {
		rec := e.Do(t, http.MethodDelete, "/v1/clients/"+id, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("client %q: status = %d, want 404", id, rec.Code)
		}
	}
}

// A client listing shows the user's own clients, marking which is in use and
// which have been revoked. A revoked one stays listed, so that a revocation
// is visible rather than silently absent.
func TestClientListing(t *testing.T) {
	e := apitest.New(t)

	phone := e.Login(t, "my phone")
	laptop := e.Login(t, "my laptop")
	e.As(t, laptop.Token, http.MethodDelete, "/v1/clients/"+phone.Client.ID)

	rec := e.As(t, laptop.Token, http.MethodGet, "/v1/clients")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var list clients.ListResponse
	e.Decode(t, rec, &list)

	var sawCurrent, sawRevoked bool
	for _, d := range list.Clients {
		if d.ID == laptop.Client.ID {
			if !d.Current {
				t.Error("the client in use is not marked current")
			}
			sawCurrent = true
		}
		if d.ID == phone.Client.ID {
			if !d.Revoked {
				t.Error("the revoked client is not marked revoked")
			}
			sawRevoked = true
		}
	}
	if !sawCurrent || !sawRevoked {
		t.Errorf("listing did not show both clients: %+v", list.Clients)
	}
}

// A listing must show only the caller's own clients.
func TestClientListingIsScopedToTheUser(t *testing.T) {
	e := apitest.New(t)

	stranger, _ := chat.NewUser("stranger", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirClient, _ := chat.NewClient(stranger.ID, "theirs", "their-hash")
	_ = e.Repo.CreateClient(t.Context(), theirClient)

	rec := e.Get(t, "/v1/clients")
	var list clients.ListResponse
	e.Decode(t, rec, &list)

	for _, d := range list.Clients {
		if d.ID == theirClient.ID {
			t.Errorf("another user's client %s is listed", d.ID)
		}
	}
}
