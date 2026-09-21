package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/auth"
	"github.com/DhanushRamesh/friday/internal/events"
	"github.com/DhanushRamesh/friday/internal/logging"
	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/runner"
	"github.com/DhanushRamesh/friday/internal/task"
)

// stubPinger : Stands in for a database handle in tests.
type stubPinger struct{ err error }

// Ping : Returns the configured error.
func (s stubPinger) Ping(context.Context) error { return s.err }

// newTestServer : Builds a Server writing its logs into the returned buffer.
func newTestServer(t *testing.T, db Pinger) (*Server, *bytes.Buffer) {
	t.Helper()
	s, buf, _, _ := newTaskServer(t, db, &provider.Stub{})
	return s, buf
}

// testTokens : The token each test server's client authenticates with, so
// that every helper can present one without it being threaded through each
// call. Test-only.
var testTokens sync.Map

// tokenOf : Returns the token registered for a test server.
func tokenOf(s *Server) string {
	token, _ := testTokens.Load(s)
	str, _ := token.(string)
	return str
}

// testUsername and testPassword : The account every test server is given.
const (
	testUsername = "tester"
	testPassword = "correct horse battery staple"
)

// registerTestDevice : Creates the test user and logs a device in, recording
// its token for the helpers to present.
func registerTestDevice(t *testing.T, s *Server, repo *memRepo) string {
	t.Helper()
	ctx := context.Background()

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}
	user, err := task.NewUser(testUsername, hash)
	if err != nil {
		t.Fatalf("task.NewUser: %v", err)
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	token, tokenHash, err := auth.NewToken()
	if err != nil {
		t.Fatalf("auth.NewToken: %v", err)
	}
	device, err := task.NewDevice(user.ID, "test device", tokenHash)
	if err != nil {
		t.Fatalf("task.NewDevice: %v", err)
	}
	if err := repo.CreateDevice(ctx, device); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	conversation := task.NewConversation(user.ID, "")
	if err := repo.CreateConversation(ctx, conversation); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if err := repo.SetActiveConversation(ctx, user.ID, device.ID, conversation.ID); err != nil {
		t.Fatalf("SetActiveConversation: %v", err)
	}

	testTokens.Store(s, token)
	t.Cleanup(func() { testTokens.Delete(s) })
	return token
}

// newTaskServer : Builds a Server with an in-memory task repository and a
// runner driving the given provider, with one client already registered.
func newTaskServer(t *testing.T, db Pinger, p provider.Provider) (*Server, *bytes.Buffer, *memRepo, *runner.Runner) {
	t.Helper()

	buf := &bytes.Buffer{}
	logger, err := logging.New(buf, logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	repo := newMemRepo()
	bus := events.NewBus(logger.Logger)
	t.Cleanup(bus.Close)

	r, err := runner.New(runner.Options{
		Repository: repo,
		Provider:   p,
		Logger:     logger.Logger,
		Publisher:  bus,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.Shutdown(ctx)
	})

	srv := New(Options{Logger: logger.Logger, DB: db, Tasks: repo, Runner: r, Events: bus})
	registerTestDevice(t, srv, repo)
	return srv, buf, repo, r
}

// records : Parses every log record written to buf.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func TestNewAppliesDefaultRequestTimeout(t *testing.T) {
	s := New(Options{Logger: slog.Default(), DB: stubPinger{}, Tasks: newMemRepo()})
	if s.requestTimeout != DefaultRequestTimeout {
		t.Errorf("requestTimeout = %v, want %v", s.requestTimeout, DefaultRequestTimeout)
	}
}
