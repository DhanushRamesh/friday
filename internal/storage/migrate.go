package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pressly/goose/v3"
)

// migrationsFS : The schema, compiled into the binary so that deployment
// remains a single file with no directory of SQL to keep alongside it.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsDir : The path within migrationsFS holding the migration files.
const migrationsDir = "migrations"

const (
	// migrationLockName : The advisory lock held while migrating, so that two
	// processes starting together cannot run the same migration at once.
	//
	// MySQL does not roll back DDL, so a migration interrupted halfway leaves
	// the schema in a state no later run can repair: the second process finds
	// a table its own CREATE has not recorded, and fails for ever after.
	migrationLockName = "friday_schema_migration"

	// migrationLockTimeout : How long to wait for another process to finish
	// migrating before giving up.
	migrationLockTimeout = 30 * time.Second
)

// migrateMu : Serialises migration within one process. The advisory lock
// covers separate processes; this covers goose's package-level configuration,
// which two goroutines would otherwise set concurrently.
var migrateMu sync.Mutex

// Migrate : Applies every migration the database has not already run.
//
// It is safe to call on every start: goose records what it has applied and
// skips it. Applying migrations from a running process is correct while FRIDAY
// is a single process; more than one starting at once would need a lock so
// that they do not attempt the same migration together.
func Migrate(ctx context.Context, db *DB, logger *slog.Logger) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	release, err := lockForMigration(ctx, db)
	if err != nil {
		return err
	}
	defer release()

	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(&gooseLogger{logger: logger})
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("storage: selecting migration dialect: %w", err)
	}

	before, err := goose.GetDBVersionContext(ctx, db.sqlDB)
	if err != nil {
		return fmt.Errorf("storage: reading schema version: %w", err)
	}

	if err := goose.UpContext(ctx, db.sqlDB, migrationsDir); err != nil {
		return fmt.Errorf("storage: applying migrations: %w", err)
	}

	after, err := goose.GetDBVersionContext(ctx, db.sqlDB)
	if err != nil {
		return fmt.Errorf("storage: reading schema version: %w", err)
	}

	if before == after {
		logger.InfoContext(ctx, "schema up to date", slog.Int64("schema_version", after))
	} else {
		logger.InfoContext(ctx, "schema migrated",
			slog.Int64("schema_version_from", before),
			slog.Int64("schema_version_to", after))
	}
	return nil
}

// lockForMigration : Takes the advisory lock and returns the function that
// releases it.
//
// The lock is held on one dedicated connection, because MySQL scopes GET_LOCK
// to the connection that took it. Taking it from the pool would risk releasing
// it from a different connection, which does nothing.
func lockForMigration(ctx context.Context, db *DB) (func(), error) {
	conn, err := db.sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: reserving a connection to migrate: %w", err)
	}

	var acquired sql.NullInt64
	err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)",
		migrationLockName, int(migrationLockTimeout.Seconds())).Scan(&acquired)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("storage: taking the migration lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		conn.Close()
		return nil, fmt.Errorf("storage: another process held the migration lock for longer than %s",
			migrationLockTimeout)
	}

	return func() {
		// Released without the caller's context, which may already be done.
		_, _ = conn.ExecContext(context.WithoutCancel(ctx),
			"SELECT RELEASE_LOCK(?)", migrationLockName)
		conn.Close()
	}, nil
}

// SchemaVersion : Returns the number of the most recent migration applied.
func SchemaVersion(ctx context.Context, db *DB) (int64, error) {
	if err := goose.SetDialect("mysql"); err != nil {
		return 0, fmt.Errorf("storage: selecting migration dialect: %w", err)
	}
	version, err := goose.GetDBVersionContext(ctx, db.sqlDB)
	if err != nil {
		return 0, fmt.Errorf("storage: reading schema version: %w", err)
	}
	return version, nil
}

// gooseLogger : Adapts goose's logging onto log/slog, so that migration
// output carries the same structure as the rest of FRIDAY's logging rather
// than being printed to stdout in its own format.
type gooseLogger struct {
	logger *slog.Logger
}

// Printf : Records one of goose's messages.
func (l *gooseLogger) Printf(format string, args ...any) {
	l.logger.Info(gooseMessage(format, args...))
}

// Fatalf : Records one of goose's fatal messages.
//
// It does not exit, unlike the logger goose installs by default. Migration
// failures are returned by Migrate so that the caller decides what happens,
// rather than a library ending the process.
func (l *gooseLogger) Fatalf(format string, args ...any) {
	l.logger.Error(gooseMessage(format, args...))
}

// gooseMessage : Renders one of goose's messages for slog.
//
// goose ends its messages with a newline, which slog does not want, and
// already prefixes them with "goose: ", so adding another would double it.
func gooseMessage(format string, args ...any) string {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\r\n")
	if !strings.HasPrefix(msg, "goose: ") {
		msg = "goose: " + msg
	}
	return msg
}
