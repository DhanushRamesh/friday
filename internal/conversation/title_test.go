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

// The assistant has to be told where it is. Asked outright and not told, it
// answers from what it remembers doing, and having switched somewhere is not
// the same as being there.
func TestWhereaboutsNamesTheConversation(t *testing.T) {
	got := conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "Dhoni Test")

	for _, want := range []string{"Dhoni Test", "conv_01M3D477HXQ4YNQX7BNXJZZCV0"} {
		if !strings.Contains(got, want) {
			t.Errorf("whereabouts = %q, want it to contain %q", got, want)
		}
	}
}

// An unnamed conversation is said to be unnamed rather than left blank, since
// a gap is what invites a guess.
func TestWhereaboutsSaysWhenThereIsNoName(t *testing.T) {
	got := conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "  ")

	if !strings.Contains(got, "no name yet") {
		t.Errorf("whereabouts = %q, want it to say the conversation is unnamed", got)
	}
	if !strings.Contains(got, "do not guess") {
		t.Errorf("whereabouts = %q, want it told not to invent a name", got)
	}
}

// A chat belonging to no conversation says nothing, rather than describing a
// conversation that is not there.
func TestWhereaboutsIsSilentWithoutAConversation(t *testing.T) {
	if got := conversation.Whereabouts("", "Dhoni Test"); got != "" {
		t.Errorf("whereabouts = %q, want nothing", got)
	}
}

// A spoken turn warns the model that the words may be the wrong ones, since
// speech-to-text does not misspell: it substitutes a word that sounds alike
// and leaves the sentence reading correctly.
func TestHeardWarnsAboutMishearing(t *testing.T) {
	got := conversation.Heard()

	for _, want := range []string{"sounds like", "Read for"} {
		if !strings.Contains(got, want) {
			t.Errorf("heard = %q, want it to mention %q", got, want)
		}
	}
}

// Reading charitably is right until a charitable reading destroys something.
func TestHeardStopsShortOfGuessingAtDestruction(t *testing.T) {
	got := conversation.Heard()

	if !strings.Contains(got, "ask which was meant") {
		t.Errorf("heard = %q, want it told to ask rather than guess", got)
	}
	if !strings.Contains(got, "destroys") {
		t.Errorf("heard = %q, want the caution tied to destructive readings", got)
	}
}
