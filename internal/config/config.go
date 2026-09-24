// Package config : loads and validates FRIDAY's runtime configuration.
//
// Values resolve from three layers, each overriding the one before it:
//
//	built-in defaults  <  config.ini  <  environment variables
//
// Every setting has an environment equivalent named FRIDAY_<SECTION>_<KEY>,
// uppercased; a setting outside any section uses FRIDAY_<KEY>.
//
// Loading reports every problem it finds rather than stopping at the first,
// and treats a setting present in the file that nothing reads as an error.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"gopkg.in/ini.v1"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// DefaultPath : The configuration file read when none is named. It is
// optional; without it, defaults and environment variables apply.
const DefaultPath = "config.ini"

// PathEnvVar : Names the environment variable holding an explicit
// configuration file path. When it is set, the file must exist.
const PathEnvVar = "FRIDAY_CONFIG"

// Environment : Names a deployment context. Validation is stricter outside
// EnvDev.
type Environment string

// Recognised environments.
const (
	// EnvDev : A developer machine, where defaults such as a blank database
	// password are permitted.
	EnvDev Environment = "dev"
	// EnvProduction : A deployed environment.
	EnvProduction Environment = "production"
)

// IsProduction : Reports whether e is EnvProduction.
func (e Environment) IsProduction() bool { return e == EnvProduction }

// Config : The fully resolved configuration for one process.
type Config struct {
	Env        Environment
	Server     Server
	Log        Log
	Database   Database
	Assistant  Assistant
	Provider   Provider
	PlatformAI PlatformAI

	// Source : The path of the file the configuration was read from, or
	// empty if no file was read.
	Source string
}

// Server : Configures the HTTP listener.
type Server struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	RequestTimeout    time.Duration

	// AllowPublicBind : Permits listening on a public interface in
	// production. Off by default, because FRIDAY speaks plain HTTP and its
	// tokens are bearer credentials: anything in front of it must terminate
	// TLS, and the way to guarantee that is to be unreachable except through
	// it.
	AllowPublicBind bool
}

// Log : Configures the structured logger.
type Log struct {
	Level     string
	Format    logging.Format
	AddSource bool
}

// ProviderName : Which engine answers a chat.
type ProviderName string

const (
	// ProviderStub : Answers from a script, needing no credentials and no
	// network. Useful for working on everything around the answer.
	ProviderStub ProviderName = "stub"
	// ProviderPlatformAI : Answers using Zoho Platform AI.
	ProviderPlatformAI ProviderName = "platformai"
)

// Assistant : What the assistant is called.
//
// The name lives here rather than in the code because it is the owner's
// choice, not the server's: the same binary should serve whatever the
// assistant is called today without being rebuilt. Nothing else in the
// server states a name.
type Assistant struct {
	// Name : What the assistant calls itself when it answers. Empty leaves
	// it nameless, which is a working assistant that simply never says what
	// it is called.
	Name string
}

// Provider : Chooses which engine answers chats.
type Provider struct {
	// Name : Which provider to use.
	Name ProviderName
}

// PlatformAI : Credentials and endpoints for Zoho Platform AI. Read only when
// it is the selected provider.
type PlatformAI struct {
	ClientID     string
	ClientSecret logging.Secret
	RefreshToken logging.Secret
	PortalID     string

	TokenURL    string
	ChatURL     string
	Scope       string
	RedirectURI string

	Vendor string
	Model  string

	Timeout time.Duration
	// InsecureSkipVerify : Skips certificate verification. Needed only for the
	// internal endpoints, whose certificates come from an internal authority.
	InsecureSkipVerify bool
}

// Database : Configures the MySQL connection. The password is held separately
// as a logging.Secret so that a Database can be logged without exposing it.
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

	// AutoMigrate : Whether to apply outstanding migrations at startup.
	// Turning it off leaves the schema to be migrated by a separate step.
	AutoMigrate bool
}

// DSN : Returns the connection string for go-sql-driver/mysql.
//
// The result contains the password. Use SafeAddr for anything that is logged
// or shown to a user.
func (d Database) DSN() string {
	c := mysql.NewConfig()
	c.Net = "tcp"
	c.Addr = fmt.Sprintf("%s:%d", d.Host, d.Port)
	c.User = d.User
	c.Passwd = d.Password.Reveal()
	c.DBName = d.Name
	// Without ParseTime, DATETIME columns scan as []byte rather than time.Time.
	c.ParseTime = true
	// UTC, so that the server's timezone cannot reinterpret stored rows.
	c.Loc = time.UTC
	c.Timeout = d.ConnectTimeout
	c.Params = map[string]string{"time_zone": "'+00:00'"}
	return c.FormatDSN()
}

// SafeAddr : Returns the connection target in the form user@host:port/name,
// without the password.
func (d Database) SafeAddr() string {
	return fmt.Sprintf("%s@%s:%d/%s", d.User, d.Host, d.Port, d.Name)
}

// LogValue : Implements slog.LogValuer, rendering the configuration without the
// database password.
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
		slog.Bool("database.auto_migrate", c.Database.AutoMigrate),
		slog.String("provider.name", string(c.Provider.Name)),
		slog.String("platformai.model", c.PlatformAI.Model),
	)
}

// Lookup : Reports the value of an environment variable and whether it was set.
// It has the signature of os.LookupEnv.
type Lookup func(key string) (string, bool)

// LoadFromEnv : Resolves configuration from the process environment and the
// file named by PathEnvVar, or DefaultPath when that variable is unset.
//
// A missing DefaultPath is not an error; a missing PathEnvVar target is.
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

