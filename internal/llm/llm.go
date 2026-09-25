// Package llm knows what each model can do.
//
// A context window is a property of the model, not of the service in front of
// it: the same model reached through two endpoints has the same window, and
// two models behind one endpoint do not. Keeping it here is what lets the
// history sent to a model be sized to the model rather than to a number
// guessed once and left behind when the configuration changes.
package llm

import "strings"

// Model : What is known about one model.
type Model struct {
	// ID : What the service calls it, and what configuration names.
	ID string
	// Name : What a person calls it.
	Name string
	// Vendor : Who makes it. Two vendors may use the same name for
	// different models, so an identifier alone does not select one.
	Vendor string
	// ContextTokens : How much the model can be given at once, prompt and
	// reply together.
	ContextTokens int
	// SupportsTools : Whether the model can be given tools and asked to call
	// them.
	//
	// A capability of the model. Which tools it is allowed to reach is not:
	// that follows from the channel the prompt arrived on.
	SupportsTools bool
}

// registry : Every model known here.
//
// A model missing from this list is not an error. Nothing it says is
// required: without it the history is held to the configured byte budget
// alone, which is far below any of these windows.
var registry = []Model{
	// Anthropic. The 4 series all take two hundred thousand.
	{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Vendor: "anthropic", ContextTokens: 200_000, SupportsTools: true},
	{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Vendor: "anthropic", ContextTokens: 200_000, SupportsTools: true},
	{ID: "claude-opus-4-5", Name: "Claude Opus 4.5", Vendor: "anthropic", ContextTokens: 200_000, SupportsTools: true},
	// The dated identifier, because the bare alias is not routed and the
	// endpoint answers "Model is not supported" to it.
	{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Vendor: "anthropic", ContextTokens: 200_000, SupportsTools: true},

	// OpenAI.
	{ID: "gpt-4o", Name: "GPT-4o", Vendor: "openai", ContextTokens: 128_000, SupportsTools: true},
	{ID: "gpt-4o-mini", Name: "GPT-4o mini", Vendor: "openai", ContextTokens: 128_000, SupportsTools: true},
	{ID: "gpt-4.1", Name: "GPT-4.1", Vendor: "openai", ContextTokens: 1_047_576, SupportsTools: true},
	{ID: "gpt-4.1-mini", Name: "GPT-4.1 mini", Vendor: "openai", ContextTokens: 1_047_576, SupportsTools: true},
	{ID: "gpt-4.1-nano", Name: "GPT-4.1 nano", Vendor: "openai", ContextTokens: 1_047_576, SupportsTools: true},

	// Ollama, on this machine. These windows are what the model ships with;
	// a Modelfile can lower them, and num_ctx at run time decides in the end.
	{ID: "qwen3:8b", Name: "Qwen 3 8B", Vendor: "ollama", ContextTokens: 32_768, SupportsTools: true},
	{ID: "llama3.2", Name: "Llama 3.2", Vendor: "ollama", ContextTokens: 131_072, SupportsTools: true},
}

// Find : The model a vendor calls id, and whether it is known.
//
// The vendor is part of the key because an identifier is only unique within
// one. An empty vendor matches on the identifier alone, for a caller that has
// no vendor to give.
func Find(vendor, id string) (Model, bool) {
	for _, m := range registry {
		if !strings.EqualFold(m.ID, id) {
			continue
		}
		if vendor == "" || strings.EqualFold(m.Vendor, vendor) {
			return m, true
		}
	}
	return Model{}, false
}

// ContextTokens : The window of the model a vendor calls id, or zero when it
// is not known.
//
// Zero is what a caller wanting a limit should treat as no limit declared,
// rather than as a model that can be given nothing.
func ContextTokens(vendor, id string) int {
	m, ok := Find(vendor, id)
	if !ok {
		return 0
	}
	return m.ContextTokens
}

// All : Every known model, in the order they are listed.
func All() []Model { return append([]Model(nil), registry...) }
