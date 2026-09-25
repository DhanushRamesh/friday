package conversation_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// A bare label is kept as it is.
func TestAPlainLabelIsKept(t *testing.T) {
	for _, want := range []string{"Roof Quotes", "Bedroom Temperature", "Cricket"} {
		if got := conversation.CleanTitle(want); got != want {
			t.Errorf("CleanTitle(%q) = %q", want, got)
		}
	}
}

// A model asked for a bare label will decorate it anyway. Taking that
// literally puts the punctuation in the listing for ever.
func TestDecorationIsStripped(t *testing.T) {
	for given, want := range map[string]string{
		`"Roof Quotes"`:        "Roof Quotes",
		`Roof Quotes.`:         "Roof Quotes",
		`“Roof Quotes”`:        "Roof Quotes",
		`Title: Roof Quotes`:   "Roof Quotes",
		"  Roof Quotes  ":      "Roof Quotes",
		"Roof Quotes\nBecause": "Roof Quotes",
	} {
		if got := conversation.CleanTitle(given); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", given, got, want)
		}
	}
}

// An answer that is a sentence rather than a label is refused. No name reads
// better in a listing than an apology does.
func TestASentenceIsRefused(t *testing.T) {
	for _, given := range []string{
		"I would call this conversation Roof Quotes",
		"This conversation is about the roof and the quotes for it",
		"Certainly, here is a name for you",
		"",
		"   ",
		"?",
	} {
		if got := conversation.CleanTitle(given); got != "" {
			t.Errorf("CleanTitle(%q) = %q, want it refused", given, got)
		}
	}
}

// A label may be a question's subject without being a question.
func TestAQuestionMarkIsRefused(t *testing.T) {
	if got := conversation.CleanTitle("What is the time?"); got != "" {
		t.Errorf("CleanTitle = %q, want a question refused", got)
	}
}

// The exchange reaches the model, or it has nothing to name.
func TestTheTitlePromptCarriesTheExchange(t *testing.T) {
	prompt := conversation.TitlePrompt([]conversation.Message{
		said(conversation.User, "what did the roofer quote"),
		said(conversation.Assistant, "forty thousand rupees"),
	})

	for _, want := range []string{"what did the roofer quote", "forty thousand rupees", "Person", "Assistant"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not contain %q", want)
		}
	}
}

// The instruction asks for a label and refuses everything else, since a model
// answering in the assistant's own manner would address the person.
func TestTheTitlePromptAsksForALabel(t *testing.T) {
	prompt := conversation.TitlePrompt([]conversation.Message{said(conversation.User, "hello")})

	for _, want := range []string{"two to four words", "Not a summary", "do not address anyone"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

// What is said aloud names the conversation, since somebody listening has no
// listing to look at.
func TestTheAnnouncementNamesIt(t *testing.T) {
	got := conversation.TitleAnnouncement("Roof Quotes")
	if !strings.Contains(got, "Roof Quotes") {
		t.Errorf("announcement = %q, want it to say the name", got)
	}
	if !strings.HasSuffix(got, ".") {
		t.Errorf("announcement = %q, want it to end on a full stop so the microphone is not reopened", got)
	}
}
