package session_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// The exchange to fold in is in the prompt, labelled by who said it.
func TestCondensePromptCarriesTheExchange(t *testing.T) {
	messages := []session.Message{
		said(session.User, "book the table for four"),
		said(session.Assistant, "booked for four at eight"),
	}

	prompt := session.CondensePrompt("", messages)

	for _, want := range []string{"book the table for four", "booked for four at eight", "Person", "Assistant"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not contain %q", want)
		}
	}
}

// Notes already kept are given back, so condensing builds on them rather than
// starting again from what is left.
func TestCondensePromptCarriesTheExistingNotes(t *testing.T) {
	prompt := session.CondensePrompt("They agreed on the roof.", []session.Message{
		said(session.User, "and the gutters"),
	})

	if !strings.Contains(prompt, "They agreed on the roof.") {
		t.Error("prompt does not carry the notes so far")
	}
}

// A session condensed for the first time says so, rather than leaving the
// model to guess what an empty section means.
func TestCondensePromptSaysWhenThereAreNoNotesYet(t *testing.T) {
	prompt := session.CondensePrompt("   ", []session.Message{
		said(session.User, "hello"),
	})

	if !strings.Contains(prompt, "(none yet)") {
		t.Error("prompt does not say there are no notes yet")
	}
}

// The instruction says what to produce and how long, so the result is notes
// rather than an answer in the assistant's voice.
func TestCondensePromptAsksForNotesOfABoundedLength(t *testing.T) {
	prompt := session.CondensePrompt("", []session.Message{said(session.User, "hello")})

	if !strings.Contains(prompt, "300 words") {
		t.Errorf("prompt does not bound the length to %d words", session.CondenseWords)
	}
	if !strings.Contains(prompt, "Do not answer") {
		t.Error("prompt does not tell the model to write notes rather than answer")
	}
}
