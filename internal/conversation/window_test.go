package conversation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// turns : An alternating conversation of n messages, each of the given
// length, numbered from one.
func turns(n, size int) []conversation.Message {
	out := make([]conversation.Message, n)
	for i := range out {
		role := conversation.User
		if i%2 == 1 {
			role = conversation.Assistant
		}
		out[i] = said(role, strings.Repeat("a", size))
		out[i].Seq = i + 1
	}
	return out
}

// Nothing is dropped while the whole conversation fits.
func TestPlanKeepsAConversationThatFits(t *testing.T) {
	messages := []conversation.Message{
		said(conversation.User, "what time is it"),
		said(conversation.Assistant, "half past two"),
	}

	window := conversation.Plan(messages, conversation.Summary{}, conversation.Limits{Bytes: 1000})

	if len(window.Messages) != 2 {
		t.Fatalf("kept %d messages, want both", len(window.Messages))
	}
	if window.Summary != "" {
		t.Errorf("summarised %q with nothing condensed", window.Summary)
	}
}

// The oldest go first: a follow-up refers to what was just said, not to what
// opened the conversation an hour ago.
func TestPlanDropsTheOldest(t *testing.T) {
	messages := []conversation.Message{
		said(conversation.User, strings.Repeat("a", 10)),
		said(conversation.Assistant, strings.Repeat("b", 10)),
		said(conversation.User, strings.Repeat("c", 10)),
	}

	window := conversation.Plan(messages, conversation.Summary{}, conversation.Limits{Bytes: 20})

	if len(window.Messages) != 2 {
		t.Fatalf("kept %d messages, want the last two", len(window.Messages))
	}
	if window.Messages[0].Content[0] != 'b' || window.Messages[1].Content[0] != 'c' {
		t.Errorf("kept %q and %q, want the last two", window.Messages[0].Content, window.Messages[1].Content)
	}
}

// A single turn longer than the whole budget is cut rather than dropped.
func TestPlanCutsATurnTooLongToFit(t *testing.T) {
	messages := []conversation.Message{said(conversation.User, strings.Repeat("a", 100))}

	window := conversation.Plan(messages, conversation.Summary{}, conversation.Limits{Bytes: 30})

	if len(window.Messages) != 1 {
		t.Fatalf("kept %d messages, want the one cut down", len(window.Messages))
	}
	if len(window.Messages[0].Content) != 30 {
		t.Errorf("kept %d characters, want 30", len(window.Messages[0].Content))
	}
}

// Planning an empty conversation is not an error, and asks for nothing.
func TestPlanOnAnEmptyConversation(t *testing.T) {
	if window := conversation.Plan(nil, conversation.Summary{}, conversation.Limits{Bytes: 100}); len(window.Messages) != 0 {
		t.Errorf("kept %d messages from nothing", len(window.Messages))
	}
}

// A service that takes only so many messages gets only so many, however
// small they are, and one place is left for the prompt.
func TestPlanHoldsToTheCountLimit(t *testing.T) {
	window := conversation.Plan(turns(30, 4), conversation.Summary{}, conversation.Limits{Count: 10})

	if len(window.Messages) != 9 {
		t.Fatalf("sent %d messages, want 9 so the prompt makes 10", len(window.Messages))
	}
	if window.Messages[len(window.Messages)-1].Seq != 30 {
		t.Errorf("last message is seq %d, want the newest", window.Messages[len(window.Messages)-1].Seq)
	}
}

// What the summary accounts for is sent as the summary, not again as itself.
func TestPlanReplacesWhatIsSummarised(t *testing.T) {
	summary := conversation.Summary{Text: "they discussed the weather", ThroughSeq: 24}

	window := conversation.Plan(turns(30, 4), summary, conversation.Limits{})

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
	if _, due := conversation.Due(turns(60, 10), conversation.Summary{}, conversation.Limits{Bytes: 60000}); due {
		t.Error("condensed a conversation with room to spare")
	}
}

// A short conversation is left alone even when its few messages are enormous:
// condensing it would leave nothing to condense into.
func TestDueLeavesAShortConversationAlone(t *testing.T) {
	if _, due := conversation.Due(turns(conversation.KeepVerbatim, 100000), conversation.Summary{}, conversation.Limits{}); due {
		t.Error("condensed a conversation of only a few messages")
	}
}

// The count ceiling triggers condensing just as the size ceiling does, so a
// service that limits messages rather than bytes is served too.
func TestDueTriggersOnTheCountLimit(t *testing.T) {
	through, due := conversation.Due(turns(95, 4), conversation.Summary{}, conversation.Limits{Count: 100})

	if !due {
		t.Fatal("did not condense at 95 of 100 messages")
	}
	// 95 less the 45 kept verbatim is seq 50, which already opens a turn.
	if through != 50 {
		t.Errorf("condensing through seq %d, want 50", through)
	}
}

// A service that will not take as many messages as the prompt needs is left
// with no history rather than a request it refuses.
func TestPlanLeavesRoomForThePromptEvenAtOne(t *testing.T) {
	window := conversation.Plan(turns(30, 4), conversation.Summary{}, conversation.Limits{Count: 1})

	if len(window.Messages) != 0 {
		t.Errorf("sent %d messages, want none so the prompt fits alone", len(window.Messages))
	}
}

// The boundary lands where a turn begins, so a question is never condensed
// apart from the answer to it.
func TestDueCutsAtTheStartOfATurn(t *testing.T) {
	messages := turns(95, 4)

	through, due := conversation.Due(messages, conversation.Summary{}, conversation.Limits{Count: 100})
	if !due {
		t.Fatal("did not condense")
	}

	for _, m := range messages {
		if m.Seq == through+1 && m.Role != conversation.User {
			t.Errorf("the verbatim tail opens with %s at seq %d, want a question", m.Role, m.Seq)
		}
	}
}

