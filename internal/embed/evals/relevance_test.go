//go:build evals

// Package evals asks a real model whether it can tell a relevant memory from
// an irrelevant one.
//
// Comparing vectors narrows a large store to a few candidates but cannot say
// whether any of them belongs: a question with nothing stored about it still
// has a nearest memory, and it scores in the same range as a real match. The
// question here is whether the model can make that judgement itself, which
// would mean no second model is needed to make it.
//
// Behind a build tag because it costs real calls to a real service.
package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/embed/tei"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
)

// memories : What is stored, as a memory would be worded.
var memories = []string{
	"Birthday is 22 October 1999.",
	"Lives in Chennai.",
	"Grocery list for 7 September 2020: cutting board 298, cup 409, containers 578, happy planet 1523, dry fruits 1476, soap 300. Total 5304 rupees.",
	"Wants answers kept short and plain, without preamble.",
	"Orders from Swiggy Instamart, Flipkart and Meesho.",
	"Favourite band is Owl City, whose singer is Adam Young.",
	"The Home Assistant voice satellite runs on the laptop and answers to Jarvis.",
	"Auto fares are usually between 165 and 700 rupees.",
	"The roofer quoted forty thousand for the terrace work.",
	"Goes to bed around 2:24 am and needs eight hours of sleep.",
	"Follows cricket closely, especially Kapil Dev and MS Dhoni.",
	"Bought an air conditioner after Carrier was recommended as the top brand.",
}

// relevant : Questions whose answer is stored, and which memory answers them.
var relevant = map[string]int{
	"when was I born":                         0,
	"which city is home for me":               1,
	"how much did that shopping trip come to": 2,
	"how should you talk to me":               3,
	"which apps do I buy things through":      4,
	"what music do I enjoy":                   5,
	"where does my speech setup run":          6,
	"what do I normally pay a rickshaw":       7,
	"what did the builder charge upstairs":    8,
	"when should I be waking up":              9,
	"which sport am I interested in":          10,
	"what cooling appliance did I get":        11,
}

// irrelevant : Questions with nothing stored about them. Every one of these
// still has a nearest memory, and that is the whole difficulty.
var irrelevant = []string{
	"what is the capital of Peru",
	"explain how a diesel engine works",
	"write a python function to reverse a list",
	"who won the Nobel prize for physics in 1998",
	"what is the boiling point of mercury",
	"summarise the plot of Hamlet",
	"how do I fix a 502 from nginx",
	"what time does the sun set in Reykjavik",
}

// shortlistSize : How many candidates the model is shown.
const shortlistSize = 3

// judgePrompt : What the model is asked, given a question and some notes.
func judgePrompt(question string, notes []string) string {
	var b strings.Builder
	b.WriteString("Here are some notes that may or may not have anything to do with a question.\n\n")
	for i, n := range notes {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(n)
		b.WriteString("\n")
	}
	b.WriteString("\nThe question: ")
	b.WriteString(question)
	b.WriteString("\n\nWhich note, if any, actually helps answer that question?\n")
	b.WriteString("Most of the time none of them will, and saying so is the right answer.\n")
	b.WriteString("A note only counts if it contains what is being asked for, not merely a related subject.\n")
	b.WriteString("Reply with the number alone, or the word none. Nothing else.\n")
	return b.String()
}

func TestTheModelCanTellRelevantFromIrrelevant(t *testing.T) {
	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	embedder := tei.New(tei.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if _, err := embedder.Check(ctx); err != nil {
		cancel()
		t.Skipf("no embedding server to shortlist with: %v", err)
	}
	cancel()

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

	stored, err := embedder.Documents(context.Background(), memories)
	if err != nil {
		t.Fatalf("embedding the memories: %v", err)
	}

	// Questions whose answer is stored: the model should find it.
	var found, missed int
	for question, want := range relevant {
		notes, idx := shortlist(t, embedder, stored, question)
		pick := judge(t, env, question, notes)
		switch {
		case pick < 0:
			missed++
			t.Errorf("%-42q said none, want %q", question, memories[want])
		case idx[pick] != want:
			missed++
			t.Errorf("%-42q chose %q, want %q", question, memories[idx[pick]], memories[want])
		default:
			found++
		}
	}

	// Questions with nothing stored: the model should say so.
	var quiet, noisy int
	for _, question := range irrelevant {
		notes, idx := shortlist(t, embedder, stored, question)
		if pick := judge(t, env, question, notes); pick >= 0 {
			noisy++
			t.Errorf("%-42q chose %q, want none", question, memories[idx[pick]])
		} else {
			quiet++
		}
	}

	t.Logf("answer stored     : %d of %d found", found, found+missed)
	t.Logf("nothing stored    : %d of %d correctly left alone", quiet, quiet+noisy)
}

// shortlist : The nearest memories to a question, and where they came from.
func shortlist(t *testing.T, e *tei.Client, stored []embed.Vector, question string) ([]string, []int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	q, err := e.Query(ctx, question)
	if err != nil {
		t.Fatalf("embedding %q: %v", question, err)
	}

	type scored struct {
		at    int
		score float64
	}
	all := make([]scored, 0, len(stored))
	for i, v := range stored {
		all = append(all, scored{at: i, score: embed.Similarity(q, v)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })

	notes := make([]string, 0, shortlistSize)
	idx := make([]int, 0, shortlistSize)
	for _, s := range all[:shortlistSize] {
		notes = append(notes, memories[s.at])
		idx = append(idx, s.at)
	}
	return notes, idx
}

// judge : Which note the model picked, or -1 for none.
func judge(t *testing.T, env *platformai.Environment, question string, notes []string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{
		Prompt:  judgePrompt(question, notes),
		Purpose: environment.PurposeCondense,
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
		t.Fatalf("%q got no usable answer", question)
	}

	answer := strings.ToLower(strings.TrimSpace(final.Text))
	if strings.HasPrefix(answer, "none") {
		return -1
	}
	for i := range notes {
		if strings.HasPrefix(answer, strconv.Itoa(i+1)) {
			return i
		}
	}
	return -1
}
