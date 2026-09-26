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
// the persona: the owner chooses what it is called and the same binary has to
// serve whatever that is.
func TestThePromptUsesTheConfiguredName(t *testing.T) {
	for name, want := range map[string]string{
		"Jarvis":  "You are Jarvis, a personal assistant.",
		"Friday":  "You are Friday, a personal assistant.",
		"  Ada  ": "You are Ada, a personal assistant.",
	} {
		if got := persona.Prompt("friday", name); !strings.HasPrefix(got, want) {
			t.Errorf("Prompt(friday, %q) = %q, want it to start %q", name, got, want)
		}
	}

	// Nameless is a working assistant, not a broken one: it simply never
	// says what it is called.
	if got := persona.Prompt("jarvis", ""); !strings.HasPrefix(got, "You are a personal assistant.") {
		t.Errorf("an unnamed assistant reads as %q", got)
	}
}

// Whatever the manner, the instructions that keep a reply speakable survive.
func TestEveryPromptStaysSpeakable(t *testing.T) {
	for _, p := range persona.All() {
		prompt := persona.Prompt(p.ID, "Jarvis")
		for _, must := range []string{"read aloud", "Do not use markdown"} {
			if !strings.Contains(prompt, must) {
				t.Errorf("%s lost %q", p.ID, must)
			}
		}
	}
}

// A nameless assistant answering plainly must not name itself. The name is
// configuration, and one written into the prompt would be wrong the moment it
// changed. A manner may name a character it is told never to mention, which
// is the opposite of hardcoding one.
func TestNoAssistantNameIsHardcoded(t *testing.T) {
	plain := persona.Prompt(persona.Default, "")
	for _, forbidden := range []string{"FRIDAY", "Friday", "JARVIS", "Jarvis"} {
		if strings.Contains(plain, forbidden) {
			t.Errorf("%q is hardcoded in %q", forbidden, plain)
		}
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

// The tools are the only way it acts, and it has to be told so. Without
// it, asked to add milk to a shopping list it has no tool for, it replied
// that milk had been added and called nothing.
func TestEveryPersonaIsToldItActsOnlyThroughTools(t *testing.T) {
	for _, p := range persona.All() {
		got := persona.Prompt(p.ID, "Jarvis")

		for _, want := range []string{
			"You act only through the tools you are given",
			"Never say you have done something",
			"say plainly that you cannot do it",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s is missing %q", p.ID, want)
			}
		}
	}
}

// A refusal can be worked around; a false success cannot even be noticed.
// The prompt says which is worse, because the model has to choose.
func TestItIsToldWhichFailureIsWorse(t *testing.T) {
	got := persona.Prompt(persona.Default, "Jarvis")

	if !strings.Contains(got, "always better than saying you have when you have not") {
		t.Errorf("the prompt does not say which way to err:\n%s", got)
	}
}
