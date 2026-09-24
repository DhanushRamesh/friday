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

// gormLogger : Adapts gorm.io/gorm/logger.Interface onto log/slog, so that
// GORM's output carries the same structure, context attributes and redaction
// as the rest of the server's logging.
type gormLogger struct {
	logger *slog.Logger
	// slowThreshold : Marks a query as worth noticing. Anything slower is
	// logged at warn even when it succeeds.
	slowThreshold time.Duration
	// logStatements : Includes the SQL text in each record. Statements carry
	// interpolated parameter values, which for the server means user messages and
	// tool output, so this stays off outside development.
	logStatements bool
}

// newGormLogger : Returns a gormLogger writing to logger.
func newGormLogger(logger *slog.Logger, slowThreshold time.Duration, logStatements bool) *gormLogger {
	return &gormLogger{
		logger:        logger,
		slowThreshold: slowThreshold,
		logStatements: logStatements,
	}
}

// LogMode : Implements logger.Interface. The requested level is ignored;
// verbosity follows the slog level.
func (l *gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

// Info : Records one of GORM's own informational messages.
func (l *gormLogger) Info(ctx context.Context, msg string, args ...any) {
	l.logger.InfoContext(ctx, "gorm: "+sprintf(msg, args))
}

// Warn : Records one of GORM's own warnings.
func (l *gormLogger) Warn(ctx context.Context, msg string, args ...any) {
	l.logger.WarnContext(ctx, "gorm: "+sprintf(msg, args))
}

// Error : Records one of GORM's own errors.
func (l *gormLogger) Error(ctx context.Context, msg string, args ...any) {
	l.logger.ErrorContext(ctx, "gorm: "+sprintf(msg, args))
}

// Trace : Records the outcome of one statement. GORM calls it after every
// query.
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)

	level := slog.LevelDebug
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		level = slog.LevelError
	case elapsed > l.slowThreshold:
		level = slog.LevelWarn
	}

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

// sprintf : Formats msg with args, returning msg unchanged when there are
// none.
func sprintf(msg string, args []any) string {
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
