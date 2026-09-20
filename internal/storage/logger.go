package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// gormLogger adapts GORM's logging interface onto log/slog.
//
// GORM's own logger writes its own format to stdout, which would bypass
// structured logging, the request identifiers carried on the context, and the
// credential redaction in internal/logging. Everything GORM has to say goes
// through the same pipeline as the rest of FRIDAY instead.
type gormLogger struct {
	logger *slog.Logger
	// slowThreshold marks a query as worth noticing. Anything slower is
	// logged at warn even when it succeeds.
	slowThreshold time.Duration
	// logStatements includes the SQL text in each record. Statements carry
	// interpolated parameter values, which for FRIDAY means user messages and
	// tool output, so this stays off outside development.
	logStatements bool
}

func newGormLogger(logger *slog.Logger, slowThreshold time.Duration, logStatements bool) *gormLogger {
	return &gormLogger{
		logger:        logger,
		slowThreshold: slowThreshold,
		logStatements: logStatements,
	}
}

// LogMode satisfies GORM's interface. Verbosity is controlled by the slog
// level instead, so the request is ignored.
func (l *gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

func (l *gormLogger) Info(ctx context.Context, msg string, args ...any) {
	l.logger.InfoContext(ctx, "gorm: "+sprintf(msg, args))
}

func (l *gormLogger) Warn(ctx context.Context, msg string, args ...any) {
	l.logger.WarnContext(ctx, "gorm: "+sprintf(msg, args))
}

func (l *gormLogger) Error(ctx context.Context, msg string, args ...any) {
	l.logger.ErrorContext(ctx, "gorm: "+sprintf(msg, args))
}

// Trace is called once per statement with its duration and outcome.
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)

	level := slog.LevelDebug
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		level = slog.LevelError
	case elapsed > l.slowThreshold:
		level = slog.LevelWarn
	}

	// A missing row is an ordinary outcome that callers handle, not a fault.
	if !l.logger.Enabled(ctx, level) {
		return
	}

	sql, rows := fc()

	attrs := []slog.Attr{
		slog.Int64("rows", rows),
		slog.Int64("duration_ms", elapsed.Milliseconds()),
	}
	if l.logStatements {
		attrs = append(attrs, slog.String("sql", sql))
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	if elapsed > l.slowThreshold {
		attrs = append(attrs, slog.Duration("slow_threshold", l.slowThreshold))
	}

	msg := "query"
	switch {
	case level == slog.LevelError:
		msg = "query failed"
	case elapsed > l.slowThreshold:
		msg = "slow query"
	}

	l.logger.LogAttrs(ctx, level, msg, attrs...)
}

// sprintf formats GORM's message only when it carries arguments, avoiding a
// needless allocation for the common case of a plain string.
func sprintf(msg string, args []any) string {
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
