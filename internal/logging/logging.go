// Package logging : provides structured logging built on log/slog.
//
// It adds three things to a standard slog logger:
//
//   - attributes carried on a context.Context and attached to every record
//     logged with it, so an identifier set once at the edge of a request
//     appears on all subsequent records;
//   - redaction of attributes whose key or type marks them as a credential;
//   - a level that can be changed after construction.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Format : Selects the encoding of log records.
type Format string

// Supported output formats.
const (
	// FormatJSON : Emits one JSON object per record.
	FormatJSON Format = "json"
	// FormatText : Emits human-readable key=value pairs.
	FormatText Format = "text"
)

// Config : Describes how the logger should be constructed.
type Config struct {
	// Level : The minimum level to emit: debug, info, warn or error.
	// Defaults to info when empty.
	Level string
	// Format : Selects the output encoding. Defaults to FormatJSON when empty.
	Format Format
	// AddSource : Attaches the source file and line to every record, at the
	// cost of a stack walk per record.
	AddSource bool
	// Service : Names the process. It is attached to every record.
	Service string
	// Version : Identifies the build. It is attached to every record.
	Version string
	// Env : Names the deployment environment. It is attached to every record.
	Env string
	// RedactKeys : Names further attribute keys whose values are replaced with
	// Redacted, in addition to the built-in set. Matching is
	// case-insensitive.
	RedactKeys []string
}

// Logger : A *slog.Logger whose minimum level can be changed after
// construction. It is safe for concurrent use.
type Logger struct {
	*slog.Logger
	level *slog.LevelVar
}

// New : Returns a Logger writing records to w in the format given by cfg.
// It reports an error if cfg names an unknown level or format.
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

// SetLevel : Sets the minimum level, taking effect immediately for this Logger
// and every logger derived from it.
func (l *Logger) SetLevel(level slog.Level) { l.level.Set(level) }

// Level : Reports the current minimum level.
func (l *Logger) Level() slog.Level { return l.level.Level() }

// ParseLevel : Returns the slog.Level named by name, which may be "debug",
// "info", "warn", "warning" or "error" in any case. An empty name returns
// slog.LevelInfo.
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

// serviceAttrs : Returns the identifying attributes from cfg that are attached
// to every record.
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
