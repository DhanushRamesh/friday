package logging

import (
	"context"
	"log/slog"
)

// ctxKey : The unexported key type for values this package stores in a
// context, preventing collision with keys from other packages.
type ctxKey int

const (
	// attrsKey : Addresses the []slog.Attr carried by a context.
	attrsKey ctxKey = iota
	// loggerKey addresses the *slog.Logger carried by a context.
	loggerKey
)

// WithAttrs : Returns a copy of ctx carrying attrs in addition to any already
// present. A logger built by New attaches them to every record logged with
// that context.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	existing := AttrsFrom(ctx)
	// A fresh slice, because appending to the parent's could write into
	// backing storage another goroutine is reading.
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, attrsKey, merged)
}

// AttrsFrom : Returns the attributes carried by ctx, or nil if there are none.
func AttrsFrom(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(attrsKey).([]slog.Attr)
	return attrs
}

// WithLogger : Returns a copy of ctx carrying logger. A nil logger leaves ctx
// unchanged.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext : Returns the logger carried by ctx, or the default logger if ctx
// carries none. It never returns nil.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
			return logger
		}
	}
	return slog.Default()
}
