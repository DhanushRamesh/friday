package runner_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmemory "github.com/DhanushRamesh/personal-assistant/internal/chat/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
)

// remembering : A runner whose conversation belongs to somebody, with the
// given memories already stored and embedded.
type remembering struct {
	runner *runner.Runner
	repo   *chatmemory.Repository
	store  *inmemory.Store
	past   *inmemory.Transcript
	recall *memory.Recall
	convID string
	userID string
}

// withMemories : Builds one, storing each fact as subject and body.
func withMemories(t *testing.T, p *recordingProvider, facts map[memory.Tier][][2]string) *remembering {
	t.Helper()
	ctx := context.Background()

	repo := chatmemory.New()
	store := inmemory.New()
	past := inmemory.NewTranscript()
	recall := &memory.Recall{
		Store: store, Transcript: past, Embedder: embed.Fake{}, Logger: discard(),
	}

	userID := chat.NewUserID()
	c := chat.NewConversation(userID, "Roof Quotes")
	if err := repo.CreateConversation(ctx, c); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	for tier, list := range facts {
		for _, f := range list {
			m, err := memory.New(userID, tier, f[0], f[1])
			if err != nil {
				t.Fatalf("memory.New: %v", err)
			}
			if err := store.Create(ctx, m); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}
	}
	if _, err := recall.Embed(ctx, 100); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	r, err := runner.New(runner.Options{
		Repository: repo, Messages: repo, Environment: p,
		Publisher: events.NewBus(discard()), Logger: discard(), Memory: recall,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.Shutdown(ctx)
	})

	return &remembering{
		runner: r, repo: repo, store: store, past: past, recall: recall,
		convID: c.ID, userID: userID,
	}
}

// settle : Waits for a chat to reach a terminal state.
func (h *remembering) settle(t *testing.T, id string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := h.repo.Get(context.Background(), id)
		if err == nil && got.Status.IsTerminal() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the chat never finished")
}

// ask : Runs one chat and waits for it to finish.
func (h *remembering) ask(t *testing.T, prompt string) {
	t.Helper()

	tk, err := chat.New(h.convID, chat.ChannelDirect, prompt)
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := h.repo.Get(context.Background(), tk.ID)
		if err == nil && got.Status.IsTerminal() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the chat never finished")
}

// What the assistant always knows reaches every prompt.
func TestTheAlwaysTierReachesThePrompt(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, map[memory.Tier][][2]string{
		memory.TierAlways: {{"Birthday", "22 October 1999"}},
	})

	h.ask(t, "anything at all")

	if !strings.Contains(p.systemPrompt(), "22 October 1999") {
		t.Errorf("system prompt does not carry what is always known:\n%s", p.systemPrompt())
	}
}

// A memory resembling the question is offered.
func TestAResemblingMemoryIsOffered(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, map[memory.Tier][][2]string{
		memory.TierRecall: {
			{"Roof quote", "the roofer quoted forty thousand rupees"},
			{"Cricket", "follows Kapil Dev and MS Dhoni"},
		},
	})

	h.ask(t, "what did the roofer quote")

	if !strings.Contains(p.systemPrompt(), "forty thousand rupees") {
		t.Errorf("the roof memory was not offered:\n%s", p.systemPrompt())
	}
}

// Offered memories are marked used, so one that is found every time and
// never helps can be told from one that is never found.
func TestOfferedMemoriesAreRecordedAsUsed(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, map[memory.Tier][][2]string{
		memory.TierRecall: {{"Roof quote", "the roofer quoted forty thousand rupees"}},
	})

	h.ask(t, "what did the roofer quote")

	all, err := h.store.All(context.Background(), h.userID, memory.TierRecall)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].Uses != 1 || all[0].LastUsedAt == nil {
		t.Errorf("uses = %d, last used = %v, want it recorded", all[0].Uses, all[0].LastUsedAt)
	}
}

// The notes carry the instruction that makes them safe to ignore. Nearest is
// not relevant, and without being told so the model treats a near miss as an
// answer.
func TestTheOfferedNotesSayTheyMayBeIrrelevant(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, map[memory.Tier][][2]string{
		memory.TierRecall: {{"Roof quote", "forty thousand rupees"}},
	})

	h.ask(t, "what did the roofer quote")

	for _, want := range []string{"none of them will", "ignoring all of them"} {
		if !strings.Contains(p.systemPrompt(), want) {
			t.Errorf("the notes are missing %q:\n%s", want, p.systemPrompt())
		}
	}
}

// Another person's memories never reach the prompt.
func TestAnotherPersonsMemoriesAreNotOffered(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, nil)

	mine, err := memory.New(chat.NewUserID(), memory.TierRecall, "Roof quote", "forty thousand rupees")
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	if err := h.store.Create(context.Background(), mine); err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.ask(t, "what did the roofer quote")

	if strings.Contains(p.systemPrompt(), "forty thousand") {
		t.Errorf("somebody else's memory reached the prompt:\n%s", p.systemPrompt())
	}
}

// With nothing stored the prompt is what it was before memory existed.
func TestNoMemoriesAddNothing(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, nil)

	h.ask(t, "anything at all")

	for _, unwanted := range []string{"What you know about the person", "Notes found by searching"} {
		if strings.Contains(p.systemPrompt(), unwanted) {
			t.Errorf("an empty memory added %q to the prompt", unwanted)
		}
	}
}

