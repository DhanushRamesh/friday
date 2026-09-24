package middleware_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/DhanushRamesh/friday/internal/api/apitest"
	"github.com/DhanushRamesh/friday/internal/api/middleware"
	"github.com/DhanushRamesh/friday/internal/logging"
)

// newRouter : Builds a router carrying the whole middleware stack in the
// order the server applies it, and returns where it logs.
//
// Built here rather than taken from a running server so that these tests
// cover the middleware alone: a failure means the middleware is wrong, not
// that something was mounted behind it wrongly.
func newRouter(t *testing.T) (chi.Router, *slog.Logger, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	logger, err := logging.New(buf, logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(middleware.RequestContext)
	r.Use(middleware.RequestLogger(logger.Logger))
	r.Use(middleware.Recoverer(logger.Logger))
	return r, logger.Logger, buf
}

// A handler logging through the request context must produce the same
// request_id as the middleware's own request record.
func TestRequestIDReachesHandlerLogs(t *testing.T) {
	r, logger, buf := newRouter(t)
	r.Get("/probe", func(w http.ResponseWriter, req *http.Request) {
		logger.InfoContext(req.Context(), "handler work")
		w.WriteHeader(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	got := apitest.Records(t, buf)
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

// Alerting keys on the level alone, so the level must follow the status.
func TestRequestLoggerLevelsByStatus(t *testing.T) {
	cases := []struct {
		path      string
		status    int
		wantLevel string
	}{
		{"/probe", http.StatusOK, "INFO"},
		{"/probe", http.StatusNotFound, "WARN"},
		{"/probe", http.StatusInternalServerError, "ERROR"},
		// Health checks are polled continuously; at info they would crowd out
		// real traffic.
		{"/health", http.StatusOK, "DEBUG"},
	}

	for _, tc := range cases {
		t.Run(tc.path+"/"+http.StatusText(tc.status), func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger, err := logging.New(buf, logging.Config{Level: "debug"})
			if err != nil {
				t.Fatalf("logging.New: %v", err)
			}

			handler := middleware.RequestLogger(logger.Logger)(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tc.path, nil))

			got := apitest.Records(t, buf)
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
	r, _, buf := newRouter(t)
	r.Get("/boom", func(w http.ResponseWriter, req *http.Request) {
		panic("tool registry not initialised")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var panicRec map[string]any
	for _, record := range apitest.Records(t, buf) {
		if record["msg"] == "panic recovered" {
			panicRec = record
		}
	}
	if panicRec == nil {
		t.Fatalf("no panic record written:\n%s", buf.String())
	}
	if panicRec["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", panicRec["level"])
	}
	if panicRec["panic"] != "tool registry not initialised" {
		t.Errorf("panic = %v, want the panic value", panicRec["panic"])
	}
	if stack, _ := panicRec["stack"].(string); !strings.Contains(stack, "middleware_test.go") {
		t.Error("stack trace missing or does not point at the panicking frame")
	}
}

// http.ErrAbortHandler is net/http's way of aborting a response; swallowing it
// would turn a deliberate abort into a spurious 500.
func TestRecovererRepanicsOnErrAbortHandler(t *testing.T) {
	buf := &bytes.Buffer{}
	logger, err := logging.New(buf, logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	handler := middleware.Recoverer(logger.Logger)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) }))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to propagate", rec)
		}
	}()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// The panic value must not reach the client.
func TestRecovererDoesNotLeakPanicToClient(t *testing.T) {
	r, _, _ := newRouter(t)
	r.Get("/boom", func(w http.ResponseWriter, req *http.Request) {
		panic(errors.New("dsn=user:password@tcp(host)/db"))
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("panic detail leaked to the client: %s", rec.Body.String())
	}
}
