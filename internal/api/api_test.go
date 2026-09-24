package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/api/apitest"
)

// What is tested here is true of the assembled API rather than of any one
// module: that authentication is applied to everything it should be, and that
// nothing secret reaches the log on the way through.

// Authentication is applied once, where the modules are mounted, rather than
// remembered by each of them. This is what checks that nothing was mounted
// outside that group.
func TestEveryEndpointButLoginRequiresAToken(t *testing.T) {
	e := apitest.New(t)

	for _, p := range []struct{ method, path string }{
		{http.MethodPost, "/v1/chats"},
		{http.MethodGet, "/v1/chats"},
		{http.MethodGet, "/v1/chats/" + apitest.SomeChatID},
		{http.MethodGet, "/v1/chats/" + apitest.SomeChatID + "/messages"},
		{http.MethodGet, "/v1/chats/" + apitest.SomeChatID + "/stream"},
		{http.MethodPost, "/v1/chats/" + apitest.SomeChatID + "/cancel"},
		{http.MethodGet, "/v1/sessions"},
		{http.MethodPost, "/v1/sessions"},
		{http.MethodGet, "/v1/sessions/" + apitest.SomeSessionID},
		{http.MethodPost, "/v1/sessions/" + apitest.SomeSessionID + "/activate"},
		{http.MethodGet, "/v1/clients"},
		{http.MethodDelete, "/v1/clients/" + apitest.SomeClientID},
		{http.MethodGet, "/v1/me"},
	} {
		rec := e.Anonymous(t, p.method, p.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", p.method, p.path, rec.Code)
		}
	}
}

// Health and readiness must stay reachable without a token: what probes them
// is infrastructure, which has none.
func TestHealthEndpointsNeedNoToken(t *testing.T) {
	e := apitest.New(t)

	for _, path := range []string{"/health", "/ready"} {
		rec := e.Anonymous(t, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200 without a token", path, rec.Code)
		}
	}
}

// Neither a password nor a token may reach the log, through any path.
func TestSecretsAreNotLogged(t *testing.T) {
	e := apitest.New(t)

	out := e.Login(t, "my phone")
	e.LoginRaw(t, `{"username":"`+apitest.Username+`","password":"`+apitest.Password+`-wrong"}`)
	e.As(t, out.Token, http.MethodGet, "/v1/me")
	e.Do(t, http.MethodPost, "/v1/chats?wait=5s", `{"prompt":"say something"}`)

	for name, secret := range map[string]string{
		"password":      apitest.Password,
		"issued token":  out.Token,
		"fixture token": e.Token,
	} {
		if strings.Contains(e.Logs.String(), secret) {
			t.Errorf("the %s was written to the log", name)
		}
	}
}

// Cross-origin access is wired from configuration, so the switch itself is
// worth a test: enabled by mistake in production it would let any page on
// the machine call the API, and disabled by mistake in development it stops
// the UI working with no clue why.
func TestCrossOriginIsOffUnlessAskedFor(t *testing.T) {
	e := apitest.New(t)

	rec := e.Preflight(t, http.MethodPost, "/v1/chats", "http://localhost:5000")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want nothing by default", got)
	}
}

func TestCrossOriginAnswersAPreflightWhenEnabled(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{AllowCrossOrigin: true})

	rec := e.Preflight(t, http.MethodPost, "/v1/chats", "http://localhost:5000")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5000" {
		t.Errorf("Allow-Origin = %q, want the origin echoed", got)
	}
	// The preflight carries no token, so it must be answered before
	// authentication rather than refused by it.
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Last-Event-ID") {
		t.Error("a resumed stream would be refused: Last-Event-ID is not allowed")
	}
}
