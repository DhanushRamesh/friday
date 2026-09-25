package runner_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// chatDone : Every status a chat can finish in.
var chatDone = []chat.Status{chat.StatusCompleted, chat.StatusFailed, chat.StatusCancelled}

// condenseMarker : A phrase only a condensation prompt contains, so a test
// provider can tell the two kinds of request apart.
const condenseMarker = "running notes"

// recorder : A provider that answers from a script and keeps every request it
// was given, so a test can assert on what reached the model.
type recorder struct {
	// notes : What to answer a condensation prompt with. Empty makes the
	// condensation fail, since a provider that says nothing has not answered.
	notes string

	mu   sync.Mutex
	seen []provider.Request
}

// Name : Returns the provider's name.
func (p *recorder) Name() string { return "recorder" }

// Run : Answers a condensation prompt with the configured notes, and anything
// else with a fixed sentence.
func (p *recorder) Run(_ context.Context, req provider.Request) (<-chan provider.Message, error) {
	p.mu.Lock()
	p.seen = append(p.seen, req)
	p.mu.Unlock()

	answer := "an answer"
	if strings.Contains(req.Prompt, condenseMarker) {
		answer = p.notes
	}

	ch := make(chan provider.Message, 1)
	ch <- provider.Final(answer)
	close(ch)
	return ch, nil
}

// requests : Every request the provider has been given.
func (p *recorder) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.seen...)
}

// condensations : The requests that asked for a session to be condensed.
func (p *recorder) condensations() []provider.Request {
	var out []provider.Request
	for _, r := range p.requests() {
		if strings.Contains(r.Prompt, condenseMarker) {
			out = append(out, r)
		}
	}
	return out
}

// asking : The request carrying the given prompt, or nil if the provider was
// never given it.
func (p *recorder) asking(prompt string) *provider.Request {
	for _, r := range p.requests() {
		if r.Prompt == prompt {
			found := r
			return &found
		}
	}
	return nil
}

