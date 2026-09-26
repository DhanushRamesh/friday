package memory_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/memory"
)

// Nothing stored adds nothing to the prompt, rather than an empty heading.
func TestNothingProducesNoBlock(t *testing.T) {
	if got := memory.Standing(nil); got != "" {
		t.Errorf("Standing = %q, want empty", got)
	}
	if got := memory.Offered(nil); got != "" {
		t.Errorf("Offered = %q, want empty", got)
	}
}

// What is always known is stated plainly, one per line.
func TestStandingListsWhatIsKnown(t *testing.T) {
	got := memory.Standing([]memory.Memory{
		{Subject: "Birthday", Body: "22 October 1999"},
		{Subject: "Home", Body: "Chennai"},
	})

	for _, want := range []string{"22 October 1999", "Chennai"} {
		if !strings.Contains(got, want) {
			t.Errorf("Standing is missing %q:\n%s", want, got)
		}
	}
	if lines := strings.Count(got, "\n- "); lines != 2 {
		t.Errorf("got %d listed facts, want 2", lines)
	}
}

// The offered notes say that none of them fitting is the usual case.
//
// This is the whole of what makes them safe. A question with nothing stored
// about it still has a nearest memory, and it scores in the same range as a
// real match, so without being told the model treats a near miss as an
// answer.
func TestOfferedSaysTheyMayNotFit(t *testing.T) {
	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Roof quote", Body: "forty thousand"}},
	})

	for _, want := range []string{
		"most of the time none of them will",
		"ignoring all of them",
		"rather than merely a related subject",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Offered is missing %q:\n%s", want, got)
		}
	}
}

// A note that was not used is not mentioned, or the answer becomes a report
// about the assistant's filing.
func TestOfferedForbidsTalkingAboutUnusedNotes(t *testing.T) {
	got := memory.Offered([]memory.Match{{Memory: memory.Memory{Subject: "A", Body: "b"}}})

	if !strings.Contains(got, "Do not mention a note you did not use") {
		t.Errorf("Offered lets the assistant talk about notes it ignored:\n%s", got)
	}
}

// Every offered memory is in the block.
func TestOfferedCarriesEveryNote(t *testing.T) {
	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Roof", Body: "forty thousand"}},
		{Memory: memory.Memory{Subject: "Cricket", Body: "Kapil Dev"}},
		{Memory: memory.Memory{Subject: "Home", Body: "Chennai"}},
	})

	for _, want := range []string{"forty thousand", "Kapil Dev", "Chennai"} {
		if !strings.Contains(got, want) {
			t.Errorf("Offered is missing %q", want)
		}
	}
}

// IDs are taken in the order they were matched, so what was offered can be
// recorded as used.
func TestIDsFollowTheMatches(t *testing.T) {
	got := memory.IDs([]memory.Match{
		{Memory: memory.Memory{ID: "mem_1"}},
		{Memory: memory.Memory{ID: "mem_2"}},
	})

	if len(got) != 2 || got[0] != "mem_1" || got[1] != "mem_2" {
		t.Errorf("IDs = %v", got)
	}
}

// The licence to warn is narrow on purpose. Unbounded, it produces an
// assistant that second-guesses every sentence; without it, one that
// watches somebody walk into something it knew about.
func TestOfferedPermitsAWarningAndBoundsIt(t *testing.T) {
	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Roof quote", Body: "Agreed forty thousand."}},
	})

	for _, want := range []string{
		"says what they are about to do",
		"a note disagrees with it",
		"one short line",
		"say what it is you are going on",
		"not where it merely shares a subject",
		"not where it agrees with what they intend",
		"invent no concern it does not support",
		"Most turns need no such line",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the licence to warn is missing %q:\n%s", want, got)
		}
	}
}

// Warning and answering are told apart, because the rule for one forbids
// the other.
func TestAnsweringAndWarningAreSeparateInstructions(t *testing.T) {
	got := memory.Offered([]memory.Match{{Memory: memory.Memory{Subject: "A", Body: "b"}}})

	answer := strings.Index(got, "To answer with:")
	warn := strings.Index(got, "To warn with:")
	if answer < 0 || warn < 0 {
		t.Fatalf("the two jobs are not named separately:\n%s", got)
	}
	if warn < answer {
		t.Error("warning is stated before answering, which is not the common case")
	}
}

// The transcript carries no licence to warn. It contains whatever
// speech-to-text got wrong, and a warning founded on a misheard sentence is
// worse than no warning at all.
func TestTheTranscriptMayNotWarn(t *testing.T) {
	got := memory.Quoted([]memory.Heard{{
		Exchange: memory.Exchange{Text: "They said: something", At: time.Now()},
	}})

	if strings.Contains(got, "To warn with:") {
		t.Errorf("the transcript was given the licence to warn:\n%s", got)
	}
	if !strings.Contains(got, "never state one as a fact of your own") {
		t.Errorf("the transcript lost its quoting rule:\n%s", got)
	}
}
