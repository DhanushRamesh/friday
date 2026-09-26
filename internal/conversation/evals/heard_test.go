//go:build evals

// Package evals asks a real model what it does with a name it was given
// by speech-to-text.
//
// Everywhere else a misheard word can be reasoned about from the words
// around it. A name cannot: one nobody has heard of and one the decoder
// has mangled look exactly alike. "Alekhya" came back as Alikia, Alakia
// and alakia chintada in a single evening.
//
// Behind a build tag because it costs real calls to a real service.
package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"encoding/json"
	"slices"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	memorystore "github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	remindstore "github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
)

// named : Things said out loud that turn on a name the decoder may have
// mangled. Each has to draw a request for the spelling rather than a
// confident guess at some name the model happens to know.
var named = []string{
	"remind me to call Vaishnavai tomorrow morning",
	"who directed the film Thondimuthalum",
	"what is Karunakaran known for",
	"remember that Sowmiya is my sister",
	"set a reminder to message Pranaav at six",
}

// asked : Words that mean it wants the spelling.
var asked = []string{"spell", "spelling", "how is", "how do you write", "letter"}

// guessed : Ways of quietly settling on a different name instead.
var guessed = []string{"i assume", "i believe you mean", "presumably", "you must mean"}

func TestItAsksHowAHeardNameIsSpelt(t *testing.T) {
	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Timeout: 90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	// A spoken turn, composed as the runner composes one.
	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name) +
		" " + conversation.Now(time.Now()) +
		" " + conversation.Heard()

	var checked, assumed int
	for _, ask := range named {
		answer := strings.ToLower(say(t, env, system, ask))

		if word, ok := contains(answer, guessed); ok {
			assumed++
			t.Errorf("%-46q settled on a name (%q):\n    %s", ask, word, answer)
			continue
		}
		if _, ok := contains(answer, asked); !ok {
			assumed++
			t.Errorf("%-46q did not ask how it is spelt:\n    %s", ask, answer)
			continue
		}
		checked++
	}

	t.Logf("a heard name: %d of %d asked for the spelling, %d did not",
		checked, len(named), assumed)
}

// A typed turn is not told any of this, because typing spells the name
// already and asking would be an insult.
func TestTypedIsNotAskedToSpell(t *testing.T) {
	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured")
	}

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Timeout: 90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	// No Heard(): this is what a typed turn is given.
	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name) +
		" " + conversation.Now(time.Now())

	answer := strings.ToLower(say(t, env, system, "who directed the film Thondimuthalum"))
	if _, ok := contains(answer, []string{"spell", "spelling"}); ok {
		t.Errorf("a typed name was queried for its spelling:\n    %s", answer)
	}
}

// offered : The tools a spoken turn actually reaches.
//
// Without them the honesty rule answers first -- "I have no tool to set
// reminders" -- and the question of what to do with the name never comes
// up. An eval that does not offer them measures a turn nobody has.
func offered(t *testing.T) []environment.ToolSpec {
	t.Helper()

	recall := &memory.Recall{Store: memorystore.New(), Embedder: embed.Fake{}}
	registry, err := tool.NewRegistry(slices.Concat(
		conversations.All(nil),
		memories.All(recall),
		reminders.All(remindstore.New(), reminders.Clock{Location: time.UTC}),
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	var specs []environment.ToolSpec
	for _, x := range registry.For(chat.ChannelVoice) {
		schema, err := json.Marshal(x.Params)
		if err != nil {
			t.Fatalf("%s has an unusable schema: %v", x.Name, err)
		}
		specs = append(specs, environment.ToolSpec{
			Name: x.Name, Description: x.Description(), Parameters: schema,
		})
	}
	return specs
}

func contains(answer string, words []string) (string, bool) {
	for _, w := range words {
		if strings.Contains(answer, w) {
			return w, true
		}
	}
	return "", false
}

// say : What the assistant answers.
func say(t *testing.T, env *platformai.Environment, system, ask string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, SystemPrompt: system, Tools: offered(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var final *environment.Message
	for msg := range stream {
		if msg.Kind.Terminal() {
			m := msg
			final = &m
		}
	}
	if final == nil || final.Kind == environment.KindError {
		t.Fatalf("%q got no usable answer", ask)
	}

	// Reaching for a tool with the name it heard is the failure, not an
	// answer: it has committed to a spelling nobody confirmed.
	if len(final.ToolCalls) > 0 {
		return "acted with the name as heard: " + final.ToolCalls[0].Arguments
	}
	return strings.TrimSpace(final.Text)
}
