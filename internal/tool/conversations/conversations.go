// Package conversations lets the assistant manage its own conversations.
//
// Every one of these is a thin call to something the API already does, so a
// tool adds no behaviour of its own and cannot drift from what the settings
// screen does. What it adds is a description written for a model rather than
// for a person.
package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// idPattern : The shape of a conversation identifier, so one the model
// invented is refused before it reaches the database.
const idPattern = `^conv_[0-9A-HJKMNP-TV-Z]{26}$`

// Listed : How many conversations a listing returns by default.
const Listed = 10

// All : Every conversation tool, in the order they are offered.
//
// Reading and reversible things may be said out loud. Deleting may not: on
// voice a misheard sentence is the whole authorisation, and there is no undo.
func All(repo chat.Repository) []tool.Tool {
	return []tool.Tool{
		list(repo),
		find(repo),
		switchTo(repo),
		create(repo),
		rename(repo),
		archive(repo),
		remove(repo),
	}
}

// list : The conversations, newest first.
func list(repo chat.Repository) tool.Tool {
	return tool.Tool{
		Name:     "conversation_list",
		Purpose:  "List the person's conversations, newest first, with their identifiers.",
		UseWhen:  "You need a conversation's identifier, or the person asks what they have been talking about.",
		Avoid:    "Do not call it twice in one turn: the identifiers do not change while you are answering.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"limit": {
					Type: "integer", Description: "How many to return, newest first.",
					Minimum: tool.Bound(1), Maximum: tool.Bound(50), Default: Listed,
				},
				"archived": {
					Type:        "boolean",
					Description: "List put-away conversations instead of the ones in use. These are two separate listings, not a filter on one.",
					Default:     false,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what have we been talking about", Args: `{"limit":5}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Limit    int  `json:"limit"`
				Archived bool `json:"archived"`
			}
			args.Limit = Listed
			_ = json.Unmarshal(in.Args, &args)

			found, err := listing(ctx, repo, in.Caller.UserID, args.Limit, args.Archived)
			if err != nil {
				return tool.Failed(err.Error())
			}
			if len(found) == 0 {
				return tool.OK("There are no conversations.")
			}
			return tool.OK(describe(found, in.Caller.ConversationID))
		},
	}
}

