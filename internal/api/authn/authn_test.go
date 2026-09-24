package authn_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/auth"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// TestMain : Lowers the password work factor for this package's tests.
//
// Only this package needs it. The fixture account's hash is already cheap, so
// verifying a real password is fast whatever the setting; but the refusal for
// an unknown username deliberately spends what a real check costs, and at the
// configured cost that is a fifth of a second, or more than two seconds under
// the race detector.
func TestMain(m *testing.M) {
	auth.PasswordCost = bcrypt.MinCost
	os.Exit(m.Run())
}

// Logging in registers the client and hands it a token, in one act: a token
// exists only for a client, and a client only for someone who proved who
// they are.
func TestLoginRegistersTheClientAndIssuesAToken(t *testing.T) {
	e := apitest.New(t)

	out := e.Login(t, "my phone")

	if !auth.LooksLikeToken(out.Token) {
		t.Errorf("token = %q, want a usable token", out.Token)
	}
	if out.User.Username != apitest.Username {
		t.Errorf("username = %q, want %q", out.User.Username, apitest.Username)
	}
	if !chat.ValidClientID(out.Client.ID) {
		t.Errorf("client id = %q, want a client identifier", out.Client.ID)
	}
	if out.Client.Name != "my phone" {
		t.Errorf("client name = %q, want it echoed back", out.Client.Name)
	}
	if !chat.ValidSessionID(out.Client.ActiveSessionID) {
		t.Errorf("active session = %q, want one ready to talk in", out.Client.ActiveSessionID)
	}
}

// A second client joins the session already there rather than starting a new
// thread, because a session belongs to the user and not to the client.
func TestASecondClientJoinsTheExistingSession(t *testing.T) {
	e := apitest.New(t)

	phone := e.Login(t, "my phone")
	laptop := e.Login(t, "my laptop")

	if laptop.Client.ActiveSessionID != phone.Client.ActiveSessionID {
		t.Errorf("laptop started in %q, want the phone's %q",
			laptop.Client.ActiveSessionID, phone.Client.ActiveSessionID)
	}
}

func TestLoginRefusesWrongCredentials(t *testing.T) {
	e := apitest.New(t)

	cases := map[string]string{
		"wrong password": `{"username":"` + apitest.Username + `","password":"not the password"}`,
		"unknown user":   `{"username":"nobody","password":"` + apitest.Password + `"}`,
		"empty password": `{"username":"` + apitest.Username + `","password":""}`,
		"empty username": `{"username":"","password":"` + apitest.Password + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := e.LoginRaw(t, body)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
		})
	}
}

// A wrong password and an unknown username must be indistinguishable, or
// whoever is guessing learns which accounts exist.
func TestLoginRefusalsLookAlike(t *testing.T) {
	e := apitest.New(t)

	wrongPassword := e.LoginRaw(t, `{"username":"`+apitest.Username+`","password":"wrong"}`).Body.String()
	unknownUser := e.LoginRaw(t, `{"username":"nobody","password":"wrong"}`).Body.String()

	if wrongPassword != unknownUser {
		t.Errorf("an unknown user is distinguishable from a wrong password:\n  %s\n  %s",
			unknownUser, wrongPassword)
	}
}

// The token is returned once and stored only as a hash.
func TestTokenIsIssuedOnceAndStoredHashed(t *testing.T) {
	e := apitest.New(t)

	out := e.Login(t, "my phone")

	stored, err := e.Repo.ClientByTokenHash(t.Context(), auth.HashToken(out.Token))
	if err != nil {
		t.Fatalf("ClientByTokenHash: %v", err)
	}
	if stored.TokenHash == out.Token {
		t.Fatal("the token itself was stored")
	}

	// Reading back never discloses it again.
	rec := e.As(t, out.Token, http.MethodGet, "/v1/me")
	if strings.Contains(rec.Body.String(), out.Token) {
		t.Errorf("the token was disclosed again: %s", rec.Body)
	}
}

func TestMalformedOrUnissuedTokensAreRefused(t *testing.T) {
	e := apitest.New(t)
	unissued, _, _ := auth.NewToken()

	cases := map[string]string{
		"no scheme":    e.Token,
		"wrong scheme": "Basic " + e.Token,
		"empty bearer": "Bearer ",
		"rubbish":      "Bearer not-a-token",
		"truncated":    "Bearer " + e.Token[:len(e.Token)-4],
		"unissued":     "Bearer " + unissued,
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			rec := e.Anonymous(t, http.MethodGet, "/v1/me", header)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want it to name the scheme", got)
			}
		})
	}
}

func TestLoginRejectsBadRequests(t *testing.T) {
	e := apitest.New(t)

	long := strings.Repeat("a", chat.MaxClientNameRunes+1)
	cases := map[string]string{
		"not json":         `nonsense`,
		"unknown field":    `{"username":"x","password":"y","admin":true}`,
		"client name long": `{"username":"` + apitest.Username + `","password":"` + apitest.Password + `","client_name":"` + long + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := e.LoginRaw(t, body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
		})
	}
}
