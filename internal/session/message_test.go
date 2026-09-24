package session_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// said : A message of the given role and length, so a test can say how much
// budget it consumes without writing out the text.
func said(role session.Role, text string) session.Message {
	return session.Message{Role: role, Content: text, At: time.Now()}
}

// Nothing is dropped while the whole session fits.
func TestWithinKeepsASessionThatFits(t *testing.T) {
	messages := []session.Message{
		said(session.User, "what time is it"),
		said(session.Assistant, "half past two"),
	}

	kept := session.Within(messages, 1000)

	if len(kept) != 2 {
		t.Fatalf("kept %d messages, want both", len(kept))
	}
}

// The oldest go first: a follow-up refers to what was just said, not to what
// opened the session an hour ago.
func TestWithinDropsTheOldest(t *testing.T) {
	messages := []session.Message{
		said(session.User, strings.Repeat("a", 10)),
		said(session.Assistant, strings.Repeat("b", 10)),
		said(session.User, strings.Repeat("c", 10)),
	}

	kept := session.Within(messages, 20)

	if len(kept) != 2 {
		t.Fatalf("kept %d messages, want the last two", len(kept))
	}
	if kept[0].Content[0] != 'b' || kept[1].Content[0] != 'c' {
		t.Errorf("kept %q and %q, want the last two", kept[0].Content, kept[1].Content)
	}
}

// A single turn longer than the whole budget is cut rather than dropped:
// dropping it would leave the model answering about a subject it never saw.
func TestWithinCutsATurnTooLongToFit(t *testing.T) {
	messages := []session.Message{said(session.User, strings.Repeat("a", 100))}

	kept := session.Within(messages, 30)

	if len(kept) != 1 {
		t.Fatalf("kept %d messages, want the one cut down", len(kept))
	}
	if len(kept[0].Content) != 30 {
		t.Errorf("kept %d characters, want 30", len(kept[0].Content))
	}
}

// Trimming an empty session is not an error, and asks for nothing.
func TestWithinOnAnEmptySession(t *testing.T) {
	if kept := session.Within(nil, 100); len(kept) != 0 {
		t.Errorf("kept %d messages from nothing", len(kept))
	}
}

// Joining two questions keeps the time of the earlier one: that is when the
// speaker started saying all of it.
func TestForModelKeepsTheEarlierTime(t *testing.T) {
	first := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	messages := []session.Message{
		{Kind: session.Chat, Role: session.User, Content: "list three languages", At: first},
		{Kind: session.Chat, Role: session.User, Content: "no, make it four", At: second},
	}

	joined := session.ForModel(messages)

	if len(joined) != 1 {
		t.Fatalf("joined into %d messages, want one", len(joined))
	}
	if !joined[0].At.Equal(first) {
		t.Errorf("time = %v, want the earlier %v", joined[0].At, first)
	}
	if !strings.Contains(joined[0].Content, "make it four") {
		t.Errorf("the correction was lost: %q", joined[0].Content)
	}
}

// A failure is shown to the person but never sent back to a model: read as
// conversation it becomes the model explaining an outage it had no part in.
func TestForModelDropsFailuresAndForPersonKeepsThem(t *testing.T) {
	messages := []session.Message{
		{Kind: session.Chat, Role: session.User, Content: "what is the time"},
		{Kind: session.Failure, Role: session.Assistant, Content: "I could not reach the service."},
		{Kind: session.Chat, Role: session.User, Content: "try again"},
	}

	forModel := session.ForModel(messages)
	for _, m := range forModel {
		if m.Kind == session.Failure {
			t.Errorf("a failure reached the model: %q", m.Content)
		}
	}
	// The two questions are now adjacent, so they are joined into one.
	if len(forModel) != 1 {
		t.Errorf("model saw %d messages, want the two questions joined", len(forModel))
	}

	if len(session.ForPerson(messages)) != 3 {
		t.Errorf("the person was not shown all three messages")
	}
}

// An answer that came back blank is not part of the conversation.
func TestForModelDropsEmptyMessages(t *testing.T) {
	messages := []session.Message{
		{Kind: session.Chat, Role: session.User, Content: "hello"},
		{Kind: session.Chat, Role: session.Assistant, Content: "   "},
		{Kind: session.Chat, Role: session.User, Content: "still there?"},
	}

	if got := len(session.ForModel(messages)); got != 1 {
		t.Errorf("kept %d messages, want the two questions joined into one", got)
	}
}
