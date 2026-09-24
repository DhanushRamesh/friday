package storage_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// testDatabase : Returns the local development database settings, skipping the
// test when MySQL is not reachable so the suite still runs on a bare checkout.
func testDatabase(t *testing.T) config.Database {
	t.Helper()

	cfg, err := config.Load("", func(key string) (string, bool) {
		// Defaults already describe the local development database; only the
		// password differs between machines.
		if key == "ASSISTANT_DATABASE_PASSWORD" {
			return "friday_dev", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	db, err := storage.Open(ctx, cfg.Database, discard(), storage.Options{})
	if err != nil {
		t.Skipf("MySQL not reachable at %s, skipping: %v", cfg.Database.SafeAddr(), err)
	}
	_ = db.Close()

	return cfg.Database
}

func open(t *testing.T, cfg config.Database, logger *slog.Logger, opts storage.Options) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), cfg, logger, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenConnectsAndPings(t *testing.T) {
	db := open(t, testDatabase(t), discard(), storage.Options{})

	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	var got int
	if err := db.Raw("SELECT 1").Scan(&got).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != 1 {
		t.Errorf("SELECT 1 = %d, want 1", got)
	}
}

func TestPoolSettingsApplied(t *testing.T) {
	cfg := testDatabase(t)
	cfg.MaxOpenConns = 7
	cfg.MaxIdleConns = 3

	db := open(t, cfg, discard(), storage.Options{})

	if stats := db.Stats(); stats.MaxOpenConnections != 7 {
		t.Errorf("MaxOpenConnections = %d, want 7", stats.MaxOpenConnections)
	}
}

// The connection must run in UTC end to end. The development machine is in
// IST, so a mistake here is invisible locally until it reaches production.
func TestTimestampsAreUTC(t *testing.T) {
	db := open(t, testDatabase(t), discard(), storage.Options{})

	var tz string
	if err := db.Raw("SELECT @@session.time_zone").Scan(&tz).Error; err != nil {
		t.Fatalf("read session time_zone: %v", err)
	}
	if tz != "+00:00" {
		t.Errorf("session time_zone = %q, want +00:00", tz)
	}

	var now time.Time
	if err := db.Raw("SELECT NOW()").Scan(&now).Error; err != nil {
		t.Fatalf("scan NOW(): %v", err)
	}
	if now.Location() != time.UTC {
		t.Errorf("NOW() location = %v, want UTC", now.Location())
	}
	// GORM stamps its own timestamps; those must agree with the connection.
	if gormNow := db.NowFunc(); gormNow.Location() != time.UTC {
		t.Errorf("gorm NowFunc location = %v, want UTC", gormNow.Location())
	}
}

// A failure to connect must name the target without exposing the password.
func TestOpenFailsWithoutLeakingPassword(t *testing.T) {
	cfg := testDatabase(t)
	const wrong = "definitely-not-the-password"
	cfg.Password = wrong

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := storage.Open(ctx, cfg, discard(), storage.Options{})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open with a wrong password: want error, got nil")
	}
	if strings.Contains(err.Error(), wrong) {
		t.Errorf("error leaked the password: %v", err)
	}
	if !strings.Contains(err.Error(), cfg.SafeAddr()) {
		t.Errorf("error %q does not name the target %q", err, cfg.SafeAddr())
	}
}

// Open must not return a usable-looking handle when the server is unreachable;
// gorm.Open alone can succeed without having spoken to MySQL.
func TestOpenFailsFastOnUnreachableServer(t *testing.T) {
	cfg := testDatabase(t)
	cfg.Port = 3999 // nothing listening
	cfg.ConnectTimeout = 2 * time.Second

	start := time.Now()
	db, err := storage.Open(context.Background(), cfg, discard(), storage.Options{})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open against a dead port: want error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %v to fail; connect timeout is not being applied", elapsed)
	}
}

func TestCloseIsSafeOnNil(t *testing.T) {
	var db *storage.DB
	if err := db.Close(); err != nil {
		t.Errorf("Close on a nil DB: %v", err)
	}
}

// Queries must be logged through the injected logger, not GORM's own writer.
func TestQueriesLogThroughInjectedLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db := open(t, testDatabase(t), logger, storage.Options{LogStatements: true})

	var got int
	if err := db.Raw("SELECT 42").Scan(&got).Error; err != nil {
		t.Fatalf("query: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"msg":"query"`) {
		t.Errorf("query not logged through the injected logger:\n%s", out)
	}
	if !strings.Contains(out, "SELECT 42") {
		t.Errorf("statement not recorded with LogStatements=true:\n%s", out)
	}
}
