package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/auth"
	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// withToken : Issues a request carrying the given Authorization header value.
func withToken(t *testing.T, s *Server, method, path, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

// Registration that anyone may perform is no authentication at all, since a
// stranger would simply issue themselves a token.
func TestRegistrationRequiresTheSecret(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	cases := map[string]string{
		"no secret":    "",
		"wrong secret": "not-the-secret",
		"near miss":    testRegistrationSecret[:len(testRegistrationSecret)-1],
	}
	for name, secret := range cases {
		t.Run(name, func(t *testing.T) {
			rec := register(t, s, `{"name":"intruder"}`, secret)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
		})
	}
}

// A blank secret disables registration rather than accepting anything, which
// is the difference between a safe default and an open door.
func TestBlankSecretDisablesRegistration(t *testing.T) {
	s, _, repo, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})
	open := New(Options{
		Logger: s.logger, DB: stubPinger{}, Tasks: repo, Runner: s.runner, Events: s.events,
		RegistrationSecret: "",
	})

	for _, secret := range []string{"", "anything"} {
		rec := register(t, open, `{"name":"intruder"}`, secret)
		if rec.Code == http.StatusCreated {
			t.Fatalf("a client was registered with secret %q while registration is disabled", secret)
		}
		if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusUnauthorized {
			t.Errorf("secret %q: status = %d, want 503 or 401", secret, rec.Code)
		}
	}
}

// The token is returned once and never stored in a recoverable form.
func TestTokenIsIssuedOnceAndStoredHashed(t *testing.T) {
	s, _, repo, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := register(t, s, `{"name":"my phone"}`, testRegistrationSecret)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var registered registeredClientView
	decodeInto(t, rec, &registered)

	if !auth.LooksLikeToken(registered.Token) {
		t.Fatalf("token = %q, want a usable token", registered.Token)
	}

	stored, err := repo.GetClient(t.Context(), registered.ID)
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if stored.TokenHash == registered.Token {
		t.Fatal("the token itself was stored")
	}
	if stored.TokenHash != auth.HashToken(registered.Token) {
		t.Error("the stored hash does not match the issued token")
	}

	// Reading the client back never discloses the token again.
	rec = withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+registered.Token)
	if strings.Contains(rec.Body.String(), registered.Token) {
		t.Errorf("the token was disclosed again: %s", rec.Body)
	}
}

// A freshly issued token works on every endpoint.
func TestAnIssuedTokenAuthenticates(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := register(t, s, `{"name":"my phone"}`, testRegistrationSecret)
	var registered registeredClientView
	decodeInto(t, rec, &registered)

	rec = withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+registered.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var me clientView
	decodeInto(t, rec, &me)
	if me.ID != registered.ID {
		t.Errorf("authenticated as %s, want %s", me.ID, registered.ID)
	}
}

func TestMalformedAuthorizationIsRefused(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})
	valid := tokenOf(s)

	cases := map[string]string{
		"absent":         "",
		"no scheme":      valid,
		"wrong scheme":   "Basic " + valid,
		"empty bearer":   "Bearer ",
		"rubbish":        "Bearer not-a-token",
		"truncated":      "Bearer " + valid[:len(valid)-4],
		"extra appended": "Bearer " + valid + "x",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			rec := withToken(t, s, http.MethodGet, "/v1/me", header)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want it to name the scheme", got)
			}
		})
	}
}

// A token that was never issued must be refused, however well formed.
func TestAnUnissuedTokenIsRefused(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	unissued, _, err := auth.NewToken()
	if err != nil {
		t.Fatalf("auth.NewToken: %v", err)
	}
	rec := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+unissued)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// A revoked device must stop working, which is the point of revocation.
func TestRevokingStopsTheToken(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := register(t, s, `{"name":"lost phone"}`, testRegistrationSecret)
	var registered registeredClientView
	decodeInto(t, rec, &registered)
	bearer := "Bearer " + registered.Token

	if rec := withToken(t, s, http.MethodGet, "/v1/me", bearer); rec.Code != http.StatusOK {
		t.Fatalf("before revoking: status = %d, want 200", rec.Code)
	}

	rec = withToken(t, s, http.MethodDelete, "/v1/clients/"+registered.ID, bearer)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d, want 204: %s", rec.Code, rec.Body)
	}

	if rec := withToken(t, s, http.MethodGet, "/v1/me", bearer); rec.Code != http.StatusUnauthorized {
		t.Errorf("after revoking: status = %d, want 401", rec.Code)
	}
	if rec := withToken(t, s, http.MethodPost, "/v1/tasks", bearer); rec.Code != http.StatusUnauthorized {
		t.Errorf("a revoked client could still submit: status = %d, want 401", rec.Code)
	}
}

// A stolen token must not be usable to lock out the rest.
func TestAClientCannotRevokeAnother(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := register(t, s, `{"name":"other phone"}`, testRegistrationSecret)
	var other registeredClientView
	decodeInto(t, rec, &other)

	// The server's own client tries to revoke the newly registered one.
	rec = withToken(t, s, http.MethodDelete, "/v1/clients/"+other.ID, "Bearer "+tokenOf(s))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	// The other client still works.
	if rec := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+other.Token); rec.Code != http.StatusOK {
		t.Errorf("the targeted client stopped working: status = %d", rec.Code)
	}
}

// A refusal must not say which kind it was, or guessing becomes cheaper.
func TestRefusalsAreIndistinguishable(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := register(t, s, `{"name":"to be revoked"}`, testRegistrationSecret)
	var revoked registeredClientView
	decodeInto(t, rec, &revoked)
	withToken(t, s, http.MethodDelete, "/v1/clients/"+revoked.ID, "Bearer "+revoked.Token)

	unissued, _, _ := auth.NewToken()

	revokedBody := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+revoked.Token).Body.String()
	unissuedBody := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+unissued).Body.String()

	if revokedBody != unissuedBody {
		t.Errorf("a revoked token is distinguishable from an unissued one:\n revoked:  %s\n unissued: %s",
			revokedBody, unissuedBody)
	}
}

// A token must never be written to the log, whatever happens to it.
func TestTokensAreNotLogged(t *testing.T) {
	s, logs, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := register(t, s, `{"name":"my phone"}`, testRegistrationSecret)
	var registered registeredClientView
	decodeInto(t, rec, &registered)

	withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+registered.Token)
	unissued, _, _ := auth.NewToken()
	withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+unissued)

	for name, secret := range map[string]string{
		"issued token":        registered.Token,
		"rejected token":      unissued,
		"registration secret": testRegistrationSecret,
	} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the %s was written to the log", name)
		}
	}
}

// Ownership still holds once tokens are in play.
func TestOneClientStillCannotReachAnothersWork(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	rec := register(t, s, `{"name":"second phone"}`, testRegistrationSecret)
	var other registeredClientView
	decodeInto(t, rec, &other)

	mine := createIn(t, s, "", "my question", "?wait=5s")

	rec = withToken(t, s, http.MethodGet, "/v1/tasks/"+mine.ID, "Bearer "+other.Token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("another client read my task: status = %d, want 404", rec.Code)
	}
	_ = task.StatusPending
}
