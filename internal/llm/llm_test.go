package llm_test

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/llm"
)

// The model configuration actually selects has to be findable, or the window
// it declares is never applied.
func TestTheConfiguredModelIsKnown(t *testing.T) {
	model, ok := llm.Find("anthropic", "claude-sonnet-4-6")
	if !ok {
		t.Fatal("claude-sonnet-4-6 is not in the catalogue")
	}
	if model.ContextTokens <= 0 {
		t.Errorf("ContextTokens = %d, want the model's window", model.ContextTokens)
	}
}

// An identifier is only unique within a vendor, so the vendor is part of the
// key rather than a label beside it.
func TestTheVendorIsPartOfTheKey(t *testing.T) {
	if _, ok := llm.Find("openai", "claude-sonnet-4-6"); ok {
		t.Error("found an Anthropic model under OpenAI")
	}
	if _, ok := llm.Find("", "claude-sonnet-4-6"); !ok {
		t.Error("no vendor should match on the identifier alone")
	}
}

// A model nobody has catalogued is not an error. It declares no window, and
// the byte budget governs alone.
func TestAnUnknownModelDeclaresNothing(t *testing.T) {
	if _, ok := llm.Find("anthropic", "claude-from-the-future"); ok {
		t.Error("found a model that is not listed")
	}
	if got := llm.ContextTokens("anthropic", "claude-from-the-future"); got != 0 {
		t.Errorf("ContextTokens = %d, want 0 for an unknown model", got)
	}
}

// Every entry has to say something useful, since a zero window is
// indistinguishable from not being listed at all.
func TestEveryEntryIsUsable(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range llm.All() {
		switch {
		case m.ID == "":
			t.Error("a model has no identifier")
		case m.Vendor == "":
			t.Errorf("%s has no vendor", m.ID)
		case m.Name == "":
			t.Errorf("%s has no name", m.ID)
		case m.ContextTokens <= 0:
			t.Errorf("%s declares no context window, which is the same as not being listed", m.ID)
		}

		key := m.Vendor + "/" + m.ID
		if seen[key] {
			t.Errorf("%s is listed twice", key)
		}
		seen[key] = true
	}
}

// The listing is a copy, so a caller cannot edit the catalogue by accident.
func TestAllReturnsACopy(t *testing.T) {
	first := llm.All()
	if len(first) == 0 {
		t.Fatal("the catalogue is empty")
	}
	first[0].ContextTokens = 1

	if llm.All()[0].ContextTokens == 1 {
		t.Error("editing the listing changed the catalogue")
	}
}
