package logging

import (
	"context"
	"log/slog"
)

// contextHandler injects attributes carried by the context into every record.
//
// It only sees the context for calls that pass one, so prefer the
// context-taking methods — logger.InfoContext(ctx, ...) over logger.Info(...).
type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if attrs := AttrsFrom(ctx); len(attrs) > 0 {
		// Clone before mutating: the caller owns rec, and AddAttrs can write
		// into backing storage shared with records held elsewhere.
		rec = rec.Clone()
		rec.AddAttrs(attrs...)
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}
