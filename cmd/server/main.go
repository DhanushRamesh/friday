// Command server runs FRIDAY.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
	"github.com/DhanushRamesh/personal-assistant/internal/provider/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// main : Runs the server, or the named command.
// serviceName : What this process calls itself in its own logs.
//
// Not the assistant's name, which is configuration: this identifies the
// process to whoever is reading the journal, and stays the same whatever the
// assistant is called today.
const serviceName = "assistant"

func main() {
	if len(os.Args) > 2 && os.Args[1] == "createuser" {
		osExitOnError(runCreateUser(os.Args[2]))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "createuser" {
		fmt.Fprintln(os.Stderr, "usage: friday createuser <username>")
		os.Exit(1)
	}

	if err := run(); err != nil {
		// Configuration is read before the logger exists, so this cannot be
		// a structured record.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// storageDB : The database handle a command works through.
type storageDB = storage.DB

// runCreateUser : Opens the database and creates a user, without starting the
// server.
func runCreateUser(username string) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	logger, err := logging.New(os.Stdout, logging.Config{
		Level: "warn", Format: cfg.Log.Format, Service: serviceName,
	})
	if err != nil {
		return err
	}

	db, err := storage.Open(context.Background(), cfg.Database, logger.Logger, storage.Options{})
	if err != nil {
		return err
	}
	defer db.Close()

	if cfg.Database.AutoMigrate {
		if err := storage.Migrate(context.Background(), db, logger.Logger); err != nil {
			return err
		}
	}
	return createUser(username, db)
}

// run : Loads configuration, opens the dependencies and serves until
// interrupted, returning the first error that prevents any of it.
func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	logger, err := logging.New(os.Stdout, logging.Config{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
		Service:   serviceName,
		Version:   version(),
		Env:       string(cfg.Env),
	})
	if err != nil {
		return err
	}
	// So that packages logging without an injected logger use this format.
	slog.SetDefault(logger.Logger)

	// Config redacts the database password, so this is safe to emit.
	logger.Info("configuration loaded", slog.Any("config", cfg))

	// Opened here so an unusable database stops startup rather than failing
	// on the first request.
	db, err := storage.Open(context.Background(), cfg.Database, logger.Logger, storage.Options{
		// Statements carry user data, so they are recorded outside production only.
		LogStatements: !cfg.Env.IsProduction(),
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("closing database", slog.Any("error", err))
		}
	}()

	if cfg.Database.AutoMigrate {
		if err := storage.Migrate(context.Background(), db, logger.Logger); err != nil {
			return err
		}
	}

	chats := chatmysql.NewRepository(db)

	// Carries a chat's messages to whoever is listening for them.
	bus := events.NewBus(logger.Logger)
	defer bus.Close()

	answerer, err := buildProvider(cfg, logger.Logger)
	if err != nil {
		return err
	}
	logger.Info("provider selected", slog.String("provider", answerer.Name()))

	chatRunner, err := runner.New(runner.Options{
		Repository: chats,
		Messages:   chats,
		Provider:   answerer,
		Logger:     logger.Logger,
		Publisher:  bus,
	})
	if err != nil {
		return err
	}

	// Chats the previous process was running are no longer being worked on.
	if err := chatRunner.Recover(context.Background()); err != nil {
		return err
	}

	handler := api.New(api.Options{
		Logger:         logger.Logger,
		DB:             db,
		Chats:          chats,
		Runner:         chatRunner,
		Events:         bus,
		RequestTimeout: cfg.Server.RequestTimeout,
		// Development only: `flutter run` serves the UI from its own port so
		// that hot reload works. In production FRIDAY serves it, so every
		// call is same-origin.
		AllowCrossOrigin: !cfg.Env.IsProduction(),
	})

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	return serve(srv, chatRunner, logger, cfg.Server.ShutdownTimeout)
}

// buildProvider : Returns the engine that answers chats, as configuration
// selects it.
func buildProvider(cfg config.Config, logger *slog.Logger) (provider.Provider, error) {
	switch cfg.Provider.Name {
	case config.ProviderPlatformAI:
		return platformai.New(platformai.Config{
			ClientID:           cfg.PlatformAI.ClientID,
			ClientSecret:       cfg.PlatformAI.ClientSecret,
			RefreshToken:       cfg.PlatformAI.RefreshToken,
			PortalID:           cfg.PlatformAI.PortalID,
			TokenURL:           cfg.PlatformAI.TokenURL,
			ChatURL:            cfg.PlatformAI.ChatURL,
			Scope:              cfg.PlatformAI.Scope,
			RedirectURI:        cfg.PlatformAI.RedirectURI,
			Vendor:             cfg.PlatformAI.Vendor,
			Model:              cfg.PlatformAI.Model,
			SystemPrompt:       platformai.SystemPromptFor(cfg.Assistant.Name),
			Timeout:            cfg.PlatformAI.Timeout,
			InsecureSkipVerify: cfg.PlatformAI.InsecureSkipVerify,
		}, logger)

	default:
		// Answers from a script, so everything around an answer can be worked
		// on without credentials or a network.
		return &provider.Stub{Delay: 300 * time.Millisecond}, nil
	}
}

// serve : Starts srv and blocks until an interrupt arrives, then drains
// in-flight requests before returning.
func serve(srv *http.Server, chatRunner *runner.Runner, logger *logging.Logger, shutdownTimeout time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "friday listening",
			slog.String("addr", srv.Addr),
			slog.String("log_level", logger.Level().String()),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining",
			slog.Duration("grace_period", shutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Stop accepting requests first, then let running chats record where they
	// got to. A chat cut off without that is left reading running for ever.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
		return err
	}
	if err := chatRunner.Shutdown(shutdownCtx); err != nil {
		logger.Error("running chats did not stop cleanly", slog.Any("error", err))
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

// version : Returns the VCS revision stamped into the binary by the Go
// toolchain, or a placeholder when it is unavailable.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "dev"
}
