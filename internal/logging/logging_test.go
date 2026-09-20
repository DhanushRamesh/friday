package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// newTestLogger : Returns a logger writing JSON into buf.
func newTestLogger(t *testing.T, cfg logging.Config) (*logging.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger, err := logging.New(buf, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return logger, buf
}

// decode : Parses the single record written to buf.
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log record written")
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("expected one record, got:\n%s", line)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("unmarshal %q: %v", line, err)
	}
	return rec
}

func TestParseLevel(t *testing.T) {
	cases := map[string]struct {
		want    slog.Level
		wantErr bool
	}{
		"debug":   {want: slog.LevelDebug},
		"INFO":    {want: slog.LevelInfo},
		"":        {want: slog.LevelInfo},
		" warn ":  {want: slog.LevelWarn},
		"warning": {want: slog.LevelWarn},
		"error":   {want: slog.LevelError},
		"loud":    {wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := logging.ParseLevel(name)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) = %v, want error", name, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q): %v", name, err)
			}
			if got != tc.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", name, got, tc.want)
			}
		})
	}
}

func TestRedactsSensitiveKeys(t *testing.T) {
	logger, buf := newTestLogger(t, logging.Config{})

	logger.InfoContext(context.Background(), "auth attempt",
		"password", "hunter2",
		"authorization", "Bearer abc.def",
		"api_key", "sk-live-123",
		"user", "dhanush",
	)

	rec := decode(t, buf)
	for _, key := range []string{"password", "authorization", "api_key"} {
		if got := rec[key]; got != logging.Redacted {
			t.Errorf("%s = %v, want %v", key, got, logging.Redacted)
		}
	}
	if got := rec["user"]; got != "dhanush" {
		t.Errorf("user = %v, want dhanush (non-sensitive keys must survive)", got)
	}
	if strings.Contains(buf.String(), "hunter2") || strings.Contains(buf.String(), "sk-live-123") {
		t.Errorf("secret leaked into output: %s", buf.String())
	}
}

// Redaction matches keys exactly, so model accounting fields survive.
func TestDoesNotRedactTokenCounts(t *testing.T) {
	logger, buf := newTestLogger(t, logging.Config{})

	logger.InfoContext(context.Background(), "model call",
		"token_count", 1274,
		"tokens_used", 98,
		"token", "should-be-hidden",
	)

	rec := decode(t, buf)
	if got := rec["token_count"]; got != float64(1274) {
		t.Errorf("token_count = %v, want 1274", got)
	}
	if got := rec["tokens_used"]; got != float64(98) {
		t.Errorf("tokens_used = %v, want 98", got)
	}
	if got := rec["token"]; got != logging.Redacted {
		t.Errorf("token = %v, want %v", got, logging.Redacted)
	}
}

func TestRedactKeysExtension(t *testing.T) {
	logger, buf := newTestLogger(t, logging.Config{RedactKeys: []string{"Gitlab_PAT"}})

	logger.InfoContext(context.Background(), "tool call", "gitlab_pat", "glpat-xyz")

	if got := decode(t, buf)["gitlab_pat"]; got != logging.Redacted {
		t.Errorf("gitlab_pat = %v, want %v", got, logging.Redacted)
	}
}

func TestSecretNeverLeaks(t *testing.T) {
	s := logging.Secret("glpat-supersecret")

	if got := s.String(); got != logging.Redacted {
		t.Errorf("String() = %q, want %q", got, logging.Redacted)
	}
	if got := s.Reveal(); got != "glpat-supersecret" {
		t.Errorf("Reveal() = %q, want the underlying value", got)
	}

	logger, buf := newTestLogger(t, logging.Config{})
	// An innocuous key: only the Secret type can protect this one.
	logger.InfoContext(context.Background(), "node connect", "value", s)

	if strings.Contains(buf.String(), "supersecret") {
		t.Errorf("Secret leaked into output: %s", buf.String())
	}
}

