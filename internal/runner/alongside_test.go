package runner

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// What goes alongside the history is measured from the prompt that will
// actually be sent.
//
// It used to be measured from the persona alone, which left out everything
// composed on per chat -- where the assistant is, the warning that a turn was
// spoken, and now the memories. The reserve was short by whatever those came
// to, so the conversation sent was longer than the budget allowed for.
func TestAlongsideMeasuresThePromptThatIsSent(t *testing.T) {
	r := &Runner{}

	short := r.alongside(&chat.Chat{}, "a short prompt")
	long := r.alongside(&chat.Chat{}, "a short prompt"+strings.Repeat("x", 500))

	if long-short != 500 {
		t.Errorf("500 more bytes of prompt changed the reserve by %d, want 500", long-short)
	}
}

// An empty prompt costs nothing, so a runner with no persona and no memory
// reserves only for its tools.
func TestAlongsideOfNothingIsNothing(t *testing.T) {
	r := &Runner{}

	if got := r.alongside(&chat.Chat{}, ""); got != 0 {
		t.Errorf("alongside = %d, want 0 with no prompt and no tools", got)
	}
}

// Only the parts that are not empty are joined, and they are separated so
// the model does not read two instructions as one sentence.
func TestJoinSkipsWhatIsEmpty(t *testing.T) {
	if got := join("one", "", "two"); got != "one\n\ntwo" {
		t.Errorf("join = %q", got)
	}
	if got := join("", ""); got != "" {
		t.Errorf("join = %q, want empty", got)
	}
	if got := join("only"); got != "only" {
		t.Errorf("join = %q", got)
	}
}
