//go:build evals

// Package evals asks a real model whether the offered notes behave.
//
// The notes are put into the system prompt of the same call that answers, so
// the model filters and answers at once. That is cheaper than judging in a
// call of its own, and it is a different thing: notes that are in front of
// the model can colour an answer even when none of them was needed.
//
// Two failures are measured. Missing a note that holds the answer, and
// answering from a note that does not.
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

	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
)

// notes : What the search puts in front of the model. The same three every
// time, so that what changes between cases is only the question.
var notes = []memory.Match{
	{Memory: memory.Memory{Subject: "Roof quote", Body: "Agreed forty thousand rupees with the roofer for the terrace work."}},
	{Memory: memory.Memory{Subject: "Monthly budget", Body: "Keeping household spending under fifty thousand rupees this month."}},
	{Memory: memory.Memory{Subject: "Peanuts", Body: "Allergic to peanuts, and carries an epipen."}},
	{Memory: memory.Memory{Subject: "Cricket", Body: "Follows cricket closely, especially Kapil Dev and MS Dhoni."}},
	{Memory: memory.Memory{Subject: "Home", Body: "Lives in Chennai."}},
}

// warned : An intention a note disagrees with, and the thing a warning has
// to mention for it to be any use.
type warned struct {
	// Ask : What the person said they are about to do.
	Ask string
	// Want : One of these has to appear, or nothing useful was said.
	Want []string
}

// conflicts : Things that should draw a line, because a note disagrees.
var conflicts = []warned{
	{Ask: "I am going to tell the roofer that sixty thousand is fine",
		Want: []string{"forty thousand", "40,000", "40000"}},
	{Ask: "I will order the peanut korma for dinner tonight",
		Want: []string{"allerg", "epipen"}},
	{Ask: "I am about to spend another sixty thousand on the kitchen this month",
		Want: []string{"fifty thousand", "50,000", "50000"}},
}

// harmless : Intentions no note disagrees with. Nothing from a note should
// reach these answers: an assistant that raises something every time is
// worse than one that never does.
var harmless = []string{
	"I am going to watch the cricket this evening",
	"I am going to buy a new pair of shoes tomorrow",
	"I will call my brother later today",
	"I am going to pay the roofer the forty thousand we agreed",
}

// alarms : Fragments that only an unwanted warning could have supplied.
// Cricket and Chennai are left out: a question may reach them innocently.
var alarms = []string{"fifty thousand", "peanut", "allerg", "epipen", "terrace"}

// answered : A question whose answer is in the notes.
type answered struct {
	// Ask : What the person said.
	Ask string
	// Want : A fragment the answer has to contain, because it is in a note.
	Want string
}

// answerable : Questions the notes do answer.
var answerable = []answered{
	{Ask: "what did the roofer quote me", Want: "forty thousand"},
	{Ask: "which city do I live in", Want: "Chennai"},
	{Ask: "how much was the terrace work going to cost", Want: "forty thousand"},
	// The same thing named differently in the question and in the note. It
	// refused this in use and said nothing was on record, which is a miss
	// and a false statement at once.
	{Ask: "what did the builder charge for the upstairs work", Want: "forty thousand"},
	{Ask: "which town am I in", Want: "Chennai"},
}

// unanswerable : Questions the notes do not answer, though one of them is
// always the nearest thing to it. Nothing from a note should reach these
// answers.
var unanswerable = []string{
	"what is the boiling point of mercury",
	"who won the Nobel prize for physics in 1998",
	"write me a one line python function that reverses a list",
	"what time is sunset in Reykjavik today",
	"how do I fix a 502 from nginx",
}

// leaks : Fragments that only a note could have supplied.
var leaks = []string{"forty thousand", "Chennai", "Kapil Dev", "MS Dhoni", "roofer", "terrace", "epipen"}

func TestOfferedNotesAreUsedOnlyWhenTheyFit(t *testing.T) {
	env := provider(t)
	system := manner(t) + "\n\n" + memory.Offered(notes)

	var used, missed int
	for _, c := range answerable {
		answer := ask(t, env, system, c.Ask)
		if strings.Contains(strings.ToLower(answer), strings.ToLower(c.Want)) {
			used++
			continue
		}
		missed++
		t.Errorf("%-46q did not use the note holding %q:\n    %s", c.Ask, c.Want, answer)
	}

	var clean, leaked int
	for _, question := range unanswerable {
		answer := strings.ToLower(ask(t, env, system, question))

		var found []string
		for _, leak := range leaks {
			if strings.Contains(answer, strings.ToLower(leak)) {
				found = append(found, leak)
			}
		}
		if len(found) == 0 {
			clean++
			continue
		}
		leaked++
		t.Errorf("%-46q leaked %v from a note that did not fit:\n    %s", question, found, answer)
	}

	t.Logf("notes that fit    : %d of %d used", used, used+missed)
	t.Logf("notes that do not : %d of %d left out of the answer", clean, clean+leaked)
}

// An assistant that only answers is an instrument. One that raises
// something every time is worse than one that never does. Both are
// measured here, because widening the instruction for the first is what
// risks the second.
func TestANoteThatDisagreesIsRaised(t *testing.T) {
	env := provider(t)
	system := manner(t) + "\n\n" + memory.Offered(notes)

	var raised, silent int
	for _, c := range conflicts {
		answer := strings.ToLower(ask(t, env, system, c.Ask))

		var found bool
		for _, want := range c.Want {
			if strings.Contains(answer, strings.ToLower(want)) {
				found = true
				break
			}
		}
		if found {
			raised++
			continue
		}
		silent++
		t.Errorf("%-62q said nothing about %v:\n    %s", c.Ask, c.Want, answer)
	}

	var quiet, nagged int
	for _, intention := range harmless {
		answer := strings.ToLower(ask(t, env, system, intention))

		var found []string
		for _, alarm := range alarms {
			if strings.Contains(answer, strings.ToLower(alarm)) {
				found = append(found, alarm)
			}
		}
		if len(found) == 0 {
			quiet++
			continue
		}
		nagged++
		t.Errorf("%-62q raised %v with nothing to raise:\n    %s", intention, found, answer)
	}

	t.Logf("a note disagrees  : %d of %d raised", raised, raised+silent)
	t.Logf("nothing disagrees : %d of %d left alone", quiet, quiet+nagged)
}

// manner : The manner the server is actually configured to answer in.
//
// Not the default. These measure a prompt, and a prompt behaves differently
// under a different manner: an eval that passes under one while the server
// runs the other measures nothing. This was found the hard way -- the
// answering rule passed here and refused a direct hit in use.
func manner(t *testing.T) string {
	t.Helper()

	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name)
}

// provider : The real environment, or a skip when none is configured.
func provider(t *testing.T) *platformai.Environment {
	t.Helper()

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
	return env
}

// ask : What the assistant answers, given that system prompt.
func ask(t *testing.T, env *platformai.Environment, system, question string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{Prompt: question, SystemPrompt: system})
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
		t.Fatalf("%q got no usable answer", question)
	}
	return final.Text
}
