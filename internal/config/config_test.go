package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/DhanushRamesh/friday/internal/config"
	"github.com/DhanushRamesh/friday/internal/logging"
)

// env : Builds a Lookup over a map, standing in for the process environment.
func env(pairs map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		v, ok := pairs[key]
		return v, ok
	}
}

// writeINI : Puts contents in a temporary file and returns its path.
func writeINI(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestDefaultsWithNoFile(t *testing.T) {
	cfg, err := config.Load("", env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Env != config.EnvDev {
		t.Errorf("Env = %q, want dev", cfg.Env)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("Server.Addr = %q, want :8080", cfg.Server.Addr)
	}
	// Dev should be readable by a human at a terminal.
	if cfg.Log.Format != logging.FormatText || !cfg.Log.AddSource {
		t.Errorf("dev logging = %q source=%v, want text with source", cfg.Log.Format, cfg.Log.AddSource)
	}
	if cfg.Database.Port != 3306 || cfg.Database.Name != "friday" {
		t.Errorf("Database = %s, want friday on 3306", cfg.Database.SafeAddr())
	}
}

func TestReadsValuesFromFile(t *testing.T) {
	path := writeINI(t, `
env = production

[server]
addr             = :9090
shutdown_timeout = 45s

[log]
level  = debug
format = json
source = false

[database]
host     = db.internal
port     = 3307
user     = agent
password = filepassword
name     = frida
max_open_conns = 50
`)

	cfg, err := config.Load(path, env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Env != config.EnvProduction {
		t.Errorf("Env = %q, want production", cfg.Env)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("Addr = %q, want :9090", cfg.Server.Addr)
	}
	if cfg.Server.ShutdownTimeout != 45*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 45s", cfg.Server.ShutdownTimeout)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != logging.FormatJSON || cfg.Log.AddSource {
		t.Errorf("log = %+v, want debug/json/no-source", cfg.Log)
	}
	if cfg.Database.SafeAddr() != "agent@db.internal:3307/frida" {
		t.Errorf("Database = %s, want agent@db.internal:3307/frida", cfg.Database.SafeAddr())
	}
	if cfg.Database.MaxOpenConns != 50 {
		t.Errorf("MaxOpenConns = %d, want 50", cfg.Database.MaxOpenConns)
	}
	// Settings absent from the file keep their defaults.
	if cfg.Server.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want the 60s default", cfg.Server.IdleTimeout)
	}
	if cfg.Source != path {
		t.Errorf("Source = %q, want the file path", cfg.Source)
	}
}

// The environment is where secrets come from on a deployed machine, so it
// must win over anything written in the file.
func TestEnvironmentOverridesFile(t *testing.T) {
	path := writeINI(t, `
[server]
addr = :9090

[database]
password = from-file
host     = from-file-host
`)

	cfg, err := config.Load(path, env(map[string]string{
		"FRIDAY_SERVER_ADDR":       ":7070",
		"FRIDAY_DATABASE_PASSWORD": "from-env",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Addr != ":7070" {
		t.Errorf("Addr = %q, want the environment value :7070", cfg.Server.Addr)
	}
	if cfg.Database.Password.Reveal() != "from-env" {
		t.Errorf("Password = %q, want the environment value", cfg.Database.Password.Reveal())
	}
	// Keys the environment does not mention still come from the file.
	if cfg.Database.Host != "from-file-host" {
		t.Errorf("Host = %q, want the file value", cfg.Database.Host)
	}
}

// An explicitly empty variable means "unset", so the file value still applies.
func TestEmptyEnvFallsThroughToFile(t *testing.T) {
	path := writeINI(t, "[server]\naddr = :9090\n")

	cfg, err := config.Load(path, env(map[string]string{"FRIDAY_SERVER_ADDR": "  "}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("Addr = %q, want the file value :9090", cfg.Server.Addr)
	}
}

// A misspelled key that nothing reads would otherwise be silently ignored,
// leaving the user staring at a setting that does nothing.
func TestUnknownKeysRejected(t *testing.T) {
	path := writeINI(t, `
[server]
addr   = :9090
addres = :9091

[databse]
host = wrong-section
`)

	_, err := config.Load(path, env(nil))
	if err == nil {
		t.Fatal("Load: want an error for unknown settings, got nil")
	}
	for _, want := range []string{"addres", "databse"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// Every problem should be reported at once, not one restart at a time.
func TestAllErrorsReportedTogether(t *testing.T) {
	path := writeINI(t, `
env = staging

[log]
level = loud

[server]
idle_timeout = ages

[database]
port = 99999
`)

	_, err := config.Load(path, env(nil))
	if err == nil {
		t.Fatal("Load: want error, got nil")
	}
	for _, want := range []string{"env", "level", "idle_timeout", "port"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// Errors must name the setting in both the file spelling and the environment
// spelling, so the user can find it whichever way they set it.
func TestErrorsNameFileAndEnvSpelling(t *testing.T) {
	path := writeINI(t, "[database]\nport = nonsense\n")

	_, err := config.Load(path, env(nil))
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{"[database] port", "FRIDAY_DATABASE_PORT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestProductionRequiresDatabasePassword(t *testing.T) {
	_, err := config.Load("", env(map[string]string{"FRIDAY_ENV": "production"}))
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("want a password error in production, got %v", err)
	}

	// The same blank password is fine on a developer machine.
	if _, err := config.Load("", env(map[string]string{"FRIDAY_ENV": "dev"})); err != nil {
		t.Errorf("dev with blank password: %v", err)
	}
}

func TestIdleConnsCannotExceedOpenConns(t *testing.T) {
	path := writeINI(t, "[database]\nmax_open_conns = 5\nmax_idle_conns = 10\n")

	_, err := config.Load(path, env(nil))
	if err == nil || !strings.Contains(err.Error(), "max_idle_conns") {
		t.Fatalf("want an idle/open conns error, got %v", err)
	}
}

func TestMissingFileIsAnError(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "absent.ini"), env(nil))
	if err == nil {
		t.Fatal("Load with a missing named file: want error, got nil")
	}
}

// A DSN is only correct if the driver can parse it back. A password
// containing @ : / ? is the case that naive string building gets wrong.
func TestDSNRoundTripsThroughDriver(t *testing.T) {
	const password = "p@ss:word/with?specials"
	path := writeINI(t, "[database]\npassword = "+password+"\n")

	cfg, err := config.Load(path, env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	parsed, err := mysql.ParseDSN(cfg.Database.DSN())
	if err != nil {
		t.Fatalf("driver cannot parse the DSN we generated: %v", err)
	}
	if parsed.Passwd != password {
		t.Errorf("Passwd = %q, want it to survive the round trip intact", parsed.Passwd)
	}
	if parsed.Addr != "127.0.0.1:3306" || parsed.DBName != "friday" {
		t.Errorf("target = %s/%s, want 127.0.0.1:3306/friday", parsed.Addr, parsed.DBName)
	}
	// Without ParseTime, DATETIME columns scan as []byte rather than time.Time.
	if !parsed.ParseTime {
		t.Error("ParseTime = false; timestamps would not scan into time.Time")
	}
	// UTC everywhere, so a server timezone change cannot reinterpret stored rows.
	if parsed.Loc != time.UTC {
		t.Errorf("Loc = %v, want UTC", parsed.Loc)
	}
}

// The password must not be reachable through logging, printing or a config dump.
func TestPasswordNeverAppearsInLogsOrSafeAddr(t *testing.T) {
	const password = "hunter2-do-not-log"
	cfg, err := config.Load("", env(map[string]string{"FRIDAY_DATABASE_PASSWORD": password}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if strings.Contains(cfg.Database.SafeAddr(), password) {
		t.Errorf("SafeAddr leaked the password: %s", cfg.Database.SafeAddr())
	}
	if strings.Contains(cfg.LogValue().String(), password) {
		t.Errorf("LogValue leaked the password: %s", cfg.LogValue().String())
	}
	// The DSN legitimately carries it; that value goes to the driver only.
	if !strings.Contains(cfg.Database.DSN(), password) {
		t.Error("DSN should carry the real password for the driver")
	}
}

// The example file is what a new user copies, so it must actually load and
// must stay in step with the keys the loader knows about.
func TestExampleFileIsValid(t *testing.T) {
	cfg, err := config.Load("../../config.example.ini", env(nil))
	if err != nil {
		t.Fatalf("config.example.ini does not load: %v", err)
	}
	if cfg.Env != config.EnvDev {
		t.Errorf("example Env = %q, want dev", cfg.Env)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("example Addr = %q, want :8080", cfg.Server.Addr)
	}
}
