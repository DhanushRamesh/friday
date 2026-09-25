package failure_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/failure"
)

func TestEveryCodeHasASentence(t *testing.T) {
	codes := []failure.Code{
		failure.Unreachable, failure.Timeout, failure.Unauthorised,
		failure.Forbidden, failure.RateLimited, failure.Unavailable,
		failure.BadRequest, failure.TooLong, failure.BadResponse,
		failure.Unexpected,
	}
	for _, c := range codes {
		if !failure.Known(c) {
			t.Errorf("%q has no sentence", c)
		}
		// Read aloud, so no code, no status number, no jargon.
		if s := failure.Sentence(c); s == "" || strings.Contains(s, "_") {
			t.Errorf("sentence for %q is not speakable: %q", c, s)
		}
	}
}

func TestAnUnknownCodeFallsBackWithoutClaimingToBeKnown(t *testing.T) {
	// Falling back matters because this runs while something is already
	// broken: an unknown code must not become a second failure.
	if got := failure.Sentence("nonsense"); got != failure.Sentence(failure.Unexpected) {
		t.Errorf("sentence = %q, want the unexpected one", got)
	}
	// Known stays false so the caller can log it rather than lose it.
	if failure.Known("nonsense") {
		t.Error("an unknown code reported itself as known")
	}
}

func TestAStatusWithNoMeaningIsUnexpected(t *testing.T) {
	if got := failure.FromHTTP(http.StatusTeapot); got != failure.Unexpected {
		t.Errorf("418 mapped to %q, want unexpected", got)
	}
}

func TestTheDetailKeepsTheServicesOwnWords(t *testing.T) {
	f := failure.FromStatus(http.StatusUnauthorized, "  INVALID_OAUTHTOKEN  ", nil)

	if f.Code != failure.Unauthorised {
		t.Errorf("code = %q, want unauthorised", f.Code)
	}
	// The sentence is chosen by the code, never by what the service said:
	// INVALID_OAUTHTOKEN read aloud tells the listener nothing.
	if strings.Contains(f.Sentence(), "OAUTH") {
		t.Errorf("the raw error leaked into the sentence: %q", f.Sentence())
	}
	if f.Detail != "INVALID_OAUTHTOKEN" {
		t.Errorf("detail = %q, want it trimmed but otherwise exact", f.Detail)
	}
	if !strings.Contains(f.Full(), "401") {
		t.Errorf("full = %q, want the status in it", f.Full())
	}
}

func TestFullIsEmptyWhenThereIsNothingToAdd(t *testing.T) {
	// Empty rather than a placeholder, so a caller can tell the difference
	// between a failure that explains itself and one that does not.
	if got := failure.New(failure.Unreachable, "", nil).Full(); got != "" {
		t.Errorf("full = %q, want empty", got)
	}
}

func TestTheCauseStaysReachable(t *testing.T) {
	f := failure.New(failure.Timeout, "took too long", context.DeadlineExceeded)

	if !errors.Is(f, context.DeadlineExceeded) {
		t.Error("errors.Is cannot see through the failure to its cause")
	}
}
