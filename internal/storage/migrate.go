package storage

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pressly/goose/v3"
)

// migrationsFS : The schema, compiled into the binary so that deployment
// remains a single file with no directory of SQL to keep alongside it.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsDir : The path within migrationsFS holding the migration files.
const migrationsDir = "migrations"

// Migrate : Applies every migration the database has not already run.
//
// It is safe to call on every start: goose records what it has applied and
// skips it. Applying migrations from a running process is correct while FRIDAY
// is a single process; more than one starting at once would need a lock so
// that they do not attempt the same migration together.
func Migrate(ctx context.Context, db *DB, logger *slog.Logger) error {
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
