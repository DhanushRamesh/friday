//go:build evals

// Package evals asks a real model whether the tool descriptions work.
//
// Everything else in the suite checks that the machinery runs. This checks
// the only thing that decides whether the assistant is any good: given what
// somebody said, does the model reach for the right tool and fill in the
// right arguments. That cannot be asserted against a stub, because a stub has
// no opinion about a description.
//
// Behind a build tag because it costs real calls to a real service:
//
//	make evals
//
// A failure here is usually a description to fix rather than code. The point
// is that a change to a description becomes measurable instead of arguable.
package evals

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	remindmemory "github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
)

// eval : One thing a person might say, and what should happen.
type eval struct {
	// Say : What the person said.
	Say string
	// Tool : The tools that would be a right first move, any one of them.
	//
	// Several, because more than one can be right: asked to delete something
	// by name, looking the name up and listing everything both lead to the
	// identifier the delete needs. Empty means no tool should be called at
	// all, which is as much a decision as choosing one.
	Tool []string
	// Args : Fragments that must appear in the arguments, if any.
	Args []string
	// Channel : How it arrived. Voice cannot reach everything.
	Channel chat.Channel
}

// cases : What the assistant is expected to do.
var cases = []eval{
	// Reaching for the right one.
	{Say: "what conversations have I had", Tool: []string{"conversation_list"}},
	{Say: "what have we been talking about lately", Tool: []string{"conversation_list"}},
	{Say: "show me the ones I put away", Tool: []string{"conversation_list"}, Args: []string{"archived"}},
	{Say: "go back to the roof conversation", Tool: []string{"conversation_find"}, Args: []string{"roof"}},
	{Say: "switch to the one about cricket", Tool: []string{"conversation_find"}, Args: []string{"cricket"}},
	{Say: "start a new conversation", Tool: []string{"conversation_new"}},
	{Say: "let's start fresh, call it Kitchen Plans", Tool: []string{"conversation_new"}, Args: []string{"Kitchen"}},
	{Say: "call this one Roof Quotes", Tool: []string{"conversation_rename"}, Args: []string{"current", "Roof Quotes"}},
	{Say: "I'm done with this conversation, put it away", Tool: []string{"conversation_archive"}, Args: []string{"current", "true"}},

	// Archiving, not deleting. The words differ and so should the tool: one
	// of them cannot be undone.
	{Say: "I've finished with this one", Tool: []string{"conversation_archive"}},

	// Either way of finding the identifier is right, and the point is that it
	// looks one up rather than inventing one.
	{Say: "delete the Roof Quotes conversation",
		Tool: []string{"conversation_find", "conversation_list"}, Channel: chat.ChannelDirect},

	// Remembering when asked.
	{Say: "remember that the roofer quoted forty thousand", Tool: []string{"memory_remember"}, Args: []string{"roof"}},
	{Say: "keep a note that my birthday is the 22nd of October", Tool: []string{"memory_remember"}, Args: []string{"birthday"}},
	{Say: "from now on always answer me briefly", Tool: []string{"memory_remember"}, Args: []string{"always"}},

	// And when not asked. Having to say "remember this" is the thing being
	// designed away: a constraint, a decision and its reason, and a figure
	// agreed are all worth keeping whether or not anybody said so.
	{Say: "I cannot take dairy, it gives me a headache", Tool: []string{"memory_remember"}},
	{Say: "we settled on MySQL in the end, Postgres would have been another thing to run",
		Tool: []string{"memory_remember"}},
	{Say: "the plumber and I agreed twelve thousand for the bathroom", Tool: []string{"memory_remember"}},
	{Say: "my sister's flight lands on the 3rd of March", Tool: []string{"memory_remember"}},

	// Looking something up on purpose, when recall has offered nothing.
	{Say: "what did I tell you about the roof", Tool: []string{"memory_search"}, Args: []string{"roof"}},
	{Say: "do you remember anything about my shopping list", Tool: []string{"memory_search"}, Args: []string{"shopping"}},

	// Changing and forgetting both need the identifier, and the point is
	// that it looks one up rather than inventing one.
	{Say: "the roofer actually said fifty thousand, update that",
		Tool: []string{"memory_search", "memory_update"}},
	{Say: "forget what I told you about the roof",
		Tool: []string{"memory_search", "memory_forget"}, Channel: chat.ChannelDirect},

	// Reminders. A length of time goes to minutes_from_now so the
	// arithmetic is the server's, and a time of day goes to at.
	{Say: "set a timer for twenty minutes", Tool: []string{"reminder_set"}, Args: []string{"20"}},
	{Say: "remind me in two hours to take the washing out", Tool: []string{"reminder_set"}, Args: []string{"120"}},
	{Say: "wake me at seven every weekday", Tool: []string{"reminder_set"}, Args: []string{"weekdays"}},
	{Say: "what timers do I have", Tool: []string{"reminder_list"}},
	{Say: "cancel my timer", Tool: []string{"reminder_list"}},

	// A note for later is remembered, not announced. The two are easy to
	// confuse and only one of them speaks at you.
	{Say: "note down that the roofer wants paying by Friday", Tool: []string{"memory_remember"}},

	// Reaching for none. A model that calls a tool at every question is as
	// wrong as one that never does.
	{Say: "what is the capital of Australia"},
	{Say: "how are you"},
	{Say: "thank you"},
	{Say: "what is twelve times eight"},

	// Nor is every passing fact a thing to write down. The test is whether
	// it should still be believed next month, and everything said is
	// already searchable on its own, so none of these needs a memory.
	{Say: "I had dosa for breakfast"},
	{Say: "it is raining here today"},
	{Say: "I am a bit tired this evening"},
	{Say: "that took longer than I expected"},
}

