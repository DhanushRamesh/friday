package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/chat/memory"
)

// These tests are inside the package because they reach the router and the
// settings a Server is built with, neither of which is published. Everything
// that goes through the API from outside is in api_test.go, and each module's
// own behaviour is tested beside it.

// stubPinger : Stands in for a database handle.
type stubPinger struct{}

// Ping : Reports the database as reachable.
func (stubPinger) Ping(context.Context) error { return nil }

// newServer : Builds a Server with the least that New requires.
func newServer() *Server {
	return New(Options{Logger: slog.Default(), DB: stubPinger{}, Chats: memory.New()})
}

func TestNewAppliesDefaultRequestTimeout(t *testing.T) {
	s := newServer()
	if s.requestTimeout != DefaultRequestTimeout {
		t.Errorf("requestTimeout = %v, want %v", s.requestTimeout, DefaultRequestTimeout)
	}
}

// The route table is the API's whole contract, and it is assembled from ten
// packages. Asserting it here means a module can neither lose an endpoint nor
// quietly add one while being moved about, which a refactor is otherwise free
// to do unnoticed.
func TestRouteTableIsComplete(t *testing.T) {
	want := map[string]bool{
		"GET /health":                     true,
		"GET /ready":                      true,
		"POST /v1/auth/login":             true,
		"GET /v1/me":                      true,
		"GET /v1/clients":                 true,
		"DELETE /v1/clients/{id}":         true,
		"POST /v1/sessions":               true,
		"GET /v1/sessions":                true,
		"GET /v1/sessions/{id}":           true,
		"POST /v1/sessions/{id}/activate": true,
		"POST /v1/chats":                  true,
		"GET /v1/chats":                   true,
		"GET /v1/chats/{id}":              true,
		"GET /v1/chats/{id}/messages":     true,
		"GET /v1/chats/{id}/stream":       true,
		"POST /v1/chats/{id}/cancel":      true,
		// Fixed by the caller: Home Assistant appends these to the address it
		// was given, so they cannot live under /v1 with the rest.
		"GET /api/tags":  true,
		"POST /api/chat": true,
	}

	err := chi.Walk(newServer().router,
		func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			got := method + " " + strings.TrimSuffix(route, "/")
			if !want[got] {
				t.Errorf("unexpected route %s", got)
			}
			delete(want, got)
			return nil
		})
	if err != nil {
		t.Fatalf("walking routes: %v", err)
	}

	for route := range want {
		t.Errorf("missing route %s", route)
	}
}
