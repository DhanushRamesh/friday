package logging

import (
	"context"
	"log/slog"
)

type ctxKey int

const (
	attrsKey ctxKey = iota
	loggerKey
)

// WithAttrs returns a context carrying attrs in addition to any already
// present. Every record logged with that context through a logger built by
// New carries them, so identifiers set once at the edge — request ID, task ID,
// user ID — appear on every downstream line without being threaded through
// each function signature.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	existing := AttrsFrom(ctx)
	// Build a fresh slice rather than appending in place: the parent context
	// may be shared across goroutines, and appending could write into backing
	// storage another goroutine is reading.
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, attrsKey, merged)
}

// AttrsFrom returns the attributes carried by ctx, or nil if there are none.
func AttrsFrom(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(attrsKey).([]slog.Attr)
	return attrs
}

// WithLogger returns a context carrying logger, for the uncommon case where a
// component needs a logger distinct from the default one.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext returns the logger carried by ctx, falling back to the default
// logger. It never returns nil.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
			return logger
		}
	}
	return slog.Default()
}