// Load : Resolves configuration from the file at path, with values from lookup
// taking precedence. An empty path skips the file.
//
// The returned error, if any, describes every problem found.
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
			// Loopback by default. ":8080" looks like localhost and is
			// not: it binds every interface, which would put a plain-HTTP
			// service carrying bearer tokens on the network by accident.
			Addr:              l.str("server", "addr", "127.0.0.1:8080"),
			ReadHeaderTimeout: l.duration("server", "read_header_timeout", 5*time.Second),
			IdleTimeout:       l.duration("server", "idle_timeout", 60*time.Second),
			ShutdownTimeout:   l.duration("server", "shutdown_timeout", 15*time.Second),
			RequestTimeout:    l.duration("server", "request_timeout", 30*time.Second),
			AllowPublicBind:   l.boolean("server", "allow_public_bind", false),
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
			AutoMigrate:     l.boolean("database", "auto_migrate", true),
		},
		Assistant: Assistant{
			Name: l.str("assistant", "name", ""),
		},
		Provider: Provider{
			Name: ProviderName(l.str("provider", "name", string(ProviderStub))),
		},
		PlatformAI: PlatformAI{
			ClientID:           l.str("platformai", "client_id", ""),
			ClientSecret:       logging.Secret(l.str("platformai", "client_secret", "")),
			RefreshToken:       logging.Secret(l.str("platformai", "refresh_token", "")),
			PortalID:           l.str("platformai", "portal_id", ""),
			TokenURL:           l.str("platformai", "token_url", ""),
			ChatURL:            l.str("platformai", "chat_url", ""),
			Scope:              l.str("platformai", "scope", ""),
			RedirectURI:        l.str("platformai", "redirect_uri", ""),
			Vendor:             l.str("platformai", "vendor", ""),
			Model:              l.str("platformai", "model", ""),
			Timeout:            l.duration("platformai", "timeout", 120*time.Second),
			InsecureSkipVerify: l.boolean("platformai", "insecure_skip_verify", false),
		},
	}

	l.rejectUnknownKeys(path)
	l.validate(cfg)

	if err := l.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate : Records an error for each setting in cfg that is out of range or
// missing.
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

	// Plain HTTP on a public interface exposes every bearer token to anyone
	// on the network. A misconfiguration that does this is silent, so it is
	// refused rather than warned about.
	if cfg.Env.IsProduction() && !cfg.Server.AllowPublicBind && bindsPublicly(cfg.Server.Addr) {
		l.errorf("%s: %q listens on a public interface, and this server speaks plain HTTP. "+
			"Bind 127.0.0.1 and put TLS in front of it, or set %s if something else already does",
			l.where("server", "addr"), cfg.Server.Addr, l.where("server", "allow_public_bind"))
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

	if cfg.Env.IsProduction() && cfg.Database.Password == "" {
		l.errorf("%s: must be set when env = production", l.where("database", "password"))
	}
}

// fieldKey : Identifies a setting by the section and key naming it in the file.
type fieldKey struct{ section, key string }

// loader : Reads typed settings from a file and an environment lookup,
// accumulating errors rather than returning at the first.
type loader struct {
	lookup Lookup
	file   *ini.File
	known  map[fieldKey]bool
	errs   []error
}

// envName : Returns the environment variable that overrides the given setting,
// such as FRIDAY_SERVER_ADDR for [server] addr.
func envName(section, key string) string {
	if section == "" {
		return "FRIDAY_" + strings.ToUpper(key)
	}
	return "FRIDAY_" + strings.ToUpper(section) + "_" + strings.ToUpper(key)
}

// where : Renders a setting in both spellings, for use in error messages.
func (l *loader) where(section, key string) string {
	if section == "" {
		return fmt.Sprintf("%s (env %s)", key, envName(section, key))
	}
	return fmt.Sprintf("[%s] %s (env %s)", section, key, envName(section, key))
}

// value : Returns the raw text of a setting and whether it was found, taking
// the environment in preference to the file. It records the setting as known.
func (l *loader) value(section, key string) (string, bool) {
	l.known[fieldKey{section, key}] = true

	if v, ok := l.lookup(envName(section, key)); ok {
		// An empty variable counts as unset, so that FRIDAY_SERVER_ADDR= in a
		// shell script falls through to the next layer.
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

// rejectUnknownKeys : Records an error for each setting in the file that no
// call to value claimed, which indicates a misspelled key.
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

// errorf : Records a validation problem.
func (l *loader) errorf(format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf(format, args...))
}

// err : Returns every recorded problem as a single error, or nil if there were
// none.
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

// str : Returns the setting's value, or fallback if it is not set.
func (l *loader) str(section, key, fallback string) string {
	if v, ok := l.value(section, key); ok {
		return v
	}
	return fallback
}

// integer : Returns the setting parsed as an int, or fallback if it is not set.
// A value that does not parse is recorded as an error.
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

// duration : Returns the setting parsed as a time.Duration, or fallback if it
// is not set. A value that does not parse, or is negative, is recorded as an
// error.
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

// boolean : Returns the setting parsed as a bool, or fallback if it is not set.
// A value that does not parse is recorded as an error.
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

// bindsPublicly : Reports whether an address accepts connections from beyond
// this machine.
//
// An empty host, as in ":8080", is the one that catches people out: it binds
// every interface, not the loopback it resembles.
func bindsPublicly(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Unparseable, so nothing can be concluded. Other validation reports
		// the malformed address; this check stays silent rather than guessing.
		return false
	}

	switch host {
	case "", "0.0.0.0", "::":
		return true
	case "localhost":
		return false
	}

	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	// A hostname that is not "localhost" resolves somewhere, and where is not
	// knowable here. Treated as public, which errs towards refusing.
	return true
}
