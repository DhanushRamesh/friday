package memory_test

import (
	"strings"
	"testing"

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
