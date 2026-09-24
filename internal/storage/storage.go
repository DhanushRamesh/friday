// Package storage : owns FRIDAY's database connection.
//
// It opens the connection, configures the pool, routes GORM's logging through
// internal/logging and exposes a reachability check. It contains no queries;
// repositories belong with the domain packages they serve.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	mysqldriver "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/config"
)

// DefaultSlowQueryThreshold : The duration past which a successful query is
// logged at warn rather than debug.
const DefaultSlowQueryThreshold = 200 * time.Millisecond

// DB : An open database handle wrapping a *gorm.DB.
type DB struct {
	*gorm.DB
	sqlDB *sql.DB
}

// Options : Settings for Open that are not part of user-facing configuration.
type Options struct {
	// SlowQueryThreshold : The duration past which a successful query is
	// logged at warn. Zero selects DefaultSlowQueryThreshold.
	SlowQueryThreshold time.Duration
	// LogStatements : Whether query logs include SQL text. Statements carry
	// interpolated parameter values, so this should be false in production.
	LogStatements bool
}

// Open : Connects to MySQL, configures the pool and verifies the connection
// before returning. The caller must Close the returned DB.
func Open(ctx context.Context, cfg config.Database, logger *slog.Logger, opts Options) (*DB, error) {
	if opts.SlowQueryThreshold <= 0 {
		opts.SlowQueryThreshold = DefaultSlowQueryThreshold
	}

	gormDB, err := gorm.Open(
		mysqldriver.New(mysqldriver.Config{DSN: cfg.DSN()}),
		&gorm.Config{
			Logger: newGormLogger(logger, opts.SlowQueryThreshold, opts.LogStatements),

			// UTC, matching the connection. GORM otherwise stamps its own
			// timestamps in the process's local time.
			NowFunc: func() time.Time { return time.Now().UTC() },

			// GORM otherwise wraps every write in its own transaction,
			// costing a round trip per statement.
			SkipDefaultTransaction: true,

			PrepareStmt: true,

			// Report driver errors as gorm.ErrDuplicatedKey and similar,
			// rather than as MySQL error numbers.
			TranslateError: true,
		},
	)
	if err != nil {
		// SafeAddr rather than the DSN, which holds the password.
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

	// gorm.Open can succeed without having reached the server.
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

// Ping : Reports whether the database is reachable.
func (d *DB) Ping(ctx context.Context) error {
	return d.sqlDB.PingContext(ctx)
}

// Stats : Returns connection pool statistics.
func (d *DB) Stats() sql.DBStats { return d.sqlDB.Stats() }

// Close : Releases the connection pool. It is safe to call on a nil DB.
func (d *DB) Close() error {
	if d == nil || d.sqlDB == nil {
		return nil
	}
	return d.sqlDB.Close()
}
