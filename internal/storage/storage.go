// Package storage owns FRIDAY's database connection.
//
// It opens and configures the connection pool, routes GORM's logging through
// internal/logging, and exposes a health check. It deliberately contains no
// queries: repositories live beside the domain packages they serve, so that
// this package stays the only place that knows how a connection is made.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	mysqldriver "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/DhanushRamesh/friday/internal/config"
)

// DefaultSlowQueryThreshold is the duration past which a successful query is
// still worth a warning. Every statement FRIDAY runs is a small indexed read
// or write, so anything slower than this points at a missing index rather
// than at honest work.
const DefaultSlowQueryThreshold = 200 * time.Millisecond

// DB is an open database handle.
type DB struct {
	*gorm.DB
	sqlDB *sql.DB
}

// Options tunes behaviour that does not belong in user-facing configuration.
type Options struct {
	// SlowQueryThreshold defaults to DefaultSlowQueryThreshold.
	SlowQueryThreshold time.Duration
	// LogStatements includes SQL text in query logs. Statements carry
	// interpolated parameters, which for FRIDAY means user messages and tool
	// output, so this should only be true in development.
	LogStatements bool
}

// Open connects to MySQL, configures the pool, and verifies the connection
// before returning. A returned DB is ready to use.
//
// The caller owns the handle and must Close it.
func Open(ctx context.Context, cfg config.Database, logger *slog.Logger, opts Options) (*DB, error) {
	if opts.SlowQueryThreshold <= 0 {
		opts.SlowQueryThreshold = DefaultSlowQueryThreshold
	}

	gormDB, err := gorm.Open(
		mysqldriver.New(mysqldriver.Config{DSN: cfg.DSN()}),
		&gorm.Config{
			Logger: newGormLogger(logger, opts.SlowQueryThreshold, opts.LogStatements),

			// GORM stamps timestamps itself. Without this it would use the
			// process's local time while the connection runs in UTC, so rows
			// written by FRIDAY and rows written by MySQL would disagree.
			NowFunc: func() time.Time { return time.Now().UTC() },

			// GORM wraps every single write in its own transaction by
			// default. FRIDAY manages transactions where it needs them, so
			// this only adds a round trip per statement.
			SkipDefaultTransaction: true,

			// Reuse prepared statements across a long-lived server.
			PrepareStmt: true,

			// Report driver errors as gorm.ErrDuplicatedKey and friends, so
			// callers can test for them without matching MySQL error numbers.
			TranslateError: true,
		},
	)
	if err != nil {
		// cfg.DSN() holds the password, so the target is described safely.
		return nil, fmt.Errorf("storage: connecting to %s: %w", cfg.SafeAddr(), err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("storage: obtaining connection pool: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	db := &DB{DB: gormDB, sqlDB: sqlDB}

	// gorm.Open can succeed without having reached the server. Fail here
	// rather than on the first request.
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := db.Ping(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage: connecting to %s: %w", cfg.SafeAddr(), err)
	}

	logger.InfoContext(ctx, "database connected",
		slog.String("target", cfg.SafeAddr()),
		slog.Int("max_open_conns", cfg.MaxOpenConns),
		slog.Int("max_idle_conns", cfg.MaxIdleConns),
		slog.Duration("conn_max_lifetime", cfg.ConnMaxLifetime),
	)

	return db, nil
}

// Ping verifies that the database is reachable. It backs the readiness check.
func (d *DB) Ping(ctx context.Context) error {
	return d.sqlDB.PingContext(ctx)
}

// Stats reports connection pool usage, for health output and diagnostics.
func (d *DB) Stats() sql.DBStats { return d.sqlDB.Stats() }

// Close releases the pool. It is safe to call on a partially opened DB.
func (d *DB) Close() error {
	if d == nil || d.sqlDB == nil {
		return nil
	}
	return d.sqlDB.Close()
}
