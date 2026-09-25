package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// usable : A tool that passes registration, for a test changing one thing
// about it.
func usable() tool.Tool {
	return tool.Tool{
		Name:     "conversation_list",
		Purpose:  "List the conversations, newest first.",
		UseWhen:  "You need a conversation's identifier.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"limit": {Type: "integer", Description: "How many to return.", Minimum: tool.Bound(1)},
			},
		},
		Run: func(context.Context, tool.Invocation) tool.Result { return tool.OK("none") },
	}
}

// A tool that cannot be described is refused when it is registered, not when
// somebody speaks. Finding out from a wrong answer costs far more.
func TestAToolMustBeDescribable(t *testing.T) {
	cases := map[string]func(*tool.Tool){
		"no name":        func(x *tool.Tool) { x.Name = "" },
		"no purpose":     func(x *tool.Tool) { x.Purpose = "" },
		"no use-when":    func(x *tool.Tool) { x.UseWhen = " " },
		"no channel":     func(x *tool.Tool) { x.Channels = nil },
		"does nothing":   func(x *tool.Tool) { x.Run = nil },
		"vague argument": func(x *tool.Tool) { x.Params.Properties["limit"] = tool.Property{Type: "integer"} },
		"untyped":        func(x *tool.Tool) { x.Params.Properties["limit"] = tool.Property{Description: "How many."} },
	}

	for name, break_ := range cases {
		broken := usable()
		break_(&broken)
		if _, err := tool.NewRegistry(broken); err == nil {
			t.Errorf("a tool with %s was accepted", name)
		}
	}
}

// Requiring an argument the tool does not take is a typo that would make
// every call fail, and it is findable without running anything.
func TestRequiringAnUnknownArgumentIsRefused(t *testing.T) {
	broken := usable()
	broken.Params.Required = []string{"conversation_id"}

	if _, err := tool.NewRegistry(broken); err == nil {
		t.Error("a tool requiring an argument it does not take was accepted")
	}
}

// Two tools by one name is a registration that silently loses one of them.
func TestAToolCannotBeRegisteredTwice(t *testing.T) {
	if _, err := tool.NewRegistry(usable(), usable()); err == nil {
		t.Error("the same tool was registered twice")
	}
}

// The gate: a channel is only offered what it may reach.
func TestAChannelIsOnlyOfferedWhatItMayReach(t *testing.T) {
	destroy := usable()
	destroy.Name = "conversation_delete"
	destroy.Purpose = "Destroy a conversation and everything in it."
	destroy.Channels = []chat.Channel{chat.ChannelDirect}

	r, err := tool.NewRegistry(usable(), destroy)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if got := len(r.For(chat.ChannelVoice)); got != 1 {
		t.Errorf("voice is offered %d tools, want only the one it may reach", got)
	}
	if got := len(r.For(chat.ChannelDirect)); got != 2 {
		t.Errorf("typed is offered %d tools, want both", got)
	}
}

// The gate again, at the other end. What the model is told is a prompt, and a
// prompt is not a boundary: a model that has been told wrongly, or that is
// repeating a call from earlier in a conversation, is stopped here.
func TestAGatedToolIsRefusedEvenIfCalled(t *testing.T) {
	var ran bool
	destroy := usable()
	destroy.Name = "conversation_delete"
	destroy.Channels = []chat.Channel{chat.ChannelDirect}
	destroy.Run = func(context.Context, tool.Invocation) tool.Result {
		ran = true
		return tool.OK("destroyed")
	}

	r, _ := tool.NewRegistry(destroy)
	got := r.Call(context.Background(), "conversation_delete", tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelVoice},
	})

	if ran {
		t.Error("a tool voice may not reach was run anyway")
	}
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %q, want a failure", got.Outcome)
	}
	// Refused out loud. A tool that silently does nothing leaves the model to
	// invent a reason, and it will invent a plausible one.
	if !strings.Contains(got.Content, "channel") {
		t.Errorf("refusal = %q, want it to say why", got.Content)
	}
}

// A name nobody has is answered with the names somebody does have, so the
// model can correct itself rather than guess again.
func TestAnUnknownToolListsTheRealOnes(t *testing.T) {
	r, _ := tool.NewRegistry(usable())

	got := r.Call(context.Background(), "conversation_destroy", tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelDirect},
	})

	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %q, want a failure", got.Outcome)
	}
	if !strings.Contains(got.Content, "conversation_list") {
		t.Errorf("refusal = %q, want it to name the tools that exist", got.Content)
	}
}

// The schema has to be the JSON Schema a service expects, with every argument
// described in it.
func TestTheSchemaRendersAsJSONSchema(t *testing.T) {
	raw, err := json.Marshal(usable().Params)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("the schema is not valid JSON: %v", err)
	}

	if got["type"] != "object" {
		t.Errorf("type = %v, want object", got["type"])
	}
	if _, ok := got["required"]; !ok {
		t.Error("required is absent, which reads as unknown rather than as none")
	}

	props, _ := got["properties"].(map[string]any)
	limit, _ := props["limit"].(map[string]any)
	if limit["description"] == "" || limit["description"] == nil {
		t.Error("the argument reached the model with no description")
	}
	if limit["minimum"] != float64(1) {
		t.Errorf("minimum = %v, want the bound carried through", limit["minimum"])
	}
}

// The description the model reads says what the tool is for and when to reach
// for it, in a fixed order, so no tool has to shout to be noticed.
func TestTheDescriptionSaysWhenToUseIt(t *testing.T) {
	x := usable()
	x.Avoid = "Do not use it to read a conversation."
	x.Examples = []tool.Example{{Ask: "what have we talked about", Args: `{"limit":5}`}}

	got := x.Description()
	for _, want := range []string{"List the conversations", "Use when:", "Do not use:", `{"limit":5}`} {
		if !strings.Contains(got, want) {
			t.Errorf("description is missing %q: %s", want, got)
		}
	}
}
