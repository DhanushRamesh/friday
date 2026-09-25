package session_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// turns : An alternating conversation of n messages, each of the given
// length, numbered from one.
func turns(n, size int) []session.Message {
	out := make([]session.Message, n)
	for i := range out {
		role := session.User
		if i%2 == 1 {
			role = session.Assistant
		}
		out[i] = said(role, strings.Repeat("a", size))
		out[i].Seq = i + 1
	}
	return out
}

// Nothing is dropped while the whole session fits.
func TestPlanKeepsASessionThatFits(t *testing.T) {
	messages := []session.Message{
		said(session.User, "what time is it"),
		said(session.Assistant, "half past two"),
	}

	window := session.Plan(messages, session.Summary{}, session.Limits{Bytes: 1000})

	if len(window.Messages) != 2 {
		t.Fatalf("kept %d messages, want both", len(window.Messages))
	}
	if window.Summary != "" {
		t.Errorf("summarised %q with nothing condensed", window.Summary)
	}
}

// The oldest go first: a follow-up refers to what was just said, not to what
// opened the session an hour ago.
func TestPlanDropsTheOldest(t *testing.T) {
	messages := []session.Message{
		said(session.User, strings.Repeat("a", 10)),
		said(session.Assistant, strings.Repeat("b", 10)),
		said(session.User, strings.Repeat("c", 10)),
	}

	window := session.Plan(messages, session.Summary{}, session.Limits{Bytes: 20})

	if len(window.Messages) != 2 {
		t.Fatalf("kept %d messages, want the last two", len(window.Messages))
	}
	if window.Messages[0].Content[0] != 'b' || window.Messages[1].Content[0] != 'c' {
		t.Errorf("kept %q and %q, want the last two", window.Messages[0].Content, window.Messages[1].Content)
	}
}

// A single turn longer than the whole budget is cut rather than dropped.
func TestPlanCutsATurnTooLongToFit(t *testing.T) {
	messages := []session.Message{said(session.User, strings.Repeat("a", 100))}

	window := session.Plan(messages, session.Summary{}, session.Limits{Bytes: 30})

	if len(window.Messages) != 1 {
		t.Fatalf("kept %d messages, want the one cut down", len(window.Messages))
	}
	if len(window.Messages[0].Content) != 30 {
		t.Errorf("kept %d characters, want 30", len(window.Messages[0].Content))
	}
}

// Planning an empty session is not an error, and asks for nothing.
func TestPlanOnAnEmptySession(t *testing.T) {
	if window := session.Plan(nil, session.Summary{}, session.Limits{Bytes: 100}); len(window.Messages) != 0 {
		t.Errorf("kept %d messages from nothing", len(window.Messages))
	}
}

// A service that takes only so many messages gets only so many, however
// small they are.
func TestPlanHoldsToTheCountLimit(t *testing.T) {
	window := session.Plan(turns(30, 4), session.Summary{}, session.Limits{Count: 10})

	if len(window.Messages) != 10 {
		t.Fatalf("sent %d messages, want 10", len(window.Messages))
	}
	if window.Messages[len(window.Messages)-1].Seq != 30 {
		t.Errorf("last message is seq %d, want the newest", window.Messages[len(window.Messages)-1].Seq)
	}
}

// What the summary accounts for is sent as the summary, not again as itself.
func TestPlanReplacesWhatIsSummarised(t *testing.T) {
	summary := session.Summary{Text: "they discussed the weather", ThroughSeq: 24}

	window := session.Plan(turns(30, 4), summary, session.Limits{})

	if window.Summary != summary.Text {
		t.Errorf("summary is %q, want it carried through", window.Summary)
	}
	if len(window.Messages) != 6 {
		t.Fatalf("sent %d messages, want the 6 after the watermark", len(window.Messages))
	}
	if window.Messages[0].Seq != 25 {
		t.Errorf("first message is seq %d, want 25", window.Messages[0].Seq)
	}
}

// Nothing is condensed while there is room, however many messages there are.
func TestDueWaitsUntilACeilingIsNear(t *testing.T) {
	if _, due := session.Due(turns(60, 10), session.Summary{}, session.Limits{Bytes: 60000}); due {
		t.Error("condensed a session with room to spare")
	}
}

