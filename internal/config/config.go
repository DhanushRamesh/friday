// Package config loads and validates FRIDAY's runtime configuration.
//
// Values are resolved from three layers, each overriding the one before it:
//
//	built-in defaults  <  config.ini  <  environment variables
//
// The file is for settings you edit by hand; the environment is for whatever
// differs per deployment, which in practice means secrets. Every key has an
// environment equivalent named FRIDAY_<SECTION>_<KEY>, so nothing in the file
// is impossible to override without editing it.
//
// Load is a pure function over a file path and a lookup callback rather than a
// reader of os.Getenv, so configuration logic can be tested without mutating
// process state.
//
// Validation collects every problem before returning. A server that reports
// one bad setting per restart wastes more time than one that reports all of
// them at once.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"gopkg.in/ini.v1"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// DefaultPath is the configuration file used when none is named. It is
// optional: FRIDAY starts on defaults and environment variables without it.
const DefaultPath = "config.ini"

// PathEnvVar names the environment variable holding an explicit config path.
// When it is set the file must exist, because a typo there should not be
// silently ignored.
const PathEnvVar = "FRIDAY_CONFIG"

// Environment names a deployment context. Validation is stricter outside dev.
type Environment string

const (
	EnvDev        Environment = "dev"
	EnvProduction Environment = "production"
)

// IsProduction reports whether defaults suitable only for a developer machine
// should be rejected.
func (e Environment) IsProduction() bool { return e == EnvProduction }

// Config is the fully resolved configuration for one process.
type Config struct {
	Env      Environment
	Server   Server
	Log      Log
	Database Database

	// Source records where the file-backed values came from, for logging at
	// startup. It is empty when no file was read.
	Source string
}

// Server configures the HTTP listener.
type Server struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	RequestTimeout    time.Duration
}

// Log configures the structured logger.
type Log struct {
	Level     string
	Format    logging.Format
	AddSource bool
}

// Database configures the MySQL connection.
//
// Connection details are held as components rather than one string so the
// password can carry the Secret type, which cannot be logged or printed.
type Database struct {
	Host     string
	Port     int
	User     string
	Password logging.Secret
	Name     string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnectTimeout  time.Duration
}

// DSN returns the connection string for go-sql-driver/mysql.
//
// The result contains the password and must never be logged. Use SafeAddr for
// anything human-facing.
func (d Database) DSN() string {
	c := mysql.NewConfig()
	c.Net = "tcp"
	c.Addr = fmt.Sprintf("%s:%d", d.Host, d.Port)
	c.User = d.User
	c.Passwd = d.Password.Reveal()
	c.DBName = d.Name
	// Without ParseTime, DATETIME columns scan as []byte rather than time.Time.
	c.ParseTime = true
	// Read and write timestamps as UTC so a change to the server timezone
	// cannot silently reinterpret rows already stored.
	c.Loc = time.UTC
	c.Timeout = d.ConnectTimeout
	c.Params = map[string]string{"time_zone": "'+00:00'"}
	return c.FormatDSN()
}

// SafeAddr describes the connection target without the password, for logs.
func (d Database) SafeAddr() string {
	return fmt.Sprintf("%s@%s:%d/%s", d.User, d.Host, d.Port, d.Name)
}

// LogValue implements slog.LogValuer so a whole Config can be logged without
// leaking the database password.
func (c Config) LogValue() slog.Value {
	source := c.Source
	if source == "" {
		source = "(defaults and environment only)"
	}
	return slog.GroupValue(
		slog.String("source", source),
		slog.String("env", string(c.Env)),
		slog.String("server.addr", c.Server.Addr),
		slog.String("log.level", c.Log.Level),
		slog.String("log.format", string(c.Log.Format)),
		slog.String("database.addr", c.Database.SafeAddr()),
		slog.Int("database.max_open_conns", c.Database.MaxOpenConns),
	)
}

// Lookup reports the value of an environment variable and whether it was set,
// matching the signature of os.LookupEnv.
type Lookup func(key string) (string, bool)

