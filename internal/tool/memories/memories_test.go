package memories_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
)

const user = "usr_01M3D477HXQ4YNQX7BNXJZZCV0"

// harness : A registry over an empty store.
func harness(t *testing.T) (*tool.Registry, *inmemory.Store, *memory.Recall) {
	t.Helper()

	store := inmemory.New()
	recall := &memory.Recall{Store: store, Embedder: embed.Fake{}}

	r, err := tool.NewRegistry(memories.All(recall)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r, store, recall
}

// call : Calls a tool as the given channel and returns its result.
func call(t *testing.T, r *tool.Registry, channel chat.Channel, name, args string) tool.Result {
	t.Helper()

	return r.Call(context.Background(), name, tool.Invocation{
		Caller: tool.Caller{
			UserID: user, Channel: channel, ConversationID: "conv_01M3D477HXQ4YNQX7BNXJZZCV0",
		},
		Args: json.RawMessage(args),
	})
}

// Every memory tool has to survive registration, which is where a missing
// description or an untyped argument is caught.
func TestEveryMemoryToolRegisters(t *testing.T) {
	if _, err := tool.NewRegistry(memories.All(nil)...); err != nil {
		t.Fatalf("a memory tool is not usable: %v", err)
	}
}

// Forgetting cannot be undone, and on voice a misheard sentence is the whole
// authorisation.
func TestVoiceCannotForget(t *testing.T) {
	r, _, _ := harness(t)

	for _, x := range r.For(chat.ChannelVoice) {
		if x.Name == "memory_forget" {
			t.Error("voice is offered memory_forget")
		}
	}

	var typedHasIt bool
	for _, x := range r.For(chat.ChannelDirect) {
		if x.Name == "memory_forget" {
			typedHasIt = true
		}
	}
	if !typedHasIt {
		t.Error("typing is not offered memory_forget either")
	}
}

// Remembering stores it and says the identifier, which is what every other
// tool needs.
func TestRememberingStoresIt(t *testing.T) {
	r, store, _ := harness(t)

	got := call(t, r, chat.ChannelVoice, "memory_remember",
		`{"subject":"Roof quote","body":"The roofer quoted forty thousand rupees."}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "mem_") {
		t.Errorf("content does not carry the identifier: %s", got.Content)
	}

	all, err := store.All(context.Background(), user, memory.TierRecall)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].Subject != "Roof quote" {
		t.Errorf("stored %v, want one roof memory", all)
	}
}

// A memory marked always goes in the tier that reaches every prompt.
func TestAlwaysGoesInTheAlwaysTier(t *testing.T) {
	r, store, _ := harness(t)

	call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"How to answer","body":"Wants answers kept short.","always":true}`)

	all, err := store.All(context.Background(), user, memory.TierAlways)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("got %d always memories, want 1", len(all))
	}
}

// A new memory is embedded at once, so it can be found by meaning before the
// next restart.
func TestANewMemoryIsEmbeddedAtOnce(t *testing.T) {
	r, store, _ := harness(t)

	call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"Roof quote","body":"forty thousand rupees"}`)

	all, _ := store.All(context.Background(), user, memory.TierRecall)
	if len(all) != 1 || !all[0].Embedded("fake") {
		t.Error("a new memory was left without a vector")
	}
}

// Searching finds it and returns the identifier.
func TestSearchingFindsWhatWasStored(t *testing.T) {
	r, _, _ := harness(t)

	call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"Roof quote","body":"the roofer quoted forty thousand rupees"}`)

	got := call(t, r, chat.ChannelDirect, "memory_search", `{"about":"the roofer quoted"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "forty thousand") || !strings.Contains(got.Content, "mem_") {
		t.Errorf("search result is not usable: %s", got.Content)
	}
}

// An empty store says so rather than returning nothing at all.
func TestSearchingAnEmptyStoreSaysSo(t *testing.T) {
	r, _, _ := harness(t)

	got := call(t, r, chat.ChannelDirect, "memory_search", `{"about":"anything"}`)
	if got.Outcome != conversation.OutcomeOK || !strings.Contains(got.Content, "Nothing is remembered") {
		t.Errorf("got %s: %s", got.Outcome, got.Content)
	}
}

// Changing a memory replaces its text and keeps its identifier.
func TestUpdatingReplacesTheText(t *testing.T) {
	r, store, _ := harness(t)

	call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"Roof quote","body":"forty thousand rupees"}`)
	all, _ := store.All(context.Background(), user, memory.TierRecall)
	id := all[0].ID

	got := call(t, r, chat.ChannelDirect, "memory_update",
		`{"id":"`+id+`","subject":"Roof quote","body":"fifty thousand rupees"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	after, err := store.Get(context.Background(), user, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Body != "fifty thousand rupees" {
		t.Errorf("body = %q, want the new text", after.Body)
	}
}

// An identifier that does not exist is refused with advice, not a guess.
func TestUpdatingSomethingThatIsNotThere(t *testing.T) {
	r, _, _ := harness(t)

	got := call(t, r, chat.ChannelDirect, "memory_update",
		`{"id":"mem_01M3D477HXQ4YNQX7BNXJZZCV0","subject":"x","body":"y"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("outcome = %s, want a failure", got.Outcome)
	}
	if !strings.Contains(got.Content, "Search for it") {
		t.Errorf("content does not say what to do instead: %s", got.Content)
	}
}

