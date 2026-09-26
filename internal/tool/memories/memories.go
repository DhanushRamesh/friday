// Package memories lets the assistant keep and look up what it has been
// asked to remember.
//
// Recall happens on its own before every turn, so these are for what recall
// cannot do: writing something down, changing it when it turns out to be
// wrong, and searching on purpose when the automatic search found nothing.
package memories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// idPattern : The shape of a memory identifier, so one the model invented is
// refused before it reaches the database.
const idPattern = `^mem_[0-9A-HJKMNP-TV-Z]{26}$`

// Listed : How many memories a search returns by default.
const Listed = 5

// All : Every memory tool, in the order they are offered.
//
// Writing and changing may be said out loud; forgetting may not. On voice a
// misheard sentence is the whole authorisation, and a memory that is gone
// cannot be recovered by asking again.
func All(recall *memory.Recall) []tool.Tool {
	return []tool.Tool{
		remember(recall),
		search(recall),
		update(recall),
		forget(recall),
	}
}

// remember : Writes something down.
func remember(recall *memory.Recall) tool.Tool {
	return tool.Tool{
		Name:    "memory_remember",
		Purpose: "Write something down so it is known in later conversations.",
		UseWhen: "The person asks you to remember something, or tells you a lasting fact about themselves that they plainly expect you to keep.",
		Avoid: "Do not use it for what is only true today, for what was said in passing, " +
			"or for anything the person did not mean to be kept. Do not write down " +
			"something you were told earlier in this conversation without being asked to.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"subject", "body"},
			Properties: map[string]tool.Property{
				"subject": {
					Type:        "string",
					Description: "A few words saying what this is about, as a label. It is what a later search matches against, so say the subject rather than repeating the fact.",
				},
				"body": {
					Type:        "string",
					Description: "The thing to remember, stated plainly and in full, so it still makes sense read on its own in a year.",
				},
				"always": {
					Type: "boolean",
					Description: "True only for a fact about the person that bears on almost anything they ask, such as where they live or how they want to be answered. " +
						"These are in front of you for every question, so there is room for very few. Everything else is false.",
					Default: false,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "remember that the roofer quoted forty thousand",
				Args: `{"subject":"Roof quote","body":"The roofer quoted forty thousand rupees for the terrace work."}`},
			{Ask: "always answer me briefly",
				Args: `{"subject":"How to answer","body":"Wants answers kept short and plain, without preamble.","always":true}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Subject string `json:"subject"`
				Body    string `json:"body"`
				Always  bool   `json:"always"`
			}
			_ = json.Unmarshal(in.Args, &args)

			store, fail := storeFor(recall, in)
			if fail != nil {
				return *fail
			}

			tier := memory.TierRecall
			if args.Always {
				tier = memory.TierAlways
			}

			m, err := memory.New(in.Caller.UserID, tier, args.Subject, args.Body)
			if err != nil {
				return tool.Failed(err.Error())
			}
			if err := store.Create(ctx, m); err != nil {
				return tool.Failed(err.Error())
			}

			// Without a vector it is found only by its wording until the next
			// catch-up, so this is attempted now and its failure reported
			// rather than hidden: the memory is stored either way.
			if err := recall.EmbedOne(ctx, m); err != nil {
				return tool.Partial(fmt.Sprintf(
					"Remembered %q, with the identifier %s. It could not be indexed for searching by meaning (%s), "+
						"so until that is working it will only be found when the wording matches.", m.Subject, m.ID, err))
			}
			return tool.OK(fmt.Sprintf("Remembered %q, with the identifier %s.", m.Subject, m.ID))
		},
	}
}

// search : Looks through what is remembered, on purpose.
func search(recall *memory.Recall) tool.Tool {
	return tool.Tool{
		Name:     "memory_search",
		Purpose:  "Search what you have been asked to remember, and return the closest with their identifiers.",
		UseWhen:  "The person refers to something you were told before and it is not already in front of you, or you need a memory's identifier in order to change or forget it.",
		Avoid:    "Do not call it to answer a question that the notes already in front of you answer, and do not call it twice with the same words.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"about"},
			Properties: map[string]tool.Property{
				"about": {
					Type:        "string",
					Description: "What to look for, in your own words. A description of the subject finds more than a repeat of the question.",
				},
				"limit": {
					Type: "integer", Description: "How many to return, closest first.",
					Minimum: tool.Bound(1), Maximum: tool.Bound(25), Default: Listed,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what did I say the roof would cost", Args: `{"about":"the quote for the roof"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				About string `json:"about"`
				Limit int    `json:"limit"`
			}
			args.Limit = Listed
			_ = json.Unmarshal(in.Args, &args)

			if _, fail := storeFor(recall, in); fail != nil {
				return *fail
			}

			looking := *recall
			looking.Candidates = args.Limit

			found, err := looking.For(ctx, in.Caller.UserID, args.About)
			if err != nil {
				return tool.Failed(err.Error())
			}
			if len(found) == 0 {
				return tool.OK("Nothing is remembered about that.")
			}
			return tool.OK(describe(found))
		},
	}
}

// update : Changes a memory that has turned out to be wrong.
func update(recall *memory.Recall) tool.Tool {
	return tool.Tool{
		Name:     "memory_update",
		Purpose:  "Replace what a memory says, keeping the same identifier.",
		UseWhen:  "Something you remember has changed or was wrong, and the person has told you what it should say.",
		Avoid:    "Do not guess the identifier. Search first, and change the memory you found rather than writing a second one about the same thing.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id", "subject", "body"},
			Properties: map[string]tool.Property{
				"id": {
					Type: "string", Description: "The memory's identifier, from a search.",
					Pattern: idPattern,
				},
				"subject": {Type: "string", Description: "What it is about, as a label. Repeat the old one if it has not changed."},
				"body":    {Type: "string", Description: "What it should say now, in full. This replaces the old text rather than being added to it."},
				"always": {
					Type:        "boolean",
					Description: "Whether it should now be in front of you for every question.",
					Default:     false,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "the roofer actually said fifty thousand",
				Args: `{"id":"mem_01M3D477HXQ4YNQX7BNXJZZCV0","subject":"Roof quote","body":"The roofer quoted fifty thousand rupees for the terrace work."}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID      string `json:"id"`
				Subject string `json:"subject"`
				Body    string `json:"body"`
				Always  bool   `json:"always"`
			}
			_ = json.Unmarshal(in.Args, &args)

			store, fail := storeFor(recall, in)
			if fail != nil {
				return *fail
			}

			existing, err := store.Get(ctx, in.Caller.UserID, args.ID)
			if err != nil {
				return tool.Failed(notFound(err, args.ID))
			}

			existing.Subject, existing.Body = strings.TrimSpace(args.Subject), strings.TrimSpace(args.Body)
			existing.Tier = memory.TierRecall
			if args.Always {
				existing.Tier = memory.TierAlways
			}
			if err := store.Update(ctx, existing); err != nil {
				return tool.Failed(err.Error())
			}

			if err := recall.EmbedOne(ctx, existing); err != nil {
				return tool.Partial(fmt.Sprintf(
					"Changed %s. It could not be re-indexed for searching by meaning (%s), "+
						"so until that is working it will only be found when the wording matches.", args.ID, err))
			}
			return tool.OK(fmt.Sprintf("Changed %s to say: %s", args.ID, existing.Text()))
		},
	}
}

// forget : Removes a memory.
func forget(recall *memory.Recall) tool.Tool {
	return tool.Tool{
		Name:    "memory_forget",
		Purpose: "Forget something, permanently.",
		UseWhen: "The person asks you to forget something and has made clear which one.",
		Avoid: "There is no undo. Do not guess the identifier, and do not forget something because it looks wrong or stale -- " +
			"only because the person asked. If more than one memory could be the one they mean, ask which.",
		// Typed only. Spoken, a misheard sentence is the whole
		// authorisation, and nothing can be recovered by asking again.
		Channels: []chat.Channel{chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id", "confirm_subject"},
			Properties: map[string]tool.Property{
				"id": {
					Type: "string", Description: "The memory's identifier, from a search.",
					Pattern: idPattern,
				},
				"confirm_subject": {
					Type:        "string",
					Description: "The subject of that exact memory, copied from the search result. It is checked against the stored one, so a wrong identifier forgets nothing.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "forget what I told you about the roof",
				Args: `{"id":"mem_01M3D477HXQ4YNQX7BNXJZZCV0","confirm_subject":"Roof quote"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID      string `json:"id"`
				Confirm string `json:"confirm_subject"`
			}
			_ = json.Unmarshal(in.Args, &args)

			store, fail := storeFor(recall, in)
			if fail != nil {
				return *fail
			}

			existing, err := store.Get(ctx, in.Caller.UserID, args.ID)
			if err != nil {
				return tool.Failed(notFound(err, args.ID))
			}
			if !strings.EqualFold(strings.TrimSpace(args.Confirm), existing.Subject) {
				return tool.Failed(fmt.Sprintf(
					"Nothing was forgotten: %s is %q, not %q. Search again and use the subject exactly as it came back.",
					args.ID, existing.Subject, args.Confirm))
			}
			if err := store.Forget(ctx, in.Caller.UserID, args.ID); err != nil {
				return tool.Failed(err.Error())
			}
			return tool.OK(fmt.Sprintf("Forgotten %q.", existing.Subject))
		},
	}
}

// storeFor : The store to act on, or the result to return instead.
//
// A tool with nowhere to write, or acting for nobody, says so rather than
// reporting a success that stored nothing.
func storeFor(recall *memory.Recall, in tool.Invocation) (memory.Store, *tool.Result) {
	if recall == nil || recall.Store == nil {
		fail := tool.Failed("There is nowhere to keep memories on this server.")
		return nil, &fail
	}
	if in.Caller.UserID == "" {
		fail := tool.Failed("This request did not come from a known person, so there is nobody to remember it for.")
		return nil, &fail
	}
	return recall.Store, nil
}

// notFound : What to say when a memory cannot be read.
func notFound(err error, id string) string {
	if errors.Is(err, memory.ErrNotFound) {
		return fmt.Sprintf("There is no memory with the identifier %s. Search for it rather than guessing the identifier.", id)
	}
	return err.Error()
}

// describe : Search results, as the model is shown them.
func describe(found []memory.Match) string {
	var b strings.Builder
	b.WriteString("Closest first. Each line is an identifier, then what is remembered.\n")
	for i := range found {
		b.WriteString("\n")
		b.WriteString(found[i].Memory.ID)
		b.WriteString("  ")
		b.WriteString(found[i].Memory.Text())
	}
	return b.String()
}
