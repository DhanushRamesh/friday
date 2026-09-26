// Command server runs the server.
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
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
	"github.com/DhanushRamesh/personal-assistant/internal/announce/hass"
	"github.com/DhanushRamesh/personal-assistant/internal/api"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/embed/tei"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/llm"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	memorymysql "github.com/DhanushRamesh/personal-assistant/internal/memory/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	remindmysql "github.com/DhanushRamesh/personal-assistant/internal/remind/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
)

// main : Runs the server, or the named command.
// serviceName : What this process calls itself in its own logs.
//
// Not the assistant's name, which is configuration: this identifies the
// process to whoever is reading the journal, and stays the same whatever the
// assistant is called today.
const serviceName = "assistant"

// embeddingCatchUpTimeout : How long embedding what has no vector may take
// at startup, before the server gets on with answering.
const embeddingCatchUpTimeout = 60 * time.Second

// embeddingCatchUpLimit : The most memories embedded in one catch-up.
const embeddingCatchUpLimit = 500

// transcriptCatchUpLimit : The most exchanges indexed in one catch-up.
//
// The transcript is indexed from nothing the first time, and the server
// should start answering rather than finish the backlog. What is left is
// picked up after each turn and at the next restart.
const transcriptCatchUpLimit = 2000

func main() {
	if len(os.Args) > 2 && os.Args[1] == "createuser" {
		osExitOnError(runCreateUser(os.Args[2]))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "createuser" {
		fmt.Fprintln(os.Stderr, "usage: personal-assistant createuser <username>")
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

	// One manner, shared by the runner that speaks in it and the API that
	// changes it. Held in memory because it is read on every prompt, and
	// started from what was last chosen, falling back to configuration when
	// nothing has been.
	// Somewhere to say a thing nobody asked for. Silent unless Home
	// Assistant is configured, which is not a fault: it is how the server
	// behaved before it could speak first.
	speaker := announcer(cfg, logger.Logger)

	// What the assistant has been asked to remember. Without an embedding
	// server it still works, matching words rather than meaning, which is
	// worse than the alternative and much better than going blind.
	remembering := &memory.Recall{
		Store:      memorymysql.New(db),
		Transcript: memorymysql.NewTranscript(db),
		Embedder:   embedder(cfg, logger.Logger),
		Logger:     logger.Logger,
	}
	catchUpEmbeddings(context.Background(), remembering, logger.Logger)

	// One store, shared by the tools that make reminders and the loop that
	// says them.
	reminderStore := remindmysql.New(db)
	clock := reminders.Clock{Now: cfg.Assistant.Now, Location: cfg.Assistant.Location}

	// What the assistant can do as well as say. A registry that will not
	// build is a programming mistake, not a configuration one, so it stops
	// the server rather than quietly offering nothing.
	tools, err := tool.NewRegistry(slices.Concat(
		conversations.All(chats),
		memories.All(remembering),
		reminders.All(reminderStore, clock),
	)...)
	if err != nil {
		return err
	}
	logger.Info("tools registered", slog.Any("tools", tools.Names()))

	manner := persona.NewSetting(startingPersona(context.Background(), chats, cfg, logger.Logger))

	chatRunner, err := runner.New(runner.Options{
		Repository:    chats,
		Messages:      chats,
		Environment:   answerer,
		Logger:        logger.Logger,
		Publisher:     bus,
		HistoryLimits: historyLimits(cfg, logger.Logger),
		AssistantName: cfg.Assistant.Name,
		Persona:       manner,
		Announcer:     speaker,
		Tools:         tools,
		Memory:        remembering,
		Now:           cfg.Assistant.Now,
	})
	if err != nil {
		return err
	}

	// Chats the previous process was running are no longer being worked on.
	if err := chatRunner.Recover(context.Background()); err != nil {
		return err
	}

	// The first work here that happens because of the clock rather than
	// because somebody asked. Stopped with the server, so a reminder is
	// never half said during a shutdown.
	reminding := &remind.Loop{
		Store:    reminderStore,
		Speaker:  remind.Everywhere{To: []remind.Speaker{remind.Aloud{Announcer: speaker}}, Logger: logger.Logger},
		Location: cfg.Assistant.Location,
		Logger:   logger.Logger,
	}
	remindCtx, stopReminders := context.WithCancel(context.Background())

	var watching sync.WaitGroup
	watching.Add(1)
	go func() {
		defer watching.Done()
		reminding.Run(remindCtx)
	}()
	// Registered in this order so they run in the other: stop first, then
	// wait for the pass in flight to finish.
	defer watching.Wait()
	defer stopReminders()

	logger.Info("watching for reminders",
		slog.Duration("every", remind.DefaultEvery),
		slog.Duration("grace", remind.DefaultGrace))

	handler := api.New(api.Options{
		Logger:         logger.Logger,
		DB:             db,
		Chats:          chats,
		Messages:       chats,
		Reminders:      reminderStore,
		Runner:         chatRunner,
		Events:         bus,
		Models:         reachableModels(cfg),
		DefaultModel:   cfg.PlatformAI.Model,
		Persona:        manner,
		RequestTimeout: cfg.Server.RequestTimeout,
		// Development only: `flutter run` serves the UI from its own port so
		// that hot reload works. In production the server serves it, so every
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
func buildProvider(cfg config.Config, logger *slog.Logger) (environment.Environment, error) {
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
			SystemPrompt:       persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name),
			Timeout:            cfg.PlatformAI.Timeout,
			InsecureSkipVerify: cfg.PlatformAI.InsecureSkipVerify,
		}, logger)

	default:
		// Answers from a script, so everything around an answer can be worked
		// on without credentials or a network.
		return &environment.Stub{Delay: 300 * time.Millisecond}, nil
	}
}

// reachableModels : The models the configured provider can call, which are
// the ones a client may be set to answer with.
//
// The provider says which models it will answer with and the catalogue says
// what each one holds. Neither knows the other: a model the endpoint routes
// but nobody has catalogued is left out, since there would be nothing to
// tell a person about it.
func reachableModels(cfg config.Config) []llm.Model {
	if cfg.Provider.Name != config.ProviderPlatformAI {
		return nil
	}

	refs := platformai.Models()
	out := make([]llm.Model, 0, len(refs))
	for _, ref := range refs {
		if m, ok := llm.Find(ref.Vendor, ref.ID); ok {
			out = append(out, m)
		}
	}
	return out
}

// announcer : Where the assistant says something without being asked.
//
// Home Assistant when it is configured, and nowhere otherwise. Nowhere is a
// working server: it answers when spoken to, which is all it could ever do
// before.
func announcer(cfg config.Config, logger *slog.Logger) announce.Announcer {
	speaker, err := hass.New(hass.Config{
		URL:       cfg.HomeAssistant.URL,
		Token:     cfg.HomeAssistant.Token,
		Satellite: cfg.HomeAssistant.Satellite,
	})
	if err != nil {
		if !errors.Is(err, hass.ErrNotConfigured) {
			logger.Warn("cannot reach Home Assistant to speak through",
				slog.Any("error", err))
		}
		return announce.Silent{Logger: logger}
	}

	logger.Info("announcing through Home Assistant",
		slog.String("satellite", cfg.HomeAssistant.Satellite))
	return speaker
}

// embedder : What turns text into vectors.
//
// Nothing when none is configured, which leaves memory matching words. A
// server that is configured but not answering is not checked here: it is
// reached per request, and one that is down at startup and up a minute later
// should not leave the assistant matching words until it is restarted.
func embedder(cfg config.Config, logger *slog.Logger) embed.Embedder {
	if !cfg.Embedding.Configured() {
		logger.Info("no embedding server, so memories are found by their wording")
		return embed.Off{}
	}

	client := tei.New(tei.Config{
		URL:     cfg.Embedding.URL,
		Model:   cfg.Embedding.Model,
		Timeout: cfg.Embedding.Timeout,
	})
	logger.Info("embedding memories", slog.String("model", client.Model()))
	return client
}

// catchUpEmbeddings : Gives a vector to memories written while the embedding
// server was away.
//
// At startup, and never fatal. A memory with no vector is found by its
// wording until this succeeds, so failing here costs accuracy rather than
// the memory.
func catchUpEmbeddings(ctx context.Context, remembering *memory.Recall, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, embeddingCatchUpTimeout)
	defer cancel()

	done, err := remembering.Embed(ctx, embeddingCatchUpLimit)
	switch {
	case err != nil:
		logger.Warn("cannot embed the memories that have no vector yet",
			slog.Any("error", err))
	case done > 0:
		logger.Info("memories embedded", slog.Int("memories", done))
	}

	// The transcript is the larger of the two and is indexed from nothing
	// the first time, so it is bounded and finishes in the background over
	// however many restarts it takes.
	indexed, err := remembering.IndexTranscript(ctx, transcriptCatchUpLimit)
	switch {
	case err != nil:
		logger.Warn("cannot index the exchanges that have no vector yet",
			slog.Any("error", err))
	case indexed > 0:
		logger.Info("exchanges indexed", slog.Int("exchanges", indexed))
	}
}

