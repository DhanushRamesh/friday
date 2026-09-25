package conversation_test

import (
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// call : A tool call, for a test that does not care about the arguments.
func call(id, name string) conversation.ToolCall {
	return conversation.ToolCall{ID: id, Name: name, Arguments: `{}`}
}

// result : A successful result answering the given call.
func result(id, name string) conversation.ToolResult {
	return conversation.ToolResult{ID: id, Name: name, Outcome: conversation.OutcomeOK, Content: "done"}
}

// Both constructed shapes have to be storable, or nothing can record a tool
// chain at all.
func TestConstructedToolMessagesAreValid(t *testing.T) {
	at := time.Now().UTC()
	for _, m := range []conversation.Message{
		conversation.CalledTools("conv_x", []conversation.ToolCall{call("c1", "conversation_list")}, at),
		conversation.ToolsReturned("conv_x", []conversation.ToolResult{result("c1", "conversation_list")}, at),
	} {
		if err := m.Valid(); err != nil {
			t.Errorf("%s message is not valid: %v", m.Role, err)
		}
	}
}

// A message is words, tool calls or tool results, and never two of them: a
// model that explains itself and acts in the same breath gives the person
// something to read that may not describe what happened.
func TestAMessageCarriesOneThing(t *testing.T) {
	at := time.Now().UTC()

	both := conversation.CalledTools("conv_x", []conversation.ToolCall{call("c1", "x")}, at)
	both.Content = "I will look that up"
	if err := both.Valid(); err == nil {
		t.Error("a message with words and tool calls was accepted")
	}

	neither := conversation.Message{ID: conversation.NewMessageID(), ConversationID: "conv_x",
		Kind: conversation.Chat, Role: conversation.Assistant, At: at}
	if err := neither.Valid(); err == nil {
		t.Error("a message with nothing in it was accepted")
	}
}

// Only the assistant asks, and only a tool answers. A result attributed to
// the assistant would read as something it knew rather than something it
// looked up.
func TestToolPayloadsBelongToTheirRoles(t *testing.T) {
	at := time.Now().UTC()

	asUser := conversation.CalledTools("conv_x", []conversation.ToolCall{call("c1", "x")}, at)
	asUser.Role = conversation.User
	if err := asUser.Valid(); err == nil {
		t.Error("the person was allowed to call a tool")
	}

	asAssistant := conversation.ToolsReturned("conv_x", []conversation.ToolResult{result("c1", "x")}, at)
	asAssistant.Role = conversation.Assistant
	if err := asAssistant.Valid(); err == nil {
		t.Error("the assistant was allowed to return a tool result")
	}
}

// A result has to say how it went. Without an outcome a model cannot tell a
// success from a failure, which is where an assistant starts reporting work
// it did not do.
func TestAResultNeedsAnOutcome(t *testing.T) {
	at := time.Now().UTC()
	m := conversation.ToolsReturned("conv_x", []conversation.ToolResult{
		{ID: "c1", Name: "x", Content: "something"},
	}, at)

	if err := m.Valid(); err == nil {
		t.Error("a result with no outcome was accepted")
	}
}

// Tool messages survive ForModel, which used to drop anything with no words
// in it.
func TestToolMessagesReachTheModel(t *testing.T) {
	at := time.Now().UTC()
	given := []conversation.Message{
		said(conversation.User, "what have we talked about"),
		conversation.CalledTools("conv_x", []conversation.ToolCall{call("c1", "conversation_list")}, at),
		conversation.ToolsReturned("conv_x", []conversation.ToolResult{result("c1", "conversation_list")}, at),
		said(conversation.Assistant, "three things"),
	}

	if got := conversation.ForModel(given); len(got) != 4 {
		t.Fatalf("gave the model %d messages, want all 4", len(got))
	}
}

// Two assistant messages in a row are joined, but never when one of them
// carries tool calls: the call and its answer are a pair, and a model reading
// them run together cannot tell which answer belongs to which call.
func TestAToolCallIsNotJoinedToProse(t *testing.T) {
	at := time.Now().UTC()
	given := []conversation.Message{
		said(conversation.Assistant, "let me look"),
		conversation.CalledTools("conv_x", []conversation.ToolCall{call("c1", "x")}, at),
	}

	got := conversation.ForModel(given)
	if len(got) != 2 {
		t.Fatalf("kept %d messages, want the prose and the call kept apart", len(got))
	}
	if len(got[1].ToolCalls) != 1 {
		t.Error("the tool call was merged away")
	}
}
