package runner_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// contents : What was said in a session, as plain strings.
func contents(messages []session.Message) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = string(m.Role) + "/" + string(m.Kind) + ": " + m.Content
	}
	return out
}

// Both halves of an exchange are written down, in the order they were said.
func TestAnExchangeIsRecorded(t *testing.T) {
	h := newHarness(t, &provider.Stub{}, runner.Options{})

	tk := h.submit(t, "check my merge requests")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	said := h.repo.Said(h.session(t))
	if len(said) != 2 {
		t.Fatalf("recorded %v, want the question and the answer", contents(said))
	}

	if said[0].Role != session.User || said[0].Content != "check my merge requests" {
		t.Errorf("first message = %q by %q, want the question", said[0].Content, said[0].Role)
	}
	if said[1].Role != session.Assistant || said[1].Kind != session.Chat {
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
	if seen[0].Role != provider.RoleUser || !strings.Contains(seen[0].Text, "List three") {
		t.Errorf("first turn = %q by %q", seen[0].Text, seen[0].Role)
	}
	if seen[1].Role != provider.RoleAssistant {
		t.Errorf("second turn = %q by %q, want the answer", seen[1].Text, seen[1].Role)
	}
	// The log knows when each was said; the model is not told. Timestamps
	// on the wire made the model echo them back into its answers.
	said := h.repo.Said(h.session(t))
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

	said := h.repo.Said(h.session(t))
	if len(said) != 2 || said[1].Kind != session.Failure {
		t.Fatalf("recorded %v, want the question then the failure", contents(said))
	}
	if len(session.ForPerson(said)) != 2 {
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

// A chat cancelled before it said anything still leaves its question behind.
// That is what lets the correction which replaced it be understood.
func TestACancelledChatStillLeavesItsQuestion(t *testing.T) {
	h := newHarness(t, &provider.Stub{
		Updates: []string{"one", "two", "three"},
		Delay:   30 * time.Millisecond,
	}, runner.Options{})

	tk := h.submit(t, "List three programming languages.")
	h.await(t, tk.ID, chat.StatusRunning)
	h.runner.Cancel(tk.ID)
	h.await(t, tk.ID, chat.StatusCancelled, chat.StatusCompleted, chat.StatusFailed)

	said := h.repo.Said(h.session(t))
	if len(said) != 1 {
		t.Fatalf("recorded %v, want only the question", contents(said))
	}
	if said[0].Content != "List three programming languages." {
		t.Errorf("recorded %q", said[0].Content)
	}
}

// recordingProvider : Answers, and keeps the history it was given so a test
// can check what a model would actually have seen.
type recordingProvider struct {
	failWith   string
	failCode   string
	failDetail string
	seen       []provider.Turn
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) history() []provider.Turn {
	return append([]provider.Turn(nil), p.seen...)
}

func (p *recordingProvider) Run(ctx context.Context, req provider.Request) (<-chan provider.Message, error) {
	p.seen = append([]provider.Turn(nil), req.History...)

	ch := make(chan provider.Message, 1)
	if p.failWith != "" {
		ch <- provider.Failure(p.failWith, p.failCode, p.failDetail)
	} else {
		ch <- provider.Final("an answer")
	}
	close(ch)
	return ch, nil
}
