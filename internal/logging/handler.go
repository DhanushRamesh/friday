package logging

import (
	"context"
	"log/slog"
)

// contextHandler : Wraps a slog.Handler and adds the attributes carried by the
// context to each record it handles.
//
// The context reaches a handler only through the context-taking log methods,
// so callers should prefer InfoContext over Info.
type contextHandler struct {
	slog.Handler
}

// Handle : Adds any attributes carried by ctx to rec and passes it to the
// wrapped handler.
func (h *contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if attrs := AttrsFrom(ctx); len(attrs) > 0 {
		// Clone first: AddAttrs can write into backing storage shared with
		// records held elsewhere.
		rec = rec.Clone()
		rec.AddAttrs(attrs...)
	}
	return h.Handler.Handle(ctx, rec)
}

// WithAttrs : Returns a handler that also records attrs, preserving context
// attribute injection.
func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup : Returns a handler that qualifies subsequent attributes with name,
// preserving context attribute injection.
func (h *contextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}
