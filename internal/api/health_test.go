package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// health : Decodes a health or readiness response body.
type health struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// get : Issues a GET against the server and returns the recorder.
func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthReportsOK(t *testing.T) {
	s, _ := newTestServer(t, stubPinger{})

	rec := get(t, s, "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

// Liveness must not depend on the database, or a database outage would have a
// supervisor restart a working server.
func TestHealthIgnoresDatabaseState(t *testing.T) {
	s, _ := newTestServer(t, stubPinger{err: errors.New("down")})

	if rec := get(t, s, "/health"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even with the database down", rec.Code)
	}
}

func TestReadyReportsOKWhenDatabaseReachable(t *testing.T) {
	s, _ := newTestServer(t, stubPinger{})

	rec := get(t, s, "/ready")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Status != "ready" || body.Checks["database"] != "ok" {
		t.Errorf("body = %+v, want ready with database ok", body)
	}
}

// Readiness must fail when the database is gone, or a load balancer keeps
// sending traffic to a server that cannot answer it.
func TestReadyFailsWhenDatabaseUnreachable(t *testing.T) {
	s, buf := newTestServer(t, stubPinger{err: errors.New("connection refused")})

	rec := get(t, s, "/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var body health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Status != "not ready" || body.Checks["database"] != "unreachable" {
		t.Errorf("body = %+v, want 'not ready' with database unreachable", body)
	}

	// The response says only "unreachable"; the cause belongs in the log.
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("driver detail leaked into the response: %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "connection refused") {
		t.Errorf("cause not logged, so the failure is undiagnosable:\n%s", buf.String())
	}
}