// LoadFromEnv resolves configuration using the process environment, reading
// the file named by FRIDAY_CONFIG, or config.ini when that is unset.
//
// A missing config.ini is not an error; a missing FRIDAY_CONFIG target is.
func LoadFromEnv() (Config, error) {
	path, explicit := os.LookupEnv(PathEnvVar)
	path = strings.TrimSpace(path)
	if path == "" {
		explicit = false
		path = DefaultPath
	}

	if !explicit {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			// No file on disk: defaults and environment are enough to start.
			path = ""
		}
	}
	return Load(path, os.LookupEnv)
}

// Load resolves configuration from the file at path, overridden by lookup.
// An empty path skips the file entirely.
func Load(path string, lookup Lookup) (Config, error) {
	l := &loader{lookup: lookup, known: map[fieldKey]bool{}}

	if path != "" {
		file, err := ini.LoadSources(ini.LoadOptions{SkipUnrecognizableLines: false}, path)
		if err != nil {
			return Config{}, fmt.Errorf("config: reading %s: %w", path, err)
		}
		l.file = file
	}

	env := Environment(l.str("", "env", string(EnvDev)))
	if env != EnvDev && env != EnvProduction {
		l.errorf("%s: %q is not a known environment (want %q or %q)",
			l.where("", "env"), env, EnvDev, EnvProduction)
	}

	// Developer machines get readable logs with source locations; anything
	// else gets JSON for aggregation.
	defaultFormat, defaultSource := string(logging.FormatJSON), false
	if env == EnvDev {
		defaultFormat, defaultSource = string(logging.FormatText), true
	}

	cfg := Config{
		Env:    env,
		Source: path,
		Server: Server{
			Addr:              l.str("server", "addr", ":8080"),
			ReadHeaderTimeout: l.duration("server", "read_header_timeout", 5*time.Second),
			IdleTimeout:       l.duration("server", "idle_timeout", 60*time.Second),
			ShutdownTimeout:   l.duration("server", "shutdown_timeout", 15*time.Second),
			RequestTimeout:    l.duration("server", "request_timeout", 30*time.Second),
		},
		Log: Log{
			Level:     l.str("log", "level", "info"),
			Format:    logging.Format(l.str("log", "format", defaultFormat)),
			AddSource: l.boolean("log", "source", defaultSource),
		},
		Database: Database{
			Host:            l.str("database", "host", "127.0.0.1"),
			Port:            l.integer("database", "port", 3306),
			User:            l.str("database", "user", "friday"),
			Password:        logging.Secret(l.str("database", "password", "")),
			Name:            l.str("database", "name", "friday"),
			MaxOpenConns:    l.integer("database", "max_open_conns", 25),
			MaxIdleConns:    l.integer("database", "max_idle_conns", 5),
			ConnMaxLifetime: l.duration("database", "conn_max_lifetime", 5*time.Minute),
			ConnectTimeout:  l.duration("database", "connect_timeout", 5*time.Second),
		},
	}

	l.rejectUnknownKeys(path)
	l.validate(cfg)

	if err := l.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (l *loader) validate(cfg Config) {
	if _, err := logging.ParseLevel(cfg.Log.Level); err != nil {
		l.errorf("%s: %v", l.where("log", "level"), err)
	}
	if f := cfg.Log.Format; f != logging.FormatJSON && f != logging.FormatText {
		l.errorf("%s: %q is not a known format (want %q or %q)",
			l.where("log", "format"), f, logging.FormatJSON, logging.FormatText)
	}
	if cfg.Server.Addr == "" {
		l.errorf("%s: must not be empty", l.where("server", "addr"))
	}
	if cfg.Database.Host == "" {
		l.errorf("%s: must not be empty", l.where("database", "host"))
	}
	if cfg.Database.Port < 1 || cfg.Database.Port > 65535 {
		l.errorf("%s: %d is not a valid port", l.where("database", "port"), cfg.Database.Port)
	}
	if cfg.Database.User == "" {
		l.errorf("%s: must not be empty", l.where("database", "user"))
	}
	if cfg.Database.Name == "" {
		l.errorf("%s: must not be empty", l.where("database", "name"))
	}
	if cfg.Database.MaxOpenConns < 1 {
		l.errorf("%s: must be at least 1", l.where("database", "max_open_conns"))
	}
	if cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
		l.errorf("%s: %d exceeds max_open_conns (%d)",
			l.where("database", "max_idle_conns"), cfg.Database.MaxIdleConns, cfg.Database.MaxOpenConns)
	}

	// A blank database password is normal on a developer machine using local
	// trust auth, and never acceptable on a deployed one.
	if cfg.Env.IsProduction() && cfg.Database.Password == "" {
		l.errorf("%s: must be set when env = production", l.where("database", "password"))
	}
}