func TestTheModelReachesForTheRightTool(t *testing.T) {
	// A store, so a tool that runs has somewhere to run against. What is
	// measured is the choice, which is made before anything runs, but a
	// registry that cannot build measures nothing.
	recall := &memory.Recall{Store: inmemory.New(), Embedder: embed.Fake{}}

	clock := reminders.Clock{Location: time.UTC}
	registry, err := tool.NewRegistry(slices.Concat(
		conversations.All(nil),
		memories.All(recall),
		reminders.All(remindmemory.New(), clock),
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

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
		SystemPrompt: persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name),
		Timeout:      90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	var right, wrong int
	for _, c := range cases {
		channel := c.Channel
		if channel == "" {
			channel = chat.ChannelVoice
		}

		called, args := ask(t, env, registry, channel, c.Say)

		switch {
		case len(c.Tool) == 0 && called != "":
			wrong++
			t.Errorf("%-45q called %s, want it answered without a tool", c.Say, called)
		case len(c.Tool) > 0 && called == "":
			wrong++
			t.Errorf("%-45q called nothing, want one of %v", c.Say, c.Tool)
		case !among(called, c.Tool):
			wrong++
			t.Errorf("%-45q called %s, want one of %v", c.Say, called, c.Tool)
		default:
			var missing []string
			for _, want := range c.Args {
				if !strings.Contains(strings.ToLower(args), strings.ToLower(want)) {
					missing = append(missing, want)
				}
			}
			if len(missing) > 0 {
				wrong++
				t.Errorf("%-45q called %s with %s, missing %v", c.Say, called, args, missing)
			} else {
				right++
			}
		}
	}

	t.Logf("%d of %d right", right, right+wrong)
}

// ask : What the model does with one thing somebody said.
//
// One call, not the whole loop: what is being measured is the choice, and the
// choice is made before anything runs.
func ask(t *testing.T, env *platformai.Environment, registry *tool.Registry, channel chat.Channel, say string) (string, string) {
	t.Helper()

	reachable := registry.For(channel)
	specs := make([]environment.ToolSpec, 0, len(reachable))
	for _, x := range reachable {
		schema, err := json.Marshal(x.Params)
		if err != nil {
			t.Fatalf("%s has an unusable schema: %v", x.Name, err)
		}
		specs = append(specs, environment.ToolSpec{
			Name: x.Name, Description: x.Description(), Parameters: schema,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{Prompt: say, Tools: specs})
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
	if final == nil {
		t.Fatalf("%q got no answer at all", say)
	}
	if final.Kind == environment.KindError {
		t.Fatalf("%q failed: %s", say, final.Detail)
	}
	if len(final.ToolCalls) == 0 {
		return "", ""
	}
	return final.ToolCalls[0].Name, final.ToolCalls[0].Arguments
}

// among : Whether the tool called is one of those that would be right.
func among(called string, right []string) bool {
	for _, name := range right {
		if name == called {
			return true
		}
	}
	return len(right) == 0 && called == ""
}
