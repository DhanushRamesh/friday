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

// testClients : The client each test server registered, so that every helper
// can present one without it being threaded through each call. Test-only.
var testClients sync.Map

// clientOf : Returns the client registered for a test server.
func clientOf(s *Server) string {
	id, _ := testClients.Load(s)
	str, _ := id.(string)
	return str
}

// registerTestClient : Registers a client against the server and records it.
func registerTestClient(t *testing.T, s *Server, repo *memRepo) string {
	t.Helper()

	client, err := task.NewClient("test client")
	if err != nil {
		t.Fatalf("task.NewClient: %v", err)
	}
	ctx := context.Background()
	if err := repo.CreateClient(ctx, client); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}

	conversation := task.NewConversation(client.ID, "")
	if err := repo.CreateConversation(ctx, conversation); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if err := repo.SetActiveConversation(ctx, client.ID, conversation.ID); err != nil {
		t.Fatalf("SetActiveConversation: %v", err)
	}

	testClients.Store(s, client.ID)
	t.Cleanup(func() { testClients.Delete(s) })
	return client.ID
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
	registerTestClient(t, srv, repo)
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
