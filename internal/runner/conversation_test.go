package runner_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
)

// contents : What was said in a conversation, as plain strings.
func contents(messages []conversation.Message) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = string(m.Role) + "/" + string(m.Kind) + ": " + m.Content
	}
	return out
}

// Both halves of an exchange are written down, in the order they were said.
func TestAnExchangeIsRecorded(t *testing.T) {
	h := newHarness(t, &environment.Stub{}, runner.Options{})

	tk := h.submit(t, "check my merge requests")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	said := h.repo.Said(h.conversation(t))
	if len(said) != 2 {
		t.Fatalf("recorded %v, want the question and the answer", contents(said))
	}

	if said[0].Role != conversation.User || said[0].Content != "check my merge requests" {
		t.Errorf("first message = %q by %q, want the question", said[0].Content, said[0].Role)
	}
	if said[1].Role != conversation.Assistant || said[1].Kind != conversation.Chat {
		t.Errorf("second message = %q by %q", said[1].Content, said[1].Role)
	}
	if said[0].Seq != 1 || said[1].Seq != 2 {
		t.Errorf("positions = %d, %d, want 1, 2", said[0].Seq, said[1].Seq)
	}
}

// The question being answered must not also reach the model as the last thing
// said. Sent twice, a model reads a question it has not been asked yet as
// something already discussed.
func TestTheQuestionIsNotAlsoInItsOwnHistory(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	first := h.submit(t, "List three programming languages.")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)

	second := h.submit(t, "No, make it four.")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	for _, turn := range recorder.history() {
		if strings.Contains(turn.Text, "No, make it four.") {
			t.Errorf("the question is repeated in its own history as %q: %s", turn.Role, turn.Text)
		}
	}
}

// The earlier exchange does reach the model, both halves of it, which is the
// whole reason the log exists.
func TestTheEarlierExchangeReachesTheModel(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	first := h.submit(t, "List three programming languages.")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)

	second := h.submit(t, "No, make it four.")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	seen := recorder.history()
	if len(seen) != 2 {
		t.Fatalf("model saw %d turns, want the question and the answer", len(seen))
	}
	if seen[0].Role != environment.RoleUser || !strings.Contains(seen[0].Text, "List three") {
		t.Errorf("first turn = %q by %q", seen[0].Text, seen[0].Role)
	}
	if seen[1].Role != environment.RoleAssistant {
		t.Errorf("second turn = %q by %q, want the answer", seen[1].Text, seen[1].Role)
	}
	// The log knows when each was said; the model is not told. Timestamps
	// on the wire made the model echo them back into its answers.
	said := h.repo.Said(h.conversation(t))
	for _, m := range said {
		if m.At.IsZero() {
			t.Errorf("the log did not record when %q was said", m.Role)
		}
	}
}

// A failure is written down, because the person saw it and a correction
// refers to it — but it is never handed back to a model.
func TestAFailureIsRecordedAndSentOnWithItsDetail(t *testing.T) {
	const reason = "The service could not complete the request."
	const detail = "INVALID_OAUTHTOKEN (HTTP 401)"
	recorder := &recordingProvider{
		failWith:   reason,
		failCode:   "unauthorised",
		failDetail: detail,
	}
	h := newHarness(t, recorder, runner.Options{})

	first := h.submit(t, "what is the time")
	h.await(t, first.ID, chat.StatusFailed, chat.StatusCompleted)

	said := h.repo.Said(h.conversation(t))
	if len(said) != 2 || said[1].Kind != conversation.Failure {
		t.Fatalf("recorded %v, want the question then the failure", contents(said))
	}
	if len(conversation.ForPerson(said)) != 2 {
		t.Error("the person is not shown the failure they watched happen")
	}

	recorder.failWith = ""
	second := h.submit(t, "try again")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	// The failure is given to the model, and with the exact error, so that
	// "what went wrong?" is answerable. Withholding it is what used to make
	// the model invent an explanation for an outage it had no part in.
	var sawDetail bool
	for _, turn := range recorder.history() {
		if strings.Contains(turn.Text, detail) {
			sawDetail = true
		}
	}
	if !sawDetail {
		t.Errorf("the exact error never reached the model: %v", recorder.history())
	}
}

