package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/DhanushRamesh/friday/internal/storage"
)

// migrated : Opens the test database and brings its schema up to date.
func migrated(t *testing.T) *storage.DB {
	t.Helper()
	db := open(t, testDatabase(t), discard(), storage.Options{})
	if err := storage.Migrate(context.Background(), db, discard()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestMigrateCreatesTheSchema(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()

	for _, table := range []string{"tasks", "task_messages"} {
		var count int
		err := db.Raw(`SELECT COUNT(*) FROM information_schema.tables
		               WHERE table_schema = DATABASE() AND table_name = ?`, table).
			Scan(&count).Error
		if err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s does not exist after migrating", table)
		}
	}

	version, err := storage.SchemaVersion(ctx, db)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version < 2 {
		t.Errorf("schema version = %d, want at least 2", version)
	}
}

// Migrate runs on every start, so running it again must do nothing rather
// than fail or reapply.
func TestMigrateIsRepeatable(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()

	before, err := storage.SchemaVersion(ctx, db)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := storage.Migrate(ctx, db, discard()); err != nil {
			t.Fatalf("Migrate call %d: %v", i+2, err)
		}
	}

	after, err := storage.SchemaVersion(ctx, db)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if before != after {
		t.Errorf("schema version moved from %d to %d on a repeat run", before, after)
	}
}

// Timestamps must keep milliseconds. Plain DATETIME truncates to the second,
// which would make a task's duration unmeasurable.
func TestTimestampColumnsKeepMilliseconds(t *testing.T) {
	db := migrated(t)

	columns := map[string]string{
		"created_at":  "datetime(3)",
		"updated_at":  "datetime(3)",
		"started_at":  "datetime(3)",
		"finished_at": "datetime(3)",
	}
	for column, want := range columns {
		var got string
		err := db.Raw(`SELECT column_type FROM information_schema.columns
		               WHERE table_schema = DATABASE() AND table_name = 'tasks' AND column_name = ?`, column).
			Scan(&got).Error
		if err != nil {
			t.Fatalf("reading %s: %v", column, err)
		}
		if got != want {
			t.Errorf("tasks.%s is %s, want %s", column, got, want)
		}
	}
}

// Deleting a task must take its messages with it, or they accumulate with no
// task to belong to.
func TestDeletingATaskRemovesItsMessages(t *testing.T) {
	db := migrated(t)

	const id = "task_01TESTCASCADE0000000000000"
	t.Cleanup(func() { db.Exec(`DELETE FROM tasks WHERE id = ?`, id) })

	if err := db.Exec(`INSERT INTO tasks (id, prompt, status, created_at, updated_at)
	                   VALUES (?, 'x', 'pending', NOW(3), NOW(3))`, id).Error; err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if err := db.Exec("INSERT INTO task_messages (task_id, seq, kind, `text`, created_at)\n"+
		"VALUES (?, 1, 'update', 'working', NOW(3))", id).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if err := db.Exec(`DELETE FROM tasks WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("delete task: %v", err)
	}

	var remaining int
	if err := db.Raw(`SELECT COUNT(*) FROM task_messages WHERE task_id = ?`, id).Scan(&remaining).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d messages left behind after the task was deleted", remaining)
	}
}

// A message cannot belong to a task that does not exist.
func TestMessageRequiresAnExistingTask(t *testing.T) {
	db := migrated(t)

	err := db.Exec("INSERT INTO task_messages (task_id, seq, kind, `text`, created_at)\n" +
		"VALUES ('task_01NOSUCHTASK00000000000000', 1, 'update', 'orphan', NOW(3))").Error
	if err == nil {
		db.Exec(`DELETE FROM task_messages WHERE task_id = 'task_01NOSUCHTASK00000000000000'`)
		t.Fatal("inserting a message for a missing task succeeded, want a foreign key error")
	}
}

// goose prefixes its own messages and ends them with a newline; neither should
// reach the log as written.
func TestGooseOutputIsLoggedCleanly(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db := open(t, testDatabase(t), discard(), storage.Options{})
	if err := storage.Migrate(context.Background(), db, logger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		msg, _ := rec["msg"].(string)
		if strings.Contains(msg, "goose: goose:") {
			t.Errorf("doubled prefix in %q", msg)
		}
		if strings.HasSuffix(msg, "\n") {
			t.Errorf("trailing newline in %q", msg)
		}
	}
}
