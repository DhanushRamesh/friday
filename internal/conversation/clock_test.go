package conversation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// The assistant is told the time, the day and the date, because it knows
// none of them and will otherwise answer from whenever it was trained.
func TestNowCarriesTheWholeMoment(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)
	got := conversation.Now(time.Date(2026, 9, 26, 15, 42, 0, 0, india))

	for _, want := range []string{"3:42 pm", "Saturday", "26 September 2026", "IST+05:30"} {
		if !strings.Contains(got, want) {
			t.Errorf("Now is missing %q:\n%s", want, got)
		}
	}
}

// Told the time, it is also told to use it rather than guess.
func TestNowForbidsGuessing(t *testing.T) {
	got := conversation.Now(time.Now())

	if !strings.Contains(got, "never guess the date") {
		t.Errorf("Now does not forbid guessing:\n%s", got)
	}
}

// Midnight and noon are the two a twelve-hour clock gets wrong.
func TestNowGetsMidnightAndNoonRight(t *testing.T) {
	midnight := conversation.Now(time.Date(2026, 9, 26, 0, 5, 0, 0, time.UTC))
	if !strings.Contains(midnight, "12:05 am") {
		t.Errorf("midnight read as %q", midnight)
	}

	noon := conversation.Now(time.Date(2026, 9, 26, 12, 5, 0, 0, time.UTC))
	if !strings.Contains(noon, "12:05 pm") {
		t.Errorf("noon read as %q", noon)
	}
}
