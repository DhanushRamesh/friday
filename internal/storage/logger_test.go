package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func captureLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func lastRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("no log record written")
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return rec
}

// trace : Invokes the GORM hook the way GORM itself would.
func trace(l *gormLogger, elapsed time.Duration, sql string, rows int64, err error) {
	l.Trace(context.Background(), time.Now().Add(-elapsed), func() (string, int64) {
		return sql, rows
	}, err)
}

func TestSuccessfulQueryLogsAtDebug(t *testing.T) {
	logger, buf := captureLogger(t)
	trace(newGormLogger(logger, time.Second, true), time.Millisecond, "SELECT 1", 1, nil)

	rec := lastRecord(t, buf)
	if rec["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", rec["level"])
	}
	if rec["msg"] != "query" {
		t.Errorf("msg = %v, want query", rec["msg"])
	}
	if rec["rows"] != float64(1) {
		t.Errorf("rows = %v, want 1", rec["rows"])
	}
}

func TestSlowQueryLogsAtWarn(t *testing.T) {
	logger, buf := captureLogger(t)
	trace(newGormLogger(logger, 50*time.Millisecond, true), 200*time.Millisecond, "SELECT SLEEP(1)", 1, nil)

	rec := lastRecord(t, buf)
	if rec["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", rec["level"])
	}
	if rec["msg"] != "slow query" {
		t.Errorf("msg = %v, want 'slow query'", rec["msg"])
	}
	if rec["slow_threshold"] == nil {
		t.Error("slow_threshold not reported, so the reader cannot tell what was exceeded")
	}
}

func TestFailedQueryLogsAtError(t *testing.T) {
	logger, buf := captureLogger(t)
	trace(newGormLogger(logger, time.Second, true), time.Millisecond, "SELECT bad", 0, errors.New("unknown column"))

	rec := lastRecord(t, buf)
	if rec["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", rec["level"])
	}
	if rec["error"] != "unknown column" {
		t.Errorf("error = %v, want the driver message", rec["error"])
	}
}

// A missing row is an ordinary outcome that callers handle. Logging it as an
// error would fill the log with noise from every lookup that finds nothing.
func TestRecordNotFoundIsNotAnError(t *testing.T) {
	logger, buf := captureLogger(t)
	trace(newGormLogger(logger, time.Second, true), time.Millisecond, "SELECT ...", 0, gorm.ErrRecordNotFound)

	if rec := lastRecord(t, buf); rec["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG for a missing row", rec["level"])
	}
}

// Statements carry interpolated parameters, which for FRIDAY means user
// messages and tool output. They must not appear unless explicitly enabled.
func TestStatementsOmittedUnlessEnabled(t *testing.T) {
	const statement = `INSERT INTO tasks (input) VALUES ('private user message')`

	logger, buf := captureLogger(t)
	trace(newGormLogger(logger, time.Second, false), time.Millisecond, statement, 1, nil)

	if strings.Contains(buf.String(), "private user message") {
		t.Errorf("statement leaked with logStatements=false: %s", buf.String())
	}
	if lastRecord(t, buf)["sql"] != nil {
		t.Error("sql attribute present with logStatements=false")
	}

	logger, buf = captureLogger(t)
	trace(newGormLogger(logger, time.Second, true), time.Millisecond, statement, 1, nil)

	if lastRecord(t, buf)["sql"] != statement {
		t.Error("sql attribute missing with logStatements=true")
	}
}

// An error must still be reported even when statements are hidden, otherwise
// production would log failures with no indication of what failed.
func TestErrorsReportedEvenWithStatementsHidden(t *testing.T) {
	logger, buf := captureLogger(t)
	trace(newGormLogger(logger, time.Second, false), time.Millisecond, "SELECT secret", 0, errors.New("boom"))

	rec := lastRecord(t, buf)
	if rec["level"] != "ERROR" || rec["error"] != "boom" {
		t.Errorf("record = %v, want an ERROR carrying the message", rec)
	}
}

func TestGormMessagesRouteThroughSlog(t *testing.T) {
	logger, buf := captureLogger(t)
	l := newGormLogger(logger, time.Second, true)
	ctx := context.Background()

	l.Info(ctx, "replacing callback %s", "gorm:create")
	if rec := lastRecord(t, buf); rec["msg"] != "gorm: replacing callback gorm:create" {
		t.Errorf("msg = %v, want the formatted GORM message", rec["msg"])
	}

	l.Warn(ctx, "plain message")
	if rec := lastRecord(t, buf); rec["level"] != "WARN" || rec["msg"] != "gorm: plain message" {
		t.Errorf("record = %v, want a WARN carrying the message", rec)
	}

	l.Error(ctx, "bad thing")
	if rec := lastRecord(t, buf); rec["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", rec["level"])
	}
}
