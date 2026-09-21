package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/auth"
	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// login : Attempts a login against the server.
func login(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

// loginAs : Logs in with the test account and returns what came back.
func loginAs(t *testing.T, s *Server, deviceName string) loginResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": testUsername, "password": testPassword, "device_name": deviceName,
	})
	rec := login(t, s, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("login: status %d, want 201: %s", rec.Code, rec.Body)
	}
	var out loginResponse
	decodeInto(t, rec, &out)
	return out
}

// withToken : Issues a request carrying the given Authorization header.
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

// Logging in registers the device and hands it a token, in one act: a token
// exists only for a device, and a device only for someone who proved who
// they are.
func TestLoginRegistersTheDeviceAndIssuesAToken(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	out := loginAs(t, s, "my phone")

	if !auth.LooksLikeToken(out.Token) {
		t.Errorf("token = %q, want a usable token", out.Token)
	}
	if out.User.Username != testUsername {
		t.Errorf("username = %q, want %q", out.User.Username, testUsername)
	}
	if !task.ValidDeviceID(out.Device.ID) {
		t.Errorf("device id = %q, want a device identifier", out.Device.ID)
	}
	if out.Device.Name != "my phone" {
		t.Errorf("device name = %q, want it echoed back", out.Device.Name)
	}
	if !task.ValidConversationID(out.Device.ActiveConversationID) {
		t.Errorf("active conversation = %q, want one ready to talk in", out.Device.ActiveConversationID)
	}
}

func TestLoginRefusesWrongCredentials(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	cases := map[string]string{
		"wrong password": `{"username":"` + testUsername + `","password":"not the password"}`,
		"unknown user":   `{"username":"nobody","password":"` + testPassword + `"}`,
		"empty password": `{"username":"` + testUsername + `","password":""}`,
		"empty username": `{"username":"","password":"` + testPassword + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := login(t, s, body)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
		})
	}
}

// A wrong password and an unknown username must be indistinguishable, or
// whoever is guessing learns which accounts exist.
func TestLoginRefusalsLookAlike(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	wrongPassword := login(t, s, `{"username":"`+testUsername+`","password":"wrong"}`).Body.String()
	unknownUser := login(t, s, `{"username":"nobody","password":"wrong"}`).Body.String()

	if wrongPassword != unknownUser {
		t.Errorf("an unknown user is distinguishable from a wrong password:\n  %s\n  %s",
			unknownUser, wrongPassword)
	}
}

// The token is returned once and stored only as a hash.
func TestTokenIsIssuedOnceAndStoredHashed(t *testing.T) {
	s, _, repo, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	out := loginAs(t, s, "my phone")

	stored, err := repo.DeviceByTokenHash(t.Context(), auth.HashToken(out.Token))
	if err != nil {
		t.Fatalf("DeviceByTokenHash: %v", err)
	}
	if stored.TokenHash == out.Token {
		t.Fatal("the token itself was stored")
	}

	// Reading back never discloses it again.
	rec := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+out.Token)
	if strings.Contains(rec.Body.String(), out.Token) {
		t.Errorf("the token was disclosed again: %s", rec.Body)
	}
}

func TestEveryEndpointButLoginRequiresAToken(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	for _, p := range []struct{ method, path string }{
		{http.MethodPost, "/v1/tasks"},
		{http.MethodGet, "/v1/tasks"},
		{http.MethodGet, "/v1/conversations"},
		{http.MethodPost, "/v1/conversations"},
		{http.MethodGet, "/v1/devices"},
		{http.MethodGet, "/v1/me"},
	} {
		rec := withToken(t, s, p.method, p.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", p.method, p.path, rec.Code)
		}
	}
}

