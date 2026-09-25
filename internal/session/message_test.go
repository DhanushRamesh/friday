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
func TestForModelIsGivenFailuresWithTheirDetail(t *testing.T) {
	messages := []session.Message{
		{Kind: session.Chat, Role: session.User, Content: "what is the time"},
		{
			Kind:    session.Failure,
			Role:    session.Assistant,
			Content: "The service could not complete the request.",
			Detail:  "INVALID_OAUTHTOKEN (HTTP 401)",
		},
		{Kind: session.Chat, Role: session.User, Content: "what exactly went wrong"},
	}

	forModel := session.ForModel(messages)

	// Three, not two joined into one: the failure separates the questions, so
	// the model can see that the first was answered with an outage and that
	// the second is asking about it.
	if len(forModel) != 3 {
		t.Fatalf("model saw %d messages, want all three", len(forModel))
	}
	if !strings.Contains(forModel[1].Content, "INVALID_OAUTHTOKEN") {
		t.Errorf("the exact error was withheld from the model: %q", forModel[1].Content)
	}

	// The person is shown the sentence and not the detail; the detail is
	// carried beside it for a client to reveal when asked.
	shown := session.ForPerson(messages)
	if len(shown) != 3 {
		t.Fatalf("the person saw %d messages, want all three", len(shown))
	}
	if strings.Contains(shown[1].Content, "INVALID_OAUTHTOKEN") {
		t.Errorf("the raw error leaked into what is read aloud: %q", shown[1].Content)
	}
	if shown[1].Detail != "INVALID_OAUTHTOKEN (HTTP 401)" {
		t.Errorf("detail = %q, want it kept beside the sentence", shown[1].Detail)
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

func TestForModelKeepsAnInterruption(t *testing.T) {
	messages := []session.Message{
		{Kind: session.Chat, Role: session.User, Content: "add rice to the list"},
		{Kind: session.Interruption, Role: session.Assistant, Content: "[stopped]"},
		{Kind: session.Chat, Role: session.User, Content: "what did you get done"},
	}

	forModel := session.ForModel(messages)

	// Three messages, not two joined into one: the interruption sits between
	// the questions and keeps them apart, which is the whole point. A model
	// that saw them joined would read one question and never learn that the
	// first was cut off.
	if len(forModel) != 3 {
		t.Fatalf("model saw %d messages, want all three", len(forModel))
	}
	if forModel[1].Kind != session.Interruption {
		t.Errorf("the interruption did not reach the model: %+v", forModel[1])
	}
}

func TestInterruptedIsTheAssistantsTurn(t *testing.T) {
	at := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)

	m := session.Interrupted("ses_1", at)

	// Written as the assistant so the roles still alternate. As the user it
	// would join onto the question before it and read as part of what was
	// asked.
	if m.Role != session.Assistant {
		t.Errorf("role = %q, want the assistant", m.Role)
	}
	if m.Kind != session.Interruption {
		t.Errorf("kind = %q, want an interruption", m.Kind)
	}
	if strings.TrimSpace(m.Content) == "" {
		t.Error("an interruption with no content would be dropped by ForModel")
	}
}

// Every kind this package can build has to be one the store accepts. They
// were listed in two places, and an interruption was rejected for a morning
// because only one of them learned about it.
func TestEveryConstructedMessageIsValid(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	for name, m := range map[string]session.Message{
		"said":        session.Said("ses_1", "what is the time", at),
		"answered":    session.Answered("ses_1", "half past two", at),
		"failed":      session.Failed("ses_1", "it did not work", "HTTP 500", at),
		"interrupted": session.Interrupted("ses_1", at),
	} {
		if err := m.Valid(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A position is not an identity. seq orders the conversation and would move
// if anything were ever removed from the middle of one; the identifier names
// the same message afterwards.
func TestEveryMessageGetsItsOwnIdentifier(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	seen := map[string]bool{}

	for _, m := range []session.Message{
		session.Said("ses_1", "one", at),
		session.Said("ses_1", "one", at),
		session.Answered("ses_1", "two", at),
		session.Failed("ses_1", "three", "detail", at),
		session.Interrupted("ses_1", at),
	} {
		if !strings.HasPrefix(m.ID, session.MessageIDPrefix) {
			t.Errorf("id = %q, want the %s prefix", m.ID, session.MessageIDPrefix)
		}
		if seen[m.ID] {
			t.Errorf("identifier handed out twice: %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestAMessageWithoutAnIdentifierIsRefused(t *testing.T) {
	m := session.Said("ses_1", "hello", time.Now())
	m.ID = ""

	if err := m.Valid(); err == nil {
		t.Error("a message with no identifier was accepted")
	}
}
