package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// stubPinger : Stands in for a database handle in tests.
type stubPinger struct{ err error }

// Ping : Returns the configured error.
func (s stubPinger) Ping(context.Context) error { return s.err }

// newTestServer : Builds a Server writing its logs into the returned buffer.
func newTestServer(t *testing.T, db Pinger) (*Server, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger, err := logging.New(buf, logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	return New(Options{Logger: logger.Logger, DB: db}), buf
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
	s := New(Options{Logger: slog.Default(), DB: stubPinger{}})
	if s.requestTimeout != DefaultRequestTimeout {
		t.Errorf("requestTimeout = %v, want %v", s.requestTimeout, DefaultRequestTimeout)
	}
}