func TestContextAttrsAppearOnEveryRecord(t *testing.T) {
	logger, buf := newTestLogger(t, logging.Config{})

	ctx := logging.WithAttrs(context.Background(), slog.String("request_id", "req-1"))
	ctx = logging.WithAttrs(ctx, slog.String("task_id", "task-1"))

	logger.InfoContext(ctx, "tool started")

	rec := decode(t, buf)
	if got := rec["request_id"]; got != "req-1" {
		t.Errorf("request_id = %v, want req-1", got)
	}
	if got := rec["task_id"]; got != "task-1" {
		t.Errorf("task_id = %v, want task-1", got)
	}
}

// Two contexts derived from the same parent must not see each other's
// attributes, which is what concurrent tasks sharing a request context do.
func TestContextAttrsDoNotBleedBetweenBranches(t *testing.T) {
	parent := logging.WithAttrs(context.Background(), slog.String("request_id", "req-1"))
	a := logging.WithAttrs(parent, slog.String("task_id", "task-a"))
	b := logging.WithAttrs(parent, slog.String("task_id", "task-b"))

	logger, buf := newTestLogger(t, logging.Config{})
	logger.InfoContext(a, "a")
	recA := decode(t, buf)

	buf.Reset()
	logger.InfoContext(b, "b")
	recB := decode(t, buf)

	if recA["task_id"] != "task-a" {
		t.Errorf("branch a task_id = %v, want task-a", recA["task_id"])
	}
	if recB["task_id"] != "task-b" {
		t.Errorf("branch b task_id = %v, want task-b", recB["task_id"])
	}
	if len(logging.AttrsFrom(parent)) != 1 {
		t.Errorf("parent context was mutated: %v", logging.AttrsFrom(parent))
	}
}

func TestSetLevelTakesEffectImmediately(t *testing.T) {
	logger, buf := newTestLogger(t, logging.Config{Level: "info"})
	ctx := context.Background()

	logger.DebugContext(ctx, "invisible")
	if buf.Len() != 0 {
		t.Fatalf("debug record emitted at info level: %s", buf.String())
	}

	logger.SetLevel(slog.LevelDebug)
	logger.DebugContext(ctx, "visible")
	if got := decode(t, buf)["msg"]; got != "visible" {
		t.Errorf("msg = %v, want visible", got)
	}
}

func TestServiceAttrsOnEveryRecord(t *testing.T) {
	logger, buf := newTestLogger(t, logging.Config{Service: "friday", Version: "v0.1.0", Env: "dev"})

	logger.InfoContext(context.Background(), "boot")

	rec := decode(t, buf)
	for key, want := range map[string]string{"service": "friday", "version": "v0.1.0", "env": "dev"} {
		if got := rec[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// Service attrs and context attrs must survive together: New applies the
// former through WithAttrs, which must not discard the context handler.
func TestServiceAttrsCoexistWithContextAttrs(t *testing.T) {
	logger, buf := newTestLogger(t, logging.Config{Service: "friday"})

	ctx := logging.WithAttrs(context.Background(), slog.String("task_id", "task-1"))
	logger.InfoContext(ctx, "agent step")

	rec := decode(t, buf)
	if rec["service"] != "friday" {
		t.Errorf("service = %v, want friday", rec["service"])
	}
	if rec["task_id"] != "task-1" {
		t.Errorf("task_id = %v, want task-1", rec["task_id"])
	}
}

func TestUnknownFormatRejected(t *testing.T) {
	if _, err := logging.New(&bytes.Buffer{}, logging.Config{Format: "xml"}); err == nil {
		t.Fatal("New with format xml: want error, got nil")
	}
}

func TestFromContextNeverNil(t *testing.T) {
	if logging.FromContext(context.Background()) == nil {
		t.Fatal("FromContext(background) = nil, want the default logger")
	}
	//lint:ignore SA1012 exercising the nil-context guard deliberately.
	if logging.FromContext(nil) == nil { //nolint:staticcheck
		t.Fatal("FromContext(nil) = nil, want the default logger")
	}

	custom, _ := logging.New(&bytes.Buffer{}, logging.Config{})
	ctx := logging.WithLogger(context.Background(), custom.Logger)
	if logging.FromContext(ctx) != custom.Logger {
		t.Error("FromContext did not return the logger stored by WithLogger")
	}
}