func TestMalformedOrUnissuedTokensAreRefused(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})
	valid := tokenOf(s)
	unissued, _, _ := auth.NewToken()

	cases := map[string]string{
		"no scheme":    valid,
		"wrong scheme": "Basic " + valid,
		"empty bearer": "Bearer ",
		"rubbish":      "Bearer not-a-token",
		"truncated":    "Bearer " + valid[:len(valid)-4],
		"unissued":     "Bearer " + unissued,
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			rec := withToken(t, s, http.MethodGet, "/v1/me", header)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want it to name the scheme", got)
			}
		})
	}
}

// A lost phone is revoked from another device, which is the reason devices
// exist separately from the user at all.
func TestAnyDeviceCanRevokeAnother(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	phone := loginAs(t, s, "my phone")
	laptop := loginAs(t, s, "my laptop")

	if rec := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+phone.Token); rec.Code != http.StatusOK {
		t.Fatalf("the phone did not work to begin with: %d", rec.Code)
	}

	// Revoke the phone from the laptop.
	rec := withToken(t, s, http.MethodDelete, "/v1/devices/"+phone.Device.ID, "Bearer "+laptop.Token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d, want 204: %s", rec.Code, rec.Body)
	}

	if rec := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+phone.Token); rec.Code != http.StatusUnauthorized {
		t.Errorf("the revoked phone still works: status = %d", rec.Code)
	}
	if rec := withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+laptop.Token); rec.Code != http.StatusOK {
		t.Errorf("the laptop stopped working too: status = %d", rec.Code)
	}
}

// One user's device must not revoke another user's.
func TestADeviceCannotRevokeAnotherUsers(t *testing.T) {
	s, _, repo, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})
	mine := loginAs(t, s, "mine")

	stranger, _ := task.NewUser("stranger", "hash")
	_ = repo.CreateUser(t.Context(), stranger)
	theirDevice, _ := task.NewDevice(stranger.ID, "theirs", "their-hash")
	_ = repo.CreateDevice(t.Context(), theirDevice)

	rec := withToken(t, s, http.MethodDelete, "/v1/devices/"+theirDevice.ID, "Bearer "+mine.Token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// A device listing shows the user's own devices, marking which is in use and
// which have been revoked.
func TestDeviceListing(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	phone := loginAs(t, s, "my phone")
	laptop := loginAs(t, s, "my laptop")
	withToken(t, s, http.MethodDelete, "/v1/devices/"+phone.Device.ID, "Bearer "+laptop.Token)

	rec := withToken(t, s, http.MethodGet, "/v1/devices", "Bearer "+laptop.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var list listDevicesResponse
	decodeInto(t, rec, &list)

	var sawCurrent, sawRevoked bool
	for _, d := range list.Devices {
		if d.ID == laptop.Device.ID {
			if !d.Current {
				t.Error("the device in use is not marked current")
			}
			sawCurrent = true
		}
		if d.ID == phone.Device.ID {
			if !d.Revoked {
				t.Error("the revoked device is not marked revoked")
			}
			sawRevoked = true
		}
	}
	if !sawCurrent || !sawRevoked {
		t.Errorf("listing did not show both devices: %+v", list.Devices)
	}
}

// Neither a password nor a token may reach the log.
func TestSecretsAreNotLogged(t *testing.T) {
	s, logs, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	out := loginAs(t, s, "my phone")
	login(t, s, `{"username":"`+testUsername+`","password":"`+testPassword+`-wrong"}`)
	withToken(t, s, http.MethodGet, "/v1/me", "Bearer "+out.Token)

	for name, secret := range map[string]string{
		"password": testPassword,
		"token":    out.Token,
	} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the %s was written to the log", name)
		}
	}
}

func TestLoginRejectsBadRequests(t *testing.T) {
	s, _, _, _ := newTaskServer(t, stubPinger{}, &provider.Stub{})

	long := strings.Repeat("a", task.MaxDeviceNameRunes+1)
	cases := map[string]string{
		"not json":         `nonsense`,
		"unknown field":    `{"username":"x","password":"y","admin":true}`,
		"device name long": `{"username":"` + testUsername + `","password":"` + testPassword + `","device_name":"` + long + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := login(t, s, body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
		})
	}
}
