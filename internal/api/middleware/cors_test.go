package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/api/middleware"
)

// serve : Runs a request through CrossOrigin wrapping a handler that
// records whether it was reached.
func serve(
	t *testing.T,
	enabled bool,
	r *http.Request,
) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	var reached bool
	handler := middleware.CrossOrigin(enabled)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	return rec, reached
}

// preflight : The request a browser sends before a call it is unsure about.
func preflight(origin, method string) *http.Request {
	r := httptest.NewRequest(http.MethodOptions, "/v1/chats", nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", method)
	r.Header.Set("Access-Control-Request-Headers", "authorization")
	return r
}

// In production the UI is same-origin, so this must do nothing at all —
// including not answering a preflight, which would be permission granted.
func TestDisabledAddsNothing(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/chats", nil)
	r.Header.Set("Origin", "http://localhost:5000")

	rec, reached := serve(t, false, r)

	if !reached {
		t.Error("the request did not reach the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want nothing when disabled", got)
	}
}

func TestAllowsALoopbackOrigin(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:5000",
		"http://127.0.0.1:38211",
		"http://localhost",
	} {
		r := httptest.NewRequest(http.MethodGet, "/v1/chats", nil)
		r.Header.Set("Origin", origin)

		rec, reached := serve(t, true, r)

		if !reached {
			t.Errorf("%s: the request did not reach the handler", origin)
		}
		// Echoed rather than a wildcard: a wildcard cannot carry the bearer
		// token every call here presents.
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("%s: Allow-Origin = %q, want it echoed", origin, got)
		}
	}
}

// A cache must not hand one origin's response to another.
func TestVariesByOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/chats", nil)
	r.Header.Set("Origin", "http://localhost:5000")

	rec, _ := serve(t, true, r)

	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
}

// The check is on the parsed host, not a prefix: these all begin with
// something that looks local and none of them are.
func TestRefusesAnOriginThatMerelyLooksLocal(t *testing.T) {
	for _, origin := range []string{
		"http://localhost.attacker.com",
		"http://127.0.0.1.attacker.com",
		"https://notlocalhost",
		"http://evil.com",
		"file://",
		"null",
	} {
		rec, _ := serve(t, true, preflight(origin, http.MethodPost))

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: Allow-Origin = %q, want nothing", origin, got)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", origin, rec.Code)
		}
	}
}

// A preflight is a question about a later request, so it must be answered
// here and never reach a handler.
func TestPreflightIsAnsweredWithoutReachingTheHandler(t *testing.T) {
	rec, reached := serve(t, true, preflight("http://localhost:5000", http.MethodPost))

	if reached {
		t.Error("the preflight reached the handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("the preflight did not say which methods are allowed")
	}
}

// Every stream carries Last-Event-ID, which browsers do not allow by
// default. Without it in this list, a resumed stream is refused.
func TestPreflightAllowsTheHeadersTheClientSends(t *testing.T) {
	rec, _ := serve(t, true, preflight("http://localhost:5000", http.MethodGet))

	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	for _, header := range []string{"Authorization", "Content-Type", "Last-Event-ID"} {
		if !strings.Contains(allowed, header) {
			t.Errorf("Allow-Headers = %q, missing %s", allowed, header)
		}
	}
}

// A request with no Origin is not cross-origin and must pass through
// untouched.
func TestARequestWithNoOriginIsUnaffected(t *testing.T) {
	rec, reached := serve(t, true, httptest.NewRequest(http.MethodGet, "/v1/chats", nil))

	if !reached {
		t.Error("the request did not reach the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want nothing", got)
	}
}

// An OPTIONS that is not a preflight is an ordinary request.
func TestPlainOptionsIsNotTreatedAsAPreflight(t *testing.T) {
	r := httptest.NewRequest(http.MethodOptions, "/v1/chats", nil)
	r.Header.Set("Origin", "http://localhost:5000")

	_, reached := serve(t, true, r)

	if !reached {
		t.Error("a plain OPTIONS was swallowed as a preflight")
	}
}