// Forgetting removes it.
func TestForgettingRemovesIt(t *testing.T) {
	r, store, _ := harness(t)

	call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"Roof quote","body":"forty thousand rupees"}`)
	all, _ := store.All(context.Background(), user, memory.TierRecall)
	id := all[0].ID

	got := call(t, r, chat.ChannelDirect, "memory_forget",
		`{"id":"`+id+`","confirm_subject":"Roof quote"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	if left, _ := store.All(context.Background(), user, memory.TierRecall); len(left) != 0 {
		t.Errorf("%d memories left, want none", len(left))
	}
}

// The subject has to match the memory being forgotten, so a wrong
// identifier forgets nothing.
func TestForgettingChecksTheSubject(t *testing.T) {
	r, store, _ := harness(t)

	call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"Roof quote","body":"forty thousand rupees"}`)
	all, _ := store.All(context.Background(), user, memory.TierRecall)
	id := all[0].ID

	got := call(t, r, chat.ChannelDirect, "memory_forget",
		`{"id":"`+id+`","confirm_subject":"Cricket scores"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("outcome = %s, want a refusal", got.Outcome)
	}
	if left, _ := store.All(context.Background(), user, memory.TierRecall); len(left) != 1 {
		t.Error("it was forgotten despite the wrong subject")
	}
}

// A request from nobody stores nothing, and says so rather than reporting a
// success that kept nothing.
func TestARequestFromNobodyStoresNothing(t *testing.T) {
	r, store, _ := harness(t)

	got := r.Call(context.Background(), "memory_remember", tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelDirect},
		Args:   json.RawMessage(`{"subject":"Roof quote","body":"forty thousand"}`),
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want a failure", got.Outcome)
	}
	if all, _ := store.All(context.Background(), user, memory.TierRecall); len(all) != 0 {
		t.Error("something was stored for nobody")
	}
}

// A server with nowhere to keep memories says so.
func TestNoStoreSaysSo(t *testing.T) {
	r, err := tool.NewRegistry(memories.All(nil)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	got := call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"Roof quote","body":"forty thousand"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want a failure", got.Outcome)
	}
}

// One person's memory is not reachable by another.
func TestAnotherPersonCannotReachIt(t *testing.T) {
	r, store, _ := harness(t)

	call(t, r, chat.ChannelDirect, "memory_remember",
		`{"subject":"Roof quote","body":"forty thousand rupees"}`)
	all, _ := store.All(context.Background(), user, memory.TierRecall)

	got := r.Call(context.Background(), "memory_update", tool.Invocation{
		Caller: tool.Caller{UserID: "usr_01M3D477HXQ4YNQX7BNXJZZCV1", Channel: chat.ChannelDirect},
		Args:   json.RawMessage(`{"id":"` + all[0].ID + `","subject":"x","body":"y"}`),
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want a failure", got.Outcome)
	}
}