// fieldKey identifies one setting by its position in the file.
type fieldKey struct{ section, key string }

// loader reads typed values from the file and environment, accumulating
// problems rather than failing at the first one.
type loader struct {
	lookup Lookup
	file   *ini.File
	known  map[fieldKey]bool
	errs   []error
}

// envName is the environment variable that overrides a setting:
// FRIDAY_SERVER_ADDR for [server] addr, FRIDAY_ENV for a top-level key.
func envName(section, key string) string {
	if section == "" {
		return "FRIDAY_" + strings.ToUpper(key)
	}
	return "FRIDAY_" + strings.ToUpper(section) + "_" + strings.ToUpper(key)
}

// where names a setting the way the user wrote it, so an error points at
// something they can find and edit.
func (l *loader) where(section, key string) string {
	if section == "" {
		return fmt.Sprintf("%s (env %s)", key, envName(section, key))
	}
	return fmt.Sprintf("[%s] %s (env %s)", section, key, envName(section, key))
}

// value resolves one setting, preferring the environment over the file.
func (l *loader) value(section, key string) (string, bool) {
	l.known[fieldKey{section, key}] = true

	if v, ok := l.lookup(envName(section, key)); ok {
		// An explicitly empty variable means "unset" rather than "blank", so
		// that FRIDAY_SERVER_ADDR= in a shell script falls through.
		if v = strings.TrimSpace(v); v != "" {
			return v, true
		}
	}

	if l.file != nil {
		s := l.file.Section(section)
		if s.HasKey(key) {
			if v := strings.TrimSpace(s.Key(key).String()); v != "" {
				return v, true
			}
		}
	}
	return "", false
}

// rejectUnknownKeys reports settings present in the file that nothing reads,
// which is almost always a typo that would otherwise be silently ignored.
func (l *loader) rejectUnknownKeys(path string) {
	if l.file == nil {
		return
	}
	for _, section := range l.file.Sections() {
		name := section.Name()
		if name == ini.DefaultSection {
			name = ""
		}
		for _, key := range section.Keys() {
			if !l.known[fieldKey{name, key.Name()}] {
				l.errorf("%s: unknown setting %s", path, l.where(name, key.Name()))
			}
		}
	}
}

func (l *loader) errorf(format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf(format, args...))
}

func (l *loader) err() error {
	if len(l.errs) == 0 {
		return nil
	}
	parts := make([]string, len(l.errs))
	for i, err := range l.errs {
		parts[i] = err.Error()
	}
	return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(parts, "\n  - "))
}

func (l *loader) str(section, key, fallback string) string {
	if v, ok := l.value(section, key); ok {
		return v
	}
	return fallback
}

func (l *loader) integer(section, key string, fallback int) int {
	v, ok := l.value(section, key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errorf("%s: %q is not a whole number", l.where(section, key), v)
		return fallback
	}
	return n
}

func (l *loader) duration(section, key string, fallback time.Duration) time.Duration {
	v, ok := l.value(section, key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.errorf("%s: %q is not a duration (want a form like 30s or 5m)", l.where(section, key), v)
		return fallback
	}
	if d < 0 {
		l.errorf("%s: %q must not be negative", l.where(section, key), v)
		return fallback
	}
	return d
}

func (l *loader) boolean(section, key string, fallback bool) bool {
	v, ok := l.value(section, key)
	if !ok {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.errorf("%s: %q is not a boolean (want true or false)", l.where(section, key), v)
		return fallback
	}
	return b
}