// A cancelled turn leaves its question and a note that it was stopped. The
// question alone would be read by the next turn as something never answered,
// and joined onto whatever is asked next.
//
// This test used to assert the question alone, and passed for the wrong
// reason: the note was being written and silently refused by the store,
// because Valid did not know the kind. The log said so and nothing else did.
func TestACancelledChatLeavesItsQuestionAndAMark(t *testing.T) {
	h := newHarness(t, &environment.Stub{
		Updates: []string{"one", "two", "three"},
		Delay:   30 * time.Millisecond,
	}, runner.Options{})

	tk := h.submit(t, "List three programming languages.")
	h.await(t, tk.ID, chat.StatusRunning)
	h.runner.Cancel(tk.ID)
	h.await(t, tk.ID, chat.StatusCancelled, chat.StatusCompleted, chat.StatusFailed)

	said := h.repo.Said(h.conversation(t))
	if len(said) != 2 {
		t.Fatalf("recorded %v, want the question and the mark", contents(said))
	}
	if said[0].Content != "List three programming languages." {
		t.Errorf("recorded %q", said[0].Content)
	}
	if said[1].Kind != conversation.Interruption {
		t.Errorf("second message is %q, want an interruption", said[1].Kind)
	}
	if said[1].Role != conversation.Assistant {
		t.Errorf("the mark is %q, want the assistant so the roles alternate", said[1].Role)
	}
}

// recordingProvider : Answers, and keeps the history it was given so a test
// can check what a model would actually have seen.
type recordingProvider struct {
	failWith   string
	failCode   string
	failDetail string
	seen       []environment.Turn
	system     string
}

// systemPrompt : What the assistant was last told about itself.
func (p *recordingProvider) systemPrompt() string { return p.system }

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) history() []environment.Turn {
	return append([]environment.Turn(nil), p.seen...)
}

func (p *recordingProvider) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	// Only what the person asked. Naming and condensing run after the answer
	// and carry no history, so recording them would wipe what is asserted on.
	if req.Purpose == environment.PurposeChat {
		p.seen = append([]environment.Turn(nil), req.History...)
		p.system = req.SystemPrompt
	}

	ch := make(chan environment.Message, 1)
	if p.failWith != "" {
		ch <- environment.Failure(p.failWith, p.failCode, p.failDetail)
	} else {
		ch <- environment.Final("an answer")
	}
	close(ch)
	return ch, nil
}

// Spoken, a failure says the exact error too. There is no "more info" on a
// speaker, so the sentence alone leaves the person with a failure and no way
// to reach what caused it.
func TestAVoiceFailureSaysTheExactError(t *testing.T) {
	const detail = "platformai: Output blocked by content filtering policy (HTTP 400)"

	recorder := &recordingProvider{
		failWith:   "The service would not let me answer that.",
		failCode:   "filtered",
		failDetail: detail,
	}
	h := newHarness(t, recorder, runner.Options{})

	tk := h.submitOn(t, chat.ChannelVoice, "do you know the lyrics of Fireflies")
	done := h.await(t, tk.ID, chat.StatusFailed, chat.StatusCompleted)

	if !strings.Contains(done.Error, "content filtering") {
		t.Errorf("spoken failure = %q, want the exact error in it", done.Error)
	}
	if done.ErrorDetail != detail {
		t.Errorf("detail = %q, want it kept exactly", done.ErrorDetail)
	}
}

// Typed, the sentence alone. The exact error is under "more info", and a wall
// of service jargon in the transcript buries the part anybody reads.
func TestATypedFailureKeepsTheJargonOutOfTheWay(t *testing.T) {
	const detail = "platformai: Output blocked by content filtering policy (HTTP 400)"

	recorder := &recordingProvider{
		failWith:   "The service would not let me answer that.",
		failCode:   "filtered",
		failDetail: detail,
	}
	h := newHarness(t, recorder, runner.Options{})

	tk := h.submitOn(t, chat.ChannelDirect, "do you know the lyrics of Fireflies")
	done := h.await(t, tk.ID, chat.StatusFailed, chat.StatusCompleted)

	if strings.Contains(done.Error, "content filtering") {
		t.Errorf("shown failure = %q, want the jargon left to more-info", done.Error)
	}
	if done.ErrorDetail != detail {
		t.Errorf("detail = %q, want it kept for when it is asked for", done.ErrorDetail)
	}
}

// A spoken turn is told its words may be misheard. A typed one is not: typing
// means what it says, and reinterpreting a word somebody chose deliberately
// is worse than taking it literally.
func TestOnlyASpokenTurnIsWarnedAboutMishearing(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	spoken := h.submitOn(t, chat.ChannelVoice, "can you unlock any of the two conversations")
	h.await(t, spoken.ID, chatDone...)
	if !strings.Contains(recorder.systemPrompt(), "sounds like") {
		t.Error("a spoken turn was not warned that its words may be misheard")
	}

	typed := h.submitOn(t, chat.ChannelDirect, "can you unlock any of the two conversations")
	h.await(t, typed.ID, chatDone...)
	if strings.Contains(recorder.systemPrompt(), "sounds like") {
		t.Error("a typed turn was warned, and typing means what it says")
	}
}