// A short session is left alone even when its few messages are enormous:
// condensing it would leave nothing to condense into.
func TestDueLeavesAShortSessionAlone(t *testing.T) {
	if _, due := session.Due(turns(session.KeepVerbatim, 100000), session.Summary{}, session.Limits{}); due {
		t.Error("condensed a session of only a few messages")
	}
}

// The count ceiling triggers condensing just as the size ceiling does, so a
// service that limits messages rather than bytes is served too.
func TestDueTriggersOnTheCountLimit(t *testing.T) {
	through, due := session.Due(turns(95, 4), session.Summary{}, session.Limits{Count: 100})

	if !due {
		t.Fatal("did not condense at 95 of 100 messages")
	}
	// 95 less the 20 kept verbatim is seq 75, moved back one so the tail
	// opens with a question rather than an answer.
	if through != 74 {
		t.Errorf("condensing through seq %d, want 74", through)
	}
}

// The boundary lands where a turn begins, so a question is never condensed
// apart from the answer to it.
func TestDueCutsAtTheStartOfATurn(t *testing.T) {
	messages := turns(95, 4)

	through, due := session.Due(messages, session.Summary{}, session.Limits{Count: 100})
	if !due {
		t.Fatal("did not condense")
	}

	for _, m := range messages {
		if m.Seq == through+1 && m.Role != session.User {
			t.Errorf("the verbatim tail opens with %s at seq %d, want a question", m.Role, m.Seq)
		}
	}
}

// Only what the summary does not already cover counts towards a ceiling.
func TestDueIgnoresWhatIsAlreadySummarised(t *testing.T) {
	summary := session.Summary{Text: "the first ninety", ThroughSeq: 90}

	if _, due := session.Due(turns(95, 4), summary, session.Limits{Count: 100}); due {
		t.Error("condensed again with only five messages outstanding")
	}
}

// A model with a small context window overrides a larger budget of our own.
func TestPlanHoldsToTheModelsContextWindow(t *testing.T) {
	// 4096 tokens less the 2048 reserved, at four bytes each, is 8192 bytes:
	// two of these messages and not the third.
	limits := session.Limits{Bytes: session.DefaultBudget, ContextTokens: 4096}
	messages := []session.Message{
		said(session.User, strings.Repeat("a", 5000)),
		said(session.Assistant, strings.Repeat("b", 5000)),
		said(session.User, strings.Repeat("c", 3000)),
	}

	window := session.Plan(messages, session.Summary{}, limits)

	if len(window.Messages) != 2 {
		t.Fatalf("sent %d messages, want the 2 that fit the window", len(window.Messages))
	}
	if window.Messages[0].Content[0] != 'b' {
		t.Errorf("kept %q first, want the newer two", window.Messages[0].Content[:1])
	}
}

// A large context window does not licence sending more than our own budget.
func TestPlanKeepsOurBudgetUnderALargeWindow(t *testing.T) {
	limits := session.Limits{Bytes: 4000, ContextTokens: 128000}
	messages := []session.Message{
		said(session.User, strings.Repeat("a", 3000)),
		said(session.Assistant, strings.Repeat("b", 3000)),
	}

	window := session.Plan(messages, session.Summary{}, limits)

	if len(window.Messages) != 1 {
		t.Fatalf("sent %d messages, want the 1 our budget allows", len(window.Messages))
	}
}

// A context window no larger than the reserve still leaves room for a turn.
func TestPlanSurvivesAWindowSmallerThanTheReserve(t *testing.T) {
	limits := session.Limits{ContextTokens: 512}
	messages := []session.Message{said(session.User, "what time is it")}

	window := session.Plan(messages, session.Summary{}, limits)

	if len(window.Messages) != 1 {
		t.Fatalf("sent %d messages, want the question itself", len(window.Messages))
	}
}

// The model's window triggers condensing just as the other two ceilings do.
func TestDueTriggersOnTheContextWindow(t *testing.T) {
	// 4096 less the reserve is 8192 bytes; 30 messages of 300 is 9000.
	limits := session.Limits{ContextTokens: 4096}

	if _, due := session.Due(turns(30, 300), session.Summary{}, limits); !due {
		t.Error("did not condense a session overrunning the model's window")
	}
}
