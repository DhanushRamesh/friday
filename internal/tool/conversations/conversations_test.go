package conversations_test

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
)

// Every conversation tool has to survive registration, which is where a
// missing description or an untyped argument is caught.
func TestEveryConversationToolRegisters(t *testing.T) {
	if _, err := tool.NewRegistry(conversations.All(nil)...); err != nil {
		t.Fatalf("a conversation tool is not usable: %v", err)
	}
}

// Deleting is the one thing that cannot be undone, and on voice a misheard
// sentence is the whole authorisation.
func TestVoiceCannotDelete(t *testing.T) {
	r, err := tool.NewRegistry(conversations.All(nil)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, x := range r.For(chat.ChannelVoice) {
		if x.Name == "conversation_delete" {
			t.Error("voice is offered conversation_delete")
		}
	}

	var typedHasIt bool
	for _, x := range r.For(chat.ChannelDirect) {
		if x.Name == "conversation_delete" {
			typedHasIt = true
		}
	}
	if !typedHasIt {
		t.Error("typed cannot delete either, so nothing can")
	}
}

// Every tool shows the model at least one worked call, and the examples have
// to be valid against the tool's own schema: an example that lies about its
// arguments teaches the model to get them wrong.
func TestEveryExampleMatchesItsSchema(t *testing.T) {
	for _, x := range conversations.All(nil) {
		if len(x.Examples) == 0 {
			t.Errorf("%s shows no example call", x.Name)
			continue
		}
		for _, e := range x.Examples {
			if err := tool.Validate(x.Params, []byte(e.Args)); err != nil {
				t.Errorf("%s has an example that its own schema refuses: %s — %v", x.Name, e.Args, err)
			}
		}
	}
}
