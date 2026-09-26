package memory_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/memory"
)

// A memory needs an owner, a tier, something to say what it is about, and
// something to remember.
func TestWhatAMemoryNeeds(t *testing.T) {
	for name, tc := range map[string]struct {
		user, subject, body string
		tier                memory.Tier
		want                error
	}{
		"no owner":     {"", "Birthday", "22 October 1999", memory.TierRecall, memory.ErrNoUser},
		"no subject":   {"usr_1", "  ", "22 October 1999", memory.TierRecall, memory.ErrNoSubject},
		"no body":      {"usr_1", "Birthday", "  ", memory.TierRecall, memory.ErrNoBody},
		"unknown tier": {"usr_1", "Birthday", "22 October 1999", memory.Tier("sometimes"), memory.ErrBadTier},
		"long subject": {"usr_1", strings.Repeat("a", memory.MaxSubject+1), "x", memory.TierRecall, memory.ErrSubjectTooLong},
		"long body":    {"usr_1", "Birthday", strings.Repeat("a", memory.MaxBody+1), memory.TierRecall, memory.ErrBodyTooLong},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := memory.New(tc.user, tc.tier, tc.subject, tc.body)
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// A good memory is stored with an identifier and both timestamps.
func TestAGoodMemoryIsReady(t *testing.T) {
	m, err := memory.New("usr_1", memory.TierAlways, " Birthday ", " 22 October 1999 ")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !memory.ValidID(m.ID) {
		t.Errorf("ID = %q, want a memory identifier", m.ID)
	}
	if m.Subject != "Birthday" || m.Body != "22 October 1999" {
		t.Errorf("stored %q / %q, want them trimmed", m.Subject, m.Body)
	}
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		t.Error("a new memory has no timestamps")
	}
}

// Subject and body are embedded together, because a question resembles a
// description of a fact more often than the fact itself.
func TestTextIsSubjectAndBody(t *testing.T) {
	m := memory.Memory{Subject: "Roof quote", Body: "forty thousand rupees"}
	if got, want := m.Text(), "Roof quote: forty thousand rupees"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

// Either half alone is still usable text rather than a stray colon.
func TestTextSurvivesAMissingHalf(t *testing.T) {
	if got := (&memory.Memory{Subject: "Roof quote"}).Text(); got != "Roof quote" {
		t.Errorf("Text = %q", got)
	}
	if got := (&memory.Memory{Body: "forty thousand"}).Text(); got != "forty thousand" {
		t.Errorf("Text = %q", got)
	}
}

// A vector belongs to the model that produced it. Comparing one from another
// model ranks everything wrongly without failing, so the name has to match.
func TestAVectorBelongsToItsModel(t *testing.T) {
	m := memory.Memory{EmbedModel: "bge", Embedding: []float32{1, 0}}

	if !m.Embedded("bge") {
		t.Error("a memory embedded by bge does not report itself embedded by bge")
	}
	if m.Embedded("other") {
		t.Error("a vector from bge was accepted as one from another model")
	}
	if m.Embedded("") {
		t.Error("a vector was accepted for a model with no name")
	}
	if (&memory.Memory{EmbedModel: "bge"}).Embedded("bge") {
		t.Error("a memory with no vector reports itself embedded")
	}
}

// An identifier is recognised by its shape, and nothing else is.
func TestOnlyAMemoryIdentifierIsValid(t *testing.T) {
	if !memory.ValidID(memory.NewID()) {
		t.Error("a fresh identifier is not valid")
	}
	for _, bad := range []string{"", "mem_", "conv_01M3D477HXQ4YNQX7BNXJZZCV0", "mem_nonsense"} {
		if memory.ValidID(bad) {
			t.Errorf("%q was accepted as a memory identifier", bad)
		}
	}
}

// Both tiers are known and nothing else is.
func TestTheTiers(t *testing.T) {
	for _, tier := range memory.Tiers() {
		if !tier.Valid() {
			t.Errorf("%q is offered but not valid", tier)
		}
	}
	if memory.Tier("sometimes").Valid() {
		t.Error("an unknown tier was accepted")
	}
}
