package health_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/health"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// body : Decodes a liveness or readiness response.
type body struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// newRouter : Mounts the health endpoints over a database in the given
// state, and returns where they log.
func newRouter(t *testing.T, dbErr error) (chi.Router, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	logger, err := logging.New(buf, logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	r := chi.NewRouter()
	health.New(logger.Logger, apitest.Pinger{Err: dbErr}).Mount(r)
	return r, buf
}

// get : Issues a GET and returns the recorder.
func get(t *testing.T, r chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// decode : Parses a response body.
func decode(t *testing.T, rec *httptest.ResponseRecorder) body {
	t.Helper()
	var out body
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestLiveReportsOK(t *testing.T) {
	r, _ := newRouter(t, nil)

	rec := get(t, r, "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := decode(t, rec); got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
}

// Liveness must not depend on the database, or a database outage would have a
// supervisor restart a working server.
func TestLiveIgnoresDatabaseState(t *testing.T) {
	r, _ := newRouter(t, errors.New("down"))

	if rec := get(t, r, "/health"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even with the database down", rec.Code)
	}
}

func TestReadyReportsOKWhenDatabaseReachable(t *testing.T) {
	r, _ := newRouter(t, nil)

	rec := get(t, r, "/ready")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := decode(t, rec); got.Status != "ready" || got.Checks["database"] != "ok" {
		t.Errorf("body = %+v, want ready with database ok", got)
	}
}

// Readiness must fail when the database is gone, or a load balancer keeps
// sending traffic to a server that cannot answer it.
func TestReadyFailsWhenDatabaseUnreachable(t *testing.T) {
	r, buf := newRouter(t, errors.New("connection refused"))

	rec := get(t, r, "/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := decode(t, rec); got.Status != "not ready" || got.Checks["database"] != "unreachable" {
		t.Errorf("body = %+v, want 'not ready' with database unreachable", got)
	}

	// The response says only "unreachable"; the cause belongs in the log.
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("driver detail leaked into the response: %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "connection refused") {
		t.Errorf("cause not logged, so the failure is undiagnosable:\n%s", buf.String())
	}
}
