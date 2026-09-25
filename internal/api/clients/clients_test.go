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
	theirClient, _ := chat.NewClient(stranger.ID, "theirs", "their-hash", chat.ChannelDirect)
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
	theirClient, _ := chat.NewClient(stranger.ID, "theirs", "their-hash", chat.ChannelDirect)
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

// Home Assistant is handed a token through a screen with no field to declare
// itself with, so its client registers as direct. Without a way to correct
// that, every spoken prompt would be recorded as typed.
func TestAClientsChannelCanBeCorrected(t *testing.T) {
	e := apitest.New(t)
	satellite := e.Login(t, "home assistant")

	rec := e.Do(t, http.MethodPost,
		"/v1/clients/"+satellite.Client.ID+"/channel", `{"channel":"voice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var list clients.ListResponse
	e.Decode(t, rec, &list)
	var found bool
	for _, c := range list.Clients {
		if c.ID == satellite.Client.ID {
			found = true
			if c.Channel != string(chat.ChannelVoice) {
				t.Errorf("channel = %q, want voice", c.Channel)
			}
		}
	}
	if !found {
		t.Error("the client is missing from the listing it answered with")
	}
}

func TestAnInventedChannelIsRefused(t *testing.T) {
	e := apitest.New(t)
	// Another client, because a client may not change its own at all and
	// would be refused before the channel was looked at.
	other := e.Login(t, "somewhere else")

	rec := e.Do(t, http.MethodPost,
		"/v1/clients/"+other.Client.ID+"/channel", `{"channel":"shouting"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

// Another user's client is answered as missing, as everywhere else.
func TestAnotherUsersClientChannelCannotBeChanged(t *testing.T) {
	e := apitest.New(t)
	stranger, _ := chat.NewUser("stranger-chan", "hash")
	_ = e.Repo.CreateUser(t.Context(), stranger)
	theirClient, _ := chat.NewClient(stranger.ID, "theirs", "their-hash", chat.ChannelDirect)
	_ = e.Repo.CreateClient(t.Context(), theirClient)

	rec := e.Do(t, http.MethodPost,
		"/v1/clients/"+theirClient.ID+"/channel", `{"channel":"voice"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// The endpoint is authenticated by the very token whose privileges it would
// raise, so a client that could set its own channel could promote itself out
// of whatever the channel restricts.
func TestAClientCannotPromoteItself(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost,
		"/v1/clients/"+e.Client.ID+"/channel", `{"channel":"voice"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403: %s", rec.Code, rec.Body)
	}

	// Lowering its own is refused too. Allowing it would mean the rule
	// depends on which direction the change goes, and a client that can
	// write the field at all is one bug away from writing either value.
	rec = e.Do(t, http.MethodPost,
		"/v1/clients/"+e.Client.ID+"/channel", `{"channel":"direct"}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("lowering: status = %d, want 403: %s", rec.Code, rec.Body)
	}
}