// find : Conversations whose name matches what the person said.
func find(repo chat.Repository) tool.Tool {
	return tool.Tool{
		Name:    "conversation_find",
		Purpose: "Find conversations whose name matches words the person used.",
		UseWhen: "The person refers to a conversation by name rather than by identifier, which is always the case when speaking.",
		Avoid: "Do not choose between several matches yourself. If more than one comes back, " +
			"say which they are and ask which was meant.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"name": {
					Type:        "string",
					Description: "The words the person used for the conversation, as they said them.",
				},
			},
			Required: []string{"name"},
		},
		Examples: []tool.Example{
			{Ask: "go back to the roof conversation", Args: `{"name":"roof"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(in.Args, &args)

			found, err := listing(ctx, repo, in.Caller.UserID, 50, false)
			if err != nil {
				return tool.Failed(err.Error())
			}

			matched := matching(found, args.Name)
			switch len(matched) {
			case 0:
				// Said plainly rather than answered with the newest. A
				// best guess here is a wrong conversation switched into
				// silently, which is worse than an honest miss.
				return tool.OK(fmt.Sprintf(
					"Nothing matches %q. Tell the person so rather than guessing at another conversation.",
					args.Name))
			case 1:
				return tool.OK("One match. " + describe(matched, in.Caller.ConversationID))
			default:
				return tool.OK(fmt.Sprintf(
					"%d conversations match. Ask the person which they meant; do not choose. %s",
					len(matched), describe(matched, in.Caller.ConversationID)))
			}
		},
	}
}

// switchTo : Make a conversation the one this client talks in.
func switchTo(repo chat.Repository) tool.Tool {
	return tool.Tool{
		Name:    "conversation_switch",
		Purpose: "Make a conversation the one this client talks in from now on.",
		UseWhen: "The person asks to go back to, or carry on with, a particular conversation.",
		Avoid: "Do not use it to answer a question about another conversation's contents. " +
			"Switching does not read it, and this turn still answers from the conversation it began in.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"conversation_id": {
					Type:        "string",
					Description: "An identifier from conversation_list or conversation_find. Never a name, and never invented.",
					Pattern:     idPattern,
				},
			},
			Required: []string{"conversation_id"},
		},
		Examples: []tool.Example{
			{Ask: "go back to the roof conversation", Args: `{"conversation_id":"conv_01M3CYNB9SKNBJ4WYX8A722VTX"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ConversationID string `json:"conversation_id"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if in.Caller.ClientID == "" {
				return tool.Failed("This request did not come from a known client, so there is nothing to switch.")
			}

			err := repo.SetActiveConversation(ctx, in.Caller.UserID, in.Caller.ClientID, args.ConversationID)
			if err != nil {
				return tool.Failed(refusal(err, args.ConversationID))
			}
			return tool.OK("Switched. It takes effect from the next thing the person says; " +
				"this turn still answers from the conversation it began in.")
		},
	}
}

// create : Start a fresh conversation.
func create(repo chat.Repository) tool.Tool {
	return tool.Tool{
		Name:    "conversation_new",
		Purpose: "Start a fresh conversation and switch to it.",
		UseWhen: "The person asks to start over, or to begin something separate.",
		Avoid: "Do not start one merely because the subject changed. " +
			"One conversation holds several subjects perfectly well.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"title": {
					Type:        "string",
					Description: "What to call it. Leave it out unless the person said what to call it; it names itself otherwise.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "start a new conversation", Args: `{}`},
			{Ask: "start a new conversation about the roof", Args: `{"title":"Roof"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if in.Caller.UserID == "" {
				return tool.Failed("There is nobody to start a conversation for.")
			}

			c := chat.NewConversation(in.Caller.UserID, args.Title)
			if err := repo.CreateConversation(ctx, c); err != nil {
				return tool.Failed("The conversation could not be created: " + err.Error())
			}
			if in.Caller.ClientID != "" {
				if err := repo.SetActiveConversation(ctx, in.Caller.UserID, in.Caller.ClientID, c.ID); err != nil {
					// Made but not switched to, which is a partial outcome
					// and has to be reported as one: saying "done" would
					// have the person talking into the old one.
					return tool.Partial("The conversation was created but this client could not be switched to it: " + err.Error())
				}
			}
			return tool.OK("Created and switched to, from the next thing the person says.")
		},
	}
}

// rename : Change what a conversation is called.
func rename(repo chat.Repository) tool.Tool {
	return tool.Tool{
		Name:     "conversation_rename",
		Purpose:  "Change what a conversation is called.",
		UseWhen:  "The person says what to call this conversation or another one.",
		Avoid:    "Do not rename to summarise. Only when asked.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"conversation_id": {
					Type:        "string",
					Description: `An identifier from conversation_list, or the word "current" for the one in progress.`,
				},
				"title": {
					Type:        "string",
					Description: "The new name. An empty string clears it, which is allowed and leaves it untitled.",
				},
			},
			Required: []string{"conversation_id", "title"},
		},
		Examples: []tool.Example{
			{Ask: "call this one roof quotes", Args: `{"conversation_id":"current","title":"Roof Quotes"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ConversationID string `json:"conversation_id"`
				Title          string `json:"title"`
			}
			_ = json.Unmarshal(in.Args, &args)

			id, bad := resolve(args.ConversationID, in.Caller)
			if bad != "" {
				return tool.Failed(bad)
			}

			if err := repo.RenameConversation(ctx, in.Caller.UserID, id, args.Title); err != nil {
				if errors.Is(err, chat.ErrTitleTooLong) {
					return tool.Failed("That name is too long. It may be up to 200 characters.")
				}
				return tool.Failed(refusal(err, id))
			}
			if strings.TrimSpace(args.Title) == "" {
				return tool.OK("The name was cleared.")
			}
			return tool.OK("Renamed to " + args.Title + ".")
		},
	}
}

// archive : Put a conversation away, or bring it back.
func archive(repo chat.Repository) tool.Tool {
	return tool.Tool{
		Name:    "conversation_archive",
		Purpose: "Put a conversation away, or bring a put-away one back.",
		UseWhen: "The person is finished with a conversation, or wants one back.",
		Avoid: "Do not use it to delete. Archiving keeps everything and can be undone, " +
			"which is why it is what to reach for when the person is merely done.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"conversation_id": {
					Type:        "string",
					Description: `An identifier from conversation_list, or the word "current".`,
				},
				"archived": {
					Type:        "boolean",
					Description: "True puts it away. False brings it back.",
				},
			},
			Required: []string{"conversation_id", "archived"},
		},
		Examples: []tool.Example{
			{Ask: "put this conversation away", Args: `{"conversation_id":"current","archived":true}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ConversationID string `json:"conversation_id"`
				Archived       bool   `json:"archived"`
			}
			_ = json.Unmarshal(in.Args, &args)

			id, bad := resolve(args.ConversationID, in.Caller)
			if bad != "" {
				return tool.Failed(bad)
			}

			if err := repo.SetConversationArchived(ctx, in.Caller.UserID, id, args.Archived); err != nil {
				return tool.Failed(refusal(err, id))
			}
			if args.Archived {
				return tool.OK("Put away. Nothing said in it was lost, and it can be brought back.")
			}
			return tool.OK("Brought back.")
		},
	}
}