// fill : Says n things in the harness's session, so its history is long
// enough to be worth condensing.
func (h *harness) fill(t *testing.T, n int) {
	t.Helper()
	id := h.session(t)
	for i := 0; i < n; i++ {
		role, text := session.User, "the person said something"
		if i%2 == 1 {
			role, text = session.Assistant, "the assistant answered"
		}
		m := session.Message{
			ID:        session.NewMessageID(),
			SessionID: id,
			Kind:      session.Chat,
			Role:      role,
			Content:   text,
			At:        time.Now().UTC(),
		}
		if _, err := h.repo.Append(context.Background(), m); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

// summary : The session's stored summary.
func (h *harness) summary(t *testing.T) session.Summary {
	t.Helper()
	s, err := h.repo.Summary(context.Background(), h.session(t))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	return s
}

// awaitSummary : Waits for the session to be condensed.
//
// Condensing happens after the chat reaches its final status, so waiting for
// the chat is not enough to see it.
func (h *harness) awaitSummary(t *testing.T) session.Summary {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := h.summary(t); s.Text != "" {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the session was never condensed")
	return session.Summary{}
}

// settle : Waits for everything the runner has in flight, including work that
// outlives the chat that started it.
func (h *harness) settle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.runner.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// A session short enough to send in full is left alone: condensing it would
// spend a model call to lose detail for nothing.
func TestAShortSessionIsNotCondensed(t *testing.T) {
	p := &recorder{notes: "they talked"}
	h := newHarness(t, p, runner.Options{})

	tk := h.submit(t, "hello")
	h.await(t, tk.ID, chatDone...)
	h.settle(t)

	if got := p.condensations(); len(got) != 0 {
		t.Errorf("condensed a short session %d times", len(got))
	}
	if s := h.summary(t); s.Text != "" {
		t.Errorf("summary = %q, want none", s.Text)
	}
}

// Once the conversation nears a ceiling its earliest part is folded into the
// session's notes, and the messages themselves stay where they are.
func TestALongSessionIsCondensedAfterTheTurn(t *testing.T) {
	p := &recorder{notes: "The person asked about the roof and was told it is fine."}
	h := newHarness(t, p, runner.Options{HistoryLimits: session.Limits{Count: 100}})

	h.fill(t, 94)
	tk := h.submit(t, "and what about the gutters")
	h.await(t, tk.ID, chatDone...)

	s := h.awaitSummary(t)
	if s.Text != p.notes {
		t.Errorf("summary = %q, want what the provider answered", s.Text)
	}
	if s.ThroughSeq == 0 {
		t.Error("the summary covers nothing")
	}

	// The transcript records what was said, so condensing must not remove
	// any of it.
	said, err := h.repo.All(context.Background(), h.session(t))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(said) != 96 {
		t.Errorf("session holds %d messages, want all 96 still there", len(said))
	}
}

// The boundary falls where a turn begins, so a question is never condensed
// apart from the answer to it.
func TestCondensingStopsAtATurnBoundary(t *testing.T) {
	p := &recorder{notes: "notes"}
	h := newHarness(t, p, runner.Options{HistoryLimits: session.Limits{Count: 100}})

	h.fill(t, 94)
	tk := h.submit(t, "and what about the gutters")
	h.await(t, tk.ID, chatDone...)
	s := h.awaitSummary(t)

	said, err := h.repo.All(context.Background(), h.session(t))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for _, m := range said {
		if m.Seq == s.ThroughSeq+1 && m.Role != session.User {
			t.Errorf("what is left verbatim opens with %s at seq %d, want a question", m.Role, m.Seq)
		}
	}
}

// The notes reach the model on the next turn, in place of what they cover.
func TestTheSummaryIsSentOnTheNextTurn(t *testing.T) {
	notes := "The person asked about the roof and was told it is fine."
	p := &recorder{notes: notes}
	h := newHarness(t, p, runner.Options{HistoryLimits: session.Limits{Count: 100}})

	h.fill(t, 94)
	first := h.submit(t, "and what about the gutters")
	h.await(t, first.ID, chatDone...)
	h.awaitSummary(t)

	second := h.submit(t, "and the drains")
	h.await(t, second.ID, chatDone...)

	asked := p.asking("and the drains")
	if asked == nil {
		t.Fatal("the second question never reached the provider")
	}
	if asked.Summary != notes {
		t.Errorf("Summary = %q, want the stored notes", asked.Summary)
	}
	if len(asked.History) >= 95 {
		t.Errorf("sent %d messages, want the condensed ones replaced by the notes", len(asked.History))
	}
}

// A provider that will not condense costs the session nothing: the notes stay
// as they were, and the conversation carries on.
func TestACondensationThatFailsLeavesTheSessionAlone(t *testing.T) {
	p := &recorder{notes: ""}
	h := newHarness(t, p, runner.Options{HistoryLimits: session.Limits{Count: 100}})

	h.fill(t, 94)
	tk := h.submit(t, "and what about the gutters")
	h.await(t, tk.ID, chatDone...)
	h.settle(t)

	if len(p.condensations()) == 0 {
		t.Fatal("never tried to condense")
	}
	if s := h.summary(t); s.Text != "" || s.ThroughSeq != 0 {
		t.Errorf("summary = %+v, want it left unset", s)
	}
}

// submitModel : Creates a chat that names a model, stores it, and starts it.
//
// The model is set before the runner is given the chat, because afterwards is
// a race with the run itself.
func (h *harness) submitModel(t *testing.T, prompt string, model chat.Model) *chat.Chat {
	t.Helper()
	tk, err := chat.New(h.session(t), chat.ChannelDirect, prompt)
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	tk.Model = model
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return tk
}

// The model a chat was accepted for is the one asked, not whatever the
// provider is configured with.
func TestTheChatsModelReachesTheProvider(t *testing.T) {
	p := &recorder{notes: "notes"}
	h := newHarness(t, p, runner.Options{})

	want := chat.NewModel("anthropic", "claude-haiku-4-5")
	tk := h.submitModel(t, "what is the time", want)
	h.await(t, tk.ID, chatDone...)

	asked := p.asking("what is the time")
	if asked == nil {
		t.Fatal("the question never reached the provider")
	}
	if asked.Model != "claude-haiku-4-5" {
		t.Errorf("Model = %q, want claude-haiku-4-5", asked.Model)
	}
	if asked.Vendor != want.Vendor {
		t.Errorf("Vendor = %q, want %q", asked.Vendor, want.Vendor)
	}
}

// A chat that chose no model sends none, so the provider uses what it is
// configured with rather than being handed an empty name.
func TestAChatWithNoModelSendsNone(t *testing.T) {
	p := &recorder{notes: "notes"}
	h := newHarness(t, p, runner.Options{})

	tk := h.submit(t, "what is the time")
	h.await(t, tk.ID, chatDone...)

	asked := p.asking("what is the time")
	if asked == nil {
		t.Fatal("the question never reached the provider")
	}
	if asked.Model != "" || asked.Vendor != "" {
		t.Errorf("sent vendor %q model %q, want neither", asked.Vendor, asked.Model)
	}
}

// A smaller model is sent less history, since the ceiling that binds is its
// context window rather than the byte budget.
func TestASmallerModelIsSentLessHistory(t *testing.T) {
	p := &recorder{notes: "notes"}
	h := newHarness(t, p, runner.Options{})

	h.fill(t, 60)

	// Qwen 3 8B holds 32,768 tokens against Claude's 200,000, so the same
	// session reaches the provider differently depending on which answers.
	small := h.submitModel(t, "on the small one", chat.NewModel("ollama", "qwen3:8b"))
	h.await(t, small.ID, chatDone...)

	big := h.submitModel(t, "on the big one", chat.NewModel("anthropic", "claude-sonnet-4-6"))
	h.await(t, big.ID, chatDone...)

	onSmall, onBig := p.asking("on the small one"), p.asking("on the big one")
	if onSmall == nil || onBig == nil {
		t.Fatal("both questions should have reached the provider")
	}
	if len(onSmall.History) > len(onBig.History) {
		t.Errorf("the smaller model was sent %d messages and the larger %d",
			len(onSmall.History), len(onBig.History))
	}
}
