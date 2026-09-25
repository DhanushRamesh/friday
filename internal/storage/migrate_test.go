package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/storage"
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

	for _, table := range []string{"chats", "conversations", "messages"} {
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
// which would make a chat's duration unmeasurable.
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
		               WHERE table_schema = DATABASE() AND table_name = 'chats' AND column_name = ?`, column).
			Scan(&got).Error
		if err != nil {
			t.Fatalf("reading %s: %v", column, err)
		}
		if got != want {
			t.Errorf("chats.%s is %s, want %s", column, got, want)
		}
	}
}

// Deleting a conversation must take its chats and its transcript with it, or they
// accumulate with nothing to belong to. Archiving and deleting both rely on
// this rather than removing the rows themselves.
func TestDeletingAConversationCascades(t *testing.T) {
	db := migrated(t)

	const user = "usr_01TESTCASCADE00000000000"
	const sess = "sess_01TESTCASCADE0000000000000"
	const id = "chat_01TESTCASCADE0000000000000"
	t.Cleanup(func() {
		db.Exec(`DELETE FROM conversations WHERE id = ?`, sess)
		db.Exec(`DELETE FROM users WHERE id = ?`, user)
	})

	if err := db.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at)
	                   VALUES (?, ?, 'x', NOW(3), NOW(3))`, user, user).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := db.Exec(`INSERT INTO conversations (id, user_id, created_at, updated_at)
	                   VALUES (?, ?, NOW(3), NOW(3))`, sess, user).Error; err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	if err := db.Exec(`INSERT INTO chats (id, conversation_id, prompt, status, created_at, updated_at)
	                   VALUES (?, ?, 'x', 'pending', NOW(3), NOW(3))`, id, sess).Error; err != nil {
		t.Fatalf("insert chat: %v", err)
	}
	if err := db.Exec(`INSERT INTO messages (id, conversation_id, seq, kind, role, content, created_at)
	                   VALUES (?, ?, 1, 'chat', 'user', 'x', NOW(3))`,
		"msg_01TESTCASCADE00000000000000"[:30], sess).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if err := db.Exec(`DELETE FROM conversations WHERE id = ?`, sess).Error; err != nil {
		t.Fatalf("delete conversation: %v", err)
	}

	for _, q := range []string{
		`SELECT COUNT(*) FROM chats WHERE conversation_id = ?`,
		`SELECT COUNT(*) FROM messages WHERE conversation_id = ?`,
	} {
		var remaining int
		if err := db.Raw(q, sess).Scan(&remaining).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if remaining != 0 {
			t.Errorf("%d rows outlived the conversation: %s", remaining, q)
		}
	}
}

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