// startingPersona : The manner to begin in.
//
// What was last chosen, or configuration when nothing has been. A database
// that will not answer is not a reason to refuse to start: the configured
// manner is a working assistant, and the next choice will store itself.
func startingPersona(ctx context.Context, repo chat.Repository, cfg config.Config, logger *slog.Logger) string {
	stored, err := repo.Setting(ctx, persona.SettingName)
	if err != nil {
		logger.Warn("cannot read the stored manner, using the configured one",
			slog.Any("error", err))
		return cfg.Assistant.Persona
	}
	if stored == "" {
		return cfg.Assistant.Persona
	}
	return stored
}

// historyLimits : The ceilings the conversation sent to the environment is held
// under, for the provider configuration selects.
//
// Each comes from whatever knows it: the message count from the service, the
// context window from the model behind it, and the size budget from us. A
// model missing from the catalogue contributes nothing and is reported, since
// the budget then governs alone.
func historyLimits(cfg config.Config, logger *slog.Logger) conversation.Limits {
	limits := conversation.Limits{Bytes: conversation.DefaultBudget}

	if cfg.Provider.Name != config.ProviderPlatformAI {
		return limits
	}

	limits.Count = platformai.MaxMessages

	vendor, id := cfg.PlatformAI.Vendor, cfg.PlatformAI.Model
	model, known := llm.Find(vendor, id)
	if !known {
		logger.Warn("model is not in the catalogue, so only the byte budget bounds the history",
			slog.String("vendor", vendor), slog.String("model", id))
		return limits
	}
	limits.ContextTokens = model.ContextTokens

	return limits
}

// serve : Starts srv and blocks until an interrupt arrives, then drains
// in-flight requests before returning.
func serve(srv *http.Server, chatRunner *runner.Runner, logger *logging.Logger, shutdownTimeout time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "server listening",
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