// remove : Destroy a conversation and everything in it.
func remove(repo chat.Repository) tool.Tool {
	return tool.Tool{
		Name:    "conversation_delete",
		Purpose: "Destroy a conversation and every message in it. This cannot be undone.",
		UseWhen: "The person has asked, in those words, for a particular conversation to be deleted.",
		Avoid: "Never as a tidy-up, and never inferred from the person being finished with something. " +
			"Use conversation_archive unless deletion was actually asked for.",
		// Typed only. On voice a misheard sentence is the whole
		// authorisation, and nothing here can be undone.
		Channels: []chat.Channel{chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"conversation_id": {
					Type:        "string",
					Description: `An identifier from conversation_list. The word "current" is not accepted: deleting the conversation in progress has to be deliberate enough to name.`,
					Pattern:     idPattern,
				},
				"confirm_title": {
					Type: "string",
					Description: "The conversation's exact current name, as conversation_list gave it. " +
						`Use "" if it is untitled. Nothing is deleted unless this matches.`,
				},
			},
			Required: []string{"conversation_id", "confirm_title"},
		},
		Examples: []tool.Example{
			{Ask: "delete the roof quotes conversation",
				Args: `{"conversation_id":"conv_01M3CYNB9SKNBJ4WYX8A722VTX","confirm_title":"Roof Quotes"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ConversationID string `json:"conversation_id"`
				ConfirmTitle   string `json:"confirm_title"`
			}
			_ = json.Unmarshal(in.Args, &args)

			// The title has to match. It forces a listing first, so an
			// identifier the model invented or misremembered cannot destroy
			// a real conversation: it is the cheapest possible proof that
			// the model looked before it acted.
			c, err := repo.GetConversation(ctx, args.ConversationID)
			if err != nil {
				return tool.Failed(refusal(err, args.ConversationID))
			}
			if c.UserID != in.Caller.UserID {
				return tool.Failed("There is no conversation with that identifier.")
			}
			if c.Title != args.ConfirmTitle {
				return tool.Failed(fmt.Sprintf(
					"Nothing was deleted. That conversation is called %q, not %q. Check the listing again.",
					c.Title, args.ConfirmTitle))
			}

			if err := repo.DeleteConversation(ctx, in.Caller.UserID, args.ConversationID); err != nil {
				return tool.Failed(refusal(err, args.ConversationID))
			}
			return tool.OK("Deleted, with everything said in it.")
		},
	}
}

// resolve : The identifier a tool should act on, or why it cannot.
//
// "current" is allowed on the reversible tools, because the commonest thing a
// person says is "this one" and making the model look that up first costs a
// round trip for nothing.
func resolve(given string, caller tool.Caller) (string, string) {
	if strings.EqualFold(strings.TrimSpace(given), "current") {
		if caller.ConversationID == "" {
			return "", "There is no conversation in progress to act on."
		}
		return caller.ConversationID, ""
	}
	if !chat.ValidConversationID(given) {
		return "", fmt.Sprintf(
			"%q is not a conversation identifier. Use conversation_list or conversation_find to get one.", given)
	}
	return given, ""
}

// refusal : What to tell the model about a storage failure.
//
// A conversation that is not there and one belonging to somebody else read
// the same, on purpose: telling them apart would say whether an identifier
// exists.
func refusal(err error, id string) string {
	switch {
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrNotOwned):
		return fmt.Sprintf("There is no conversation with the identifier %q.", id)
	default:
		return "That could not be done: " + err.Error()
	}
}

// listing : The conversations to choose from.
func listing(ctx context.Context, repo chat.Repository, userID string, limit int, archived bool) ([]chat.Conversation, error) {
	if userID == "" {
		return nil, errors.New("there is nobody to list conversations for")
	}
	if archived {
		return repo.ListArchivedConversations(ctx, userID, limit)
	}
	return repo.ListConversations(ctx, userID, limit)
}

// describe : The conversations as the model should read them.
//
// One per line with the identifier first, because the identifier is the part
// the model has to copy exactly and anything it has to copy should be easy to
// find.
func describe(found []chat.Conversation, current string) string {
	var b strings.Builder
	for _, c := range found {
		title := c.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "%s — %s, last used %s", c.ID, title, ago(c.UpdatedAt))
		if c.ID == current {
			b.WriteString(" (this one)")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// matching : The conversations whose name matches what was said.
//
// Whole words rather than substrings, because the words come from speech and
// a substring match would have "roof" find "proof of purchase". An exact name
// wins outright; otherwise every word has to appear, and failing that any
// word does.
func matching(found []chat.Conversation, name string) []chat.Conversation {
	wanted := strings.Fields(strings.ToLower(strings.TrimSpace(name)))
	if len(wanted) == 0 {
		return nil
	}

	var exact, all, some []chat.Conversation
	for _, c := range found {
		title := strings.ToLower(c.Title)
		if title == "" {
			continue
		}
		if title == strings.ToLower(name) {
			exact = append(exact, c)
			continue
		}

		words := map[string]bool{}
		for _, w := range strings.Fields(title) {
			words[strings.Trim(w, ".,:;!?'\"")] = true
		}

		hits := 0
		for _, w := range wanted {
			if words[w] {
				hits++
			}
		}
		switch {
		case hits == len(wanted):
			all = append(all, c)
		case hits > 0:
			some = append(some, c)
		}
	}

	if len(exact) > 0 {
		return exact
	}
	if len(all) > 0 {
		return all
	}
	return some
}

// ago : How long ago, in words, since a model reading a timestamp has to do
// arithmetic to know whether something is recent.
func ago(at time.Time) string {
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
