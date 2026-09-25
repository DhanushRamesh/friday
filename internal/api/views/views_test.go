package views_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// The JSON names here are the API's published contract: a client reads them,
// and renaming one silently breaks it. These tests pin the names rather than
// the rendering, which the handler tests already cover.

// keys : The field names a value marshals to.
func keys(t *testing.T, v any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	out := map[string]bool{}
	for k := range fields {
		out[k] = true
	}
	return out
}

// assertKeys : Reports any published name that is missing or unexpected.
func assertKeys(t *testing.T, v any, want ...string) {
	t.Helper()
	got := keys(t, v)
	for _, name := range want {
		if !got[name] {
			t.Errorf("%T does not publish %q", v, name)
		}
		delete(got, name)
	}
	for name := range got {
		t.Errorf("%T publishes an unexpected %q", v, name)
	}
}

func TestChatPublishesItsFields(t *testing.T) {
	now := time.Now().UTC().Truncate(chat.StoredPrecision)
	tk := &chat.Chat{
		ID: chat.NewID(), SessionID: chat.NewSessionID(), Prompt: "hello",
		Status: chat.StatusCompleted, Response: "hi", CreatedAt: now, UpdatedAt: now,
		StartedAt: &now, FinishedAt: &now,
	}

	assertKeys(t, views.OfChat(tk),
		"id", "session_id", "prompt", "status", "response",
		"created_at", "updated_at", "started_at", "finished_at")
}

// A listing must not carry response bodies: they can be large, and a listing
// is read to choose one, not to read them all.
func TestSummaryOmitsTheResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(chat.StoredPrecision)
	got := keys(t, views.OfSummary(chat.Summary{
		ID: chat.NewID(), Prompt: "hello", Status: chat.StatusCompleted,
		CreatedAt: now, UpdatedAt: now,
	}))

	if got["response"] {
		t.Error("a chat summary publishes a response body")
	}
	if !got["prompt"] || !got["status"] {
		t.Errorf("a summary is missing its basics: %v", got)
	}
}

// The password hash must never be published, whatever else a user carries.
func TestUserNeverPublishesTheHash(t *testing.T) {
	user, err := chat.NewUser("tester", "$2a$04$averyrealisticlookinghashvalue")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}

	assertKeys(t, views.OfUser(user), "id", "username", "created_at")

	raw, _ := json.Marshal(views.OfUser(user))
	for _, prefix := range []string{"$2a$", "$2b$", "$2y$"} {
		if strings.Contains(string(raw), prefix) {
			t.Errorf("the password hash was published: %s", raw)
		}
	}
}

// The token is never published with a client: only its hash is stored, and
// the one time the token itself appears is the login response.
func TestClientNeverPublishesTheToken(t *testing.T) {
	client, err := chat.NewClient(chat.NewUserID(), "my phone", "a-token-hash", chat.ChannelDirect)
	if err != nil {
		t.Fatalf("chat.NewClient: %v", err)
	}
	client.ActiveSessionID = chat.NewSessionID()

	assertKeys(t, views.OfClient(*client, true),
		"id", "name", "channel", "current", "revoked", "active_session_id",
		"created_at")

	raw, _ := json.Marshal(views.OfClient(*client, true))
	if strings.Contains(string(raw), "a-token-hash") {
		t.Errorf("the token hash was published: %s", raw)
	}
}

// Revoked is published even when false, so a client that is fine is visibly
// fine rather than merely silent about it.
func TestClientAlwaysStatesWhetherRevoked(t *testing.T) {
	client, _ := chat.NewClient(chat.NewUserID(), "", "hash", chat.ChannelDirect)

	got := keys(t, views.OfClient(*client, false))
	if !got["revoked"] || !got["current"] {
		t.Errorf("revoked and current must always appear: %v", got)
	}
	// An unrevoked client has no revocation time to publish.
	if got["revoked_at"] {
		t.Error("revoked_at appears on a client that was never revoked")
	}
}

func TestSessionPublishesItsFields(t *testing.T) {
	session := chat.NewSession(chat.NewUserID(), "groceries")

	assertKeys(t, views.OfSession(*session, true),
		"id", "title", "active", "created_at", "updated_at")
}

func TestMessagesKeepTheirOrderAndSequence(t *testing.T) {
	now := time.Now().UTC()
	got := views.OfMessages([]chat.Message{
		{Seq: 1, Kind: "update", Text: "first", CreatedAt: now},
		{Seq: 2, Kind: "final", Text: "second", CreatedAt: now},
	})

	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}
	if got[0].Seq != 1 || got[0].Text != "first" {
		t.Errorf("first message = %+v", got[0])
	}
	if got[1].Seq != 2 || got[1].Text != "second" {
		t.Errorf("second message = %+v", got[1])
	}
	assertKeys(t, got[0], "seq", "kind", "text", "created_at")
}
