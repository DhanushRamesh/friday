package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/DhanushRamesh/friday/internal/logging"
)

func testLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger, err := logging.New(buf, logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	return logger.Logger, buf
}

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

// A handler logging through the request context must produce the same
// request_id as the middleware's own request record.
func TestRequestIDReachesHandlerLogs(t *testing.T) {
	logger, buf := testLogger(t)

	handler := middleware.RequestID(requestContext(requestLogger(logger)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.InfoContext(r.Context(), "handler work")
			w.WriteHeader(http.StatusOK)
		}),
	)))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks", nil))

	got := records(t, buf)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2 (handler + request): %v", len(got), got)
	}

	handlerID, _ := got[0]["request_id"].(string)
	requestID, _ := got[1]["request_id"].(string)
	if handlerID == "" {
		t.Error("handler record has no request_id; context propagation is broken")
	}
	if handlerID != requestID {
		t.Errorf("request_id mismatch: handler %q, request %q", handlerID, requestID)
	}
}

func TestRequestLoggerLevelsByStatus(t *testing.T) {
	cases := []struct {
		path      string
		status    int
		wantLevel string
	}{
		{"/v1/tasks", http.StatusOK, "INFO"},
		{"/v1/tasks", http.StatusNotFound, "WARN"},
		{"/v1/tasks", http.StatusInternalServerError, "ERROR"},
		{"/health", http.StatusOK, "DEBUG"},
	}

	for _, tc := range cases {
		t.Run(tc.path+"/"+http.StatusText(tc.status), func(t *testing.T) {
			logger, buf := testLogger(t)
			handler := requestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tc.path, nil))

			got := records(t, buf)
			if len(got) != 1 {
				t.Fatalf("got %d records, want 1", len(got))
			}
			if got[0]["level"] != tc.wantLevel {
				t.Errorf("level = %v, want %v", got[0]["level"], tc.wantLevel)
			}
			if got[0]["status"] != float64(tc.status) {
				t.Errorf("status = %v, want %d", got[0]["status"], tc.status)
			}
		})
	}
}

func TestRecovererLogsAndReturns500(t *testing.T) {
	logger, buf := testLogger(t)

	handler := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("tool registry not initialised")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	got := records(t, buf)
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0]["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", got[0]["level"])
	}
	if got[0]["panic"] != "tool registry not initialised" {
		t.Errorf("panic = %v, want the panic value", got[0]["panic"])
	}
	if stack, _ := got[0]["stack"].(string); !strings.Contains(stack, "middleware_test.go") {
		t.Error("stack trace missing or does not point at the panicking frame")
	}
}

// http.ErrAbortHandler is the stdlib's way of aborting a response; swallowing
// it would turn a deliberate abort into a bogus 500.
func TestRecovererRepanicsOnErrAbortHandler(t *testing.T) {
	logger, _ := testLogger(t)

	handler := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to propagate", rec)
		}
	}()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestHealthEndpoint(t *testing.T) {
	logger, _ := testLogger(t)

	rec := httptest.NewRecorder()
	routes(logger, 30*time.Second).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}
