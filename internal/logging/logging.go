// Package logging provides structured logging for FRIDAY.
//
// It wraps log/slog with three things the stdlib does not give you:
//
//   - context-carried attributes, so a request or task ID set once in
//     middleware appears on every line logged downstream;
//   - redaction of credential-shaped values, so tokens and keys never
//     reach disk;
//   - a runtime-adjustable level, so verbosity can be raised on a running
//     server without a restart.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Format selects the encoding of log records.
type Format string

const (
	// FormatJSON emits one JSON object per record. Use in deployed environments.
	FormatJSON Format = "json"
	// FormatText emits human-readable key=value pairs. Use during development.
	FormatText Format = "text"
)

// Config describes how the logger should be constructed.
type Config struct {
	// Level is the minimum level to emit: debug, info, warn or error.
	// Defaults to info when empty.
	Level string
	// Format selects the output encoding. Defaults to FormatJSON when empty.
	Format Format
	// AddSource attaches the source file and line to every record. It costs a
	// stack walk per record, so it is normally enabled only in development.
	AddSource bool
	// Service, Version and Env are attached to every record so lines from
	// different processes remain distinguishable once aggregated.
	Service string
	Version string
	Env     string
	// RedactKeys extends the built-in set of attribute keys whose values are
	// replaced with a placeholder. Matching is case-insensitive.
	RedactKeys []string
}

// Logger is a *slog.Logger whose level can be changed after construction.
type Logger struct {
	*slog.Logger
	level *slog.LevelVar
}

// New builds a Logger writing to w.
//
// The returned Logger is safe for concurrent use.
func New(w io.Writer, cfg Config) (*Logger, error) {
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	lvar := new(slog.LevelVar)
	lvar.Set(level)

	opts := &slog.HandlerOptions{
		Level:       lvar,
		AddSource:   cfg.AddSource,
		ReplaceAttr: redactor(cfg.RedactKeys),
	}

	var base slog.Handler
	switch cfg.Format {
	case FormatText:
		base = slog.NewTextHandler(w, opts)
	case FormatJSON, "":
		base = slog.NewJSONHandler(w, opts)
	default:
		return nil, fmt.Errorf("logging: unknown format %q (want %q or %q)", cfg.Format, FormatJSON, FormatText)
	}

	logger := slog.New(&contextHandler{Handler: base})

	if attrs := serviceAttrs(cfg); len(attrs) > 0 {
		logger = slog.New(logger.Handler().WithAttrs(attrs))
	}

	return &Logger{Logger: logger, level: lvar}, nil
}

// SetLevel changes the minimum level. It takes effect immediately for every
// logger derived from this one.
func (l *Logger) SetLevel(level slog.Level) { l.level.Set(level) }

// Level reports the current minimum level.
func (l *Logger) Level() slog.Level { return l.level.Level() }

// ParseLevel converts a level name to a slog.Level. An empty name yields
// LevelInfo.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging: unknown level %q (want debug, info, warn or error)", name)
	}
}

func serviceAttrs(cfg Config) []slog.Attr {
	var attrs []slog.Attr
	if cfg.Service != "" {
		attrs = append(attrs, slog.String("service", cfg.Service))
	}
	if cfg.Version != "" {
		attrs = append(attrs, slog.String("version", cfg.Version))
	}
	if cfg.Env != "" {
		attrs = append(attrs, slog.String("env", cfg.Env))
	}
	return attrs
}