// Only what the summary does not already cover counts towards a ceiling.
func TestDueIgnoresWhatIsAlreadySummarised(t *testing.T) {
	summary := conversation.Summary{Text: "the first ninety", ThroughSeq: 90}

	if _, due := conversation.Due(turns(95, 4), summary, conversation.Limits{Count: 100}); due {
		t.Error("condensed again with only five messages outstanding")
	}
}

// A model with a small context window overrides a larger budget of our own.
func TestPlanHoldsToTheModelsContextWindow(t *testing.T) {
	// 4096 tokens less the 2048 reserved, at four bytes each, is 8192 bytes:
	// two of these messages and not the third.
	limits := conversation.Limits{Bytes: conversation.DefaultBudget, ContextTokens: 4096}
	messages := []conversation.Message{
		said(conversation.User, strings.Repeat("a", 5000)),
		said(conversation.Assistant, strings.Repeat("b", 5000)),
		said(conversation.User, strings.Repeat("c", 3000)),
	}

	window := conversation.Plan(messages, conversation.Summary{}, limits)

	if len(window.Messages) != 2 {
		t.Fatalf("sent %d messages, want the 2 that fit the window", len(window.Messages))
	}
	if window.Messages[0].Content[0] != 'b' {
		t.Errorf("kept %q first, want the newer two", window.Messages[0].Content[:1])
	}
}

// A large context window does not licence sending more than our own budget.
func TestPlanKeepsOurBudgetUnderALargeWindow(t *testing.T) {
	limits := conversation.Limits{Bytes: 4000, ContextTokens: 128000}
	messages := []conversation.Message{
		said(conversation.User, strings.Repeat("a", 3000)),
		said(conversation.Assistant, strings.Repeat("b", 3000)),
	}

	window := conversation.Plan(messages, conversation.Summary{}, limits)

	if len(window.Messages) != 1 {
		t.Fatalf("sent %d messages, want the 1 our budget allows", len(window.Messages))
	}
}

// A context window no larger than the reserve still leaves room for a turn.
func TestPlanSurvivesAWindowSmallerThanTheReserve(t *testing.T) {
	limits := conversation.Limits{ContextTokens: 512}
	messages := []conversation.Message{said(conversation.User, "what time is it")}

	window := conversation.Plan(messages, conversation.Summary{}, limits)

	if len(window.Messages) != 1 {
		t.Fatalf("sent %d messages, want the question itself", len(window.Messages))
	}
}

// The model's window triggers condensing just as the other two ceilings do.
func TestDueTriggersOnTheContextWindow(t *testing.T) {
	// 4096 less the reserve is 8192 bytes; 60 messages of 300 is 18000.
	limits := conversation.Limits{ContextTokens: 4096}

	if _, due := conversation.Due(turns(60, 300), conversation.Summary{}, limits); !due {
		t.Error("did not condense a conversation overrunning the model's window")
	}
}

// A window trimmed by size can cut an assistant's tool calls away and leave
// the answers behind them. A service rejects a result that answers nothing,
// and a model reading one has a fact with no account of where it came from.
func TestATrimmedWindowDropsOrphanedToolResults(t *testing.T) {
	at := time.Now().UTC()
	long := strings.Repeat("a", 400)

	messages := []conversation.Message{
		{Seq: 1, Kind: conversation.Chat, Role: conversation.User, Content: long},
		conversation.CalledTools("conv_x", []conversation.ToolCall{
			{ID: "c1", Name: "conversation_list", Arguments: "{}"}}, at),
		conversation.ToolsReturned("conv_x", []conversation.ToolResult{
			{ID: "c1", Name: "conversation_list", Outcome: conversation.OutcomeOK, Content: long}}, at),
		{Seq: 4, Kind: conversation.Chat, Role: conversation.Assistant, Content: long},
		{Seq: 5, Kind: conversation.Chat, Role: conversation.User, Content: long},
	}
	for i := range messages {
		messages[i].Seq = i + 1
		messages[i].ConversationID = "conv_x"
	}

	// Small enough that the front of the conversation, including the call, is
	// cut away.
	window := conversation.Plan(messages, conversation.Summary{}, conversation.Limits{Bytes: 900})

	var asked, answered bool
	for _, m := range window.Messages {
		if len(m.ToolCalls) > 0 {
			asked = true
		}
		if len(m.ToolResults) > 0 {
			answered = true
		}
	}
	if answered && !asked {
		t.Error("a tool result was sent without the call that asked for it")
	}
}

// When both survive, both are sent: dropping a pair that fits would lose the
// only record of what the assistant actually did.
func TestAWholeToolPairSurvives(t *testing.T) {
	at := time.Now().UTC()
	messages := []conversation.Message{
		{Seq: 1, ConversationID: "conv_x", Kind: conversation.Chat, Role: conversation.User, Content: "what have we talked about"},
		conversation.CalledTools("conv_x", []conversation.ToolCall{
			{ID: "c1", Name: "conversation_list", Arguments: "{}"}}, at),
		conversation.ToolsReturned("conv_x", []conversation.ToolResult{
			{ID: "c1", Name: "conversation_list", Outcome: conversation.OutcomeOK, Content: "three"}}, at),
	}
	for i := range messages {
		messages[i].Seq = i + 1
	}

	window := conversation.Plan(messages, conversation.Summary{}, conversation.Limits{})

	if len(window.Messages) != 3 {
		t.Fatalf("sent %d messages, want the question, the call and the answer", len(window.Messages))
	}
}
