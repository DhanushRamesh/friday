package persona_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/persona"
)

// Every persona has to be describable, since a listing offers them by name
// and summary and an empty one is a row nobody can choose between.
func TestEveryPersonaIsDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range persona.All() {
		switch {
		case p.ID == "":
			t.Error("a persona has no identifier")
		case p.Name == "":
			t.Errorf("%s has no name", p.ID)
		case p.Summary == "":
			t.Errorf("%s has no summary", p.ID)
		}
		if seen[p.ID] {
			t.Errorf("%s is listed twice", p.ID)
		}
		seen[p.ID] = true
	}
}

// The default has to exist, or choosing nothing chooses something missing.
func TestTheDefaultExists(t *testing.T) {
	p, ok := persona.Find(persona.Default)
	if !ok {
		t.Fatalf("the default persona %q is not in the registry", persona.Default)
	}
	if p.Manner != "" {
		t.Error("the default asks for a manner, so an assistant has one before anyone chose it")
	}
}

// The spoken rules come last, so a manner cannot talk its way past them.
func TestTheSpokenRulesComeAfterTheManner(t *testing.T) {
	prompt := persona.Prompt("jarvis", "Jarvis")

	manner := strings.Index(prompt, "butler")
	rules := strings.Index(prompt, "read aloud")
	if manner < 0 || rules < 0 {
		t.Fatalf("prompt is missing the manner or the rules: %q", prompt)
	}
	if rules < manner {
		t.Error("the spoken rules are read before the manner, so the manner overrides them")
	}
}

// Whatever the manner asks for, a reply must not end on a question: Home
// Assistant reads the last character to decide whether to keep listening.
func TestEveryPersonaIsForbiddenToEndOnAQuestion(t *testing.T) {
	for _, p := range persona.All() {
		prompt := persona.Prompt(p.ID, "Jarvis")
		if !strings.Contains(prompt, "Never end a reply with a question") {
			t.Errorf("%s may end a reply with a question", p.ID)
		}
	}
}

// The assistant answers to the name it is configured with, not to the name of
// the persona.
func TestThePromptUsesTheConfiguredName(t *testing.T) {
	if got := persona.Prompt("friday", "Alfred"); !strings.Contains(got, "You are Alfred,") {
		t.Errorf("prompt does not introduce the assistant as Alfred: %q", got)
	}
	if got := persona.Prompt("jarvis", ""); !strings.HasPrefix(got, "You are a personal assistant.") {
		t.Errorf("an unnamed assistant reads as %q", got)
	}
}

// A persona nobody recognises answers plainly rather than stopping the
// server: a settings row with a stale word in it is not a reason to refuse
// to work.
func TestAnUnknownPersonaAnswersPlainly(t *testing.T) {
	if _, ok := persona.Find("alfred"); ok {
		t.Fatal("alfred should not be in the registry")
	}

	unknown := persona.Prompt("alfred", "Jarvis")
	plain := persona.Prompt(persona.Default, "Jarvis")
	if unknown != plain {
		t.Errorf("an unknown persona gives %q, want the plain prompt", unknown)
	}
}

// The two characterful ones have to be told the things that keep them from
// breaking character or performing it.
func TestTheCharactersAreHeldToTheirManner(t *testing.T) {
	for id, address := range map[string]string{"jarvis": "sir", "friday": "boss"} {
		p, ok := persona.Find(id)
		if !ok {
			t.Fatalf("%s is missing", id)
		}
		if !strings.Contains(p.Manner, address) {
			t.Errorf("%s is not told to say %q", id, address)
		}
		if !strings.Contains(p.Manner, "Do not act out a role") {
			t.Errorf("%s may perform its manner rather than have it", id)
		}
		if !strings.Contains(p.Manner, "Tony Stark") {
			t.Errorf("%s is not forbidden from mentioning the films", id)
		}
	}
}