// A runner with no memory at all still answers, which is how it behaved
// before there was anywhere to keep anything.
func TestARunnerWithoutMemoryStillAnswers(t *testing.T) {
	p := &recordingProvider{}
	h := newHarness(t, p, runner.Options{})

	tk := h.submit(t, "what did the roofer quote")
	h.await(t, tk.ID, chat.StatusCompleted)

	if p.systemPrompt() == "" {
		t.Error("no system prompt was sent at all")
	}
}

// Something said in another conversation is found again and quoted.
func TestAPastExchangeIsQuoted(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, nil)

	h.past.Add(memory.Exchange{
		MessageID:      "msg_earlier",
		UserID:         h.userID,
		ConversationID: "conv_01M3D477HXQ4YNQX7BNXJZZCV9",
		Text:           memory.ExchangeText("what did the roofer quote", "He quoted forty thousand rupees."),
		At:             time.Now().UTC().Add(-72 * time.Hour),
	})
	if _, err := h.recall.IndexTranscript(context.Background(), 10); err != nil {
		t.Fatalf("IndexTranscript: %v", err)
	}

	h.ask(t, "what did the roofer quote")

	if !strings.Contains(p.systemPrompt(), "forty thousand rupees") {
		t.Errorf("the earlier exchange was not quoted:\n%s", p.systemPrompt())
	}
	if !strings.Contains(p.systemPrompt(), "never state one as a fact of your own") {
		t.Errorf("the transcript was offered without saying it is a transcript:\n%s", p.systemPrompt())
	}
}

// What was said in this conversation is not quoted back: it is already in
// front of the model.
func TestTheCurrentConversationIsNotQuotedBack(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, nil)

	h.past.Add(memory.Exchange{
		MessageID:      "msg_here",
		UserID:         h.userID,
		ConversationID: h.convID,
		Text:           memory.ExchangeText("what did the roofer quote", "He quoted forty thousand rupees."),
		At:             time.Now().UTC(),
	})
	if _, err := h.recall.IndexTranscript(context.Background(), 10); err != nil {
		t.Fatalf("IndexTranscript: %v", err)
	}

	h.ask(t, "what did the roofer quote")

	if strings.Contains(p.systemPrompt(), "forty thousand rupees") {
		t.Errorf("this conversation was quoted back to itself:\n%s", p.systemPrompt())
	}
}

// What the model was shown is recorded on the chat, so an answer can be
// explained afterwards rather than guessed at.
func TestWhatWasRecalledIsRecordedOnTheChat(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, map[memory.Tier][][2]string{
		memory.TierAlways: {{"Home", "Lives in Chennai"}},
		memory.TierRecall: {{"Roof quote", "the roofer quoted forty thousand rupees"}},
	})

	h.past.Add(memory.Exchange{
		MessageID:      "msg_earlier",
		UserID:         h.userID,
		ConversationID: "conv_01M3D477HXQ4YNQX7BNXJZZCV9",
		Text:           memory.ExchangeText("is the terrace worth doing", "Yes, before the rains."),
		At:             time.Now().UTC().Add(-48 * time.Hour),
	})
	if _, err := h.recall.IndexTranscript(context.Background(), 10); err != nil {
		t.Fatalf("IndexTranscript: %v", err)
	}

	tk, err := chat.New(h.convID, chat.ChannelDirect, "what did the roofer quote")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	h.settle(t, tk.ID)

	got, err := h.repo.Get(context.Background(), tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Recalled == nil {
		t.Fatal("nothing was recorded about how the answer was made")
	}
	if len(got.Recalled.Always) != 1 || !strings.Contains(got.Recalled.Always[0].Text, "Chennai") {
		t.Errorf("always = %v, want the Chennai memory", got.Recalled.Always)
	}
	if len(got.Recalled.Notes) == 0 || !strings.Contains(got.Recalled.Notes[0].Text, "forty thousand") {
		t.Errorf("notes = %v, want the roof memory", got.Recalled.Notes)
	}
	if got.Recalled.Notes[0].Score <= 0 {
		t.Error("a searched note was recorded with no score")
	}
	if len(got.Recalled.Exchanges) == 0 {
		t.Error("the past exchange was not recorded")
	}
}

// Every message a turn writes says which turn wrote it, or the tool calls
// of one answer cannot be told from those of the next.
func TestEveryMessageSaysWhichChatWroteIt(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, nil)

	tk, err := chat.New(h.convID, chat.ChannelDirect, "anything at all")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	h.settle(t, tk.ID)

	said, err := h.repo.All(context.Background(), h.convID)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(said) == 0 {
		t.Fatal("nothing was recorded at all")
	}
	for _, m := range said {
		if m.ChatID != tk.ID {
			t.Errorf("message %s says chat %q, want %q", m.ID, m.ChatID, tk.ID)
		}
	}
}

// The assistant is told the time on every turn. It cannot know it, and
// nothing about a reminder or a date works until it does.
func TestThePromptCarriesTheTime(t *testing.T) {
	p := &recordingProvider{}
	h := withMemories(t, p, nil)

	h.ask(t, "what day is it")

	if !strings.Contains(p.systemPrompt(), "The time where the person is") {
		t.Errorf("the prompt does not say what time it is:\n%s", p.systemPrompt())
	}
}
