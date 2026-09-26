// Package reminders lets the assistant say something at a time rather than
// because it was asked.
//
// A reminder only ever says something. There is no tool here for making one
// that runs an instruction, because a task firing with nobody watching has
// nobody to catch it.
package reminders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// idPattern : The shape of a reminder identifier, so one the model invented
// is refused before it reaches the database.
const idPattern = `^rem_[0-9A-HJKMNP-TV-Z]{26}$`

// Bounds on when something may be set for.
const (
	// MaxMinutes : The longest a relative reminder may be, in minutes.
	// A year, which is past anything anybody says out loud.
	MaxMinutes = 525600
	// MinSeconds : The shortest a reminder may be, in seconds.
	//
	// Below this it would fire while the assistant is still saying that it
	// has been set.
	MinSeconds = 5
	// MaxSeconds : The longest worth saying in seconds rather than minutes.
	MaxSeconds = 86400
	// MaxAhead : The furthest ahead an absolute one may be set.
	MaxAhead = 5 * 365 * 24 * time.Hour
	// Slack : How far in the past a time may be and still be taken as now.
	//
	// The model works the time out from what it was told, and a second or
	// two passes while it does. Refusing those would refuse "in a minute".
	Slack = 2 * time.Minute
)

// layouts : How a written time may be spelt.
//
// Several, because a model writes the same moment several ways and
// refusing it over a missing "T" would be refusing the reminder.
var layouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

// Clock : What the tools need to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written hour means.
	// Nil is UTC.
	Location *time.Location
}

// now : The moment, in UTC.
func (c Clock) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

// where : The person's zone.
func (c Clock) where() *time.Location {
	if c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// All : Every reminder tool, in the order they are offered.
//
// All three may be said out loud. None of them destroys anything: a
// cancelled reminder that was wanted after all is set again in a sentence.
func All(store remind.Store, clock Clock) []tool.Tool {
	return []tool.Tool{
		set(store, clock),
		list(store, clock),
		cancel(store),
	}
}

// set : Arranges for something to be said later.
func set(store remind.Store, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "reminder_set",
		Purpose: "Arrange for something to be said out loud at a time, once or repeatedly.",
		UseWhen: "The person asks to be reminded, wants a timer, or wants telling at a time or on a day.",
		Avoid: "Give exactly one of seconds_from_now, minutes_from_now or at. Use one of the first two for " +
			"anything said as a length of time, so the arithmetic is not yours to get wrong: do not " +
			"convert seconds into minutes, and never refuse a length because it is not a whole number " +
			"of minutes. Do not use this to write something down for later reference: that is " +
			"memory_remember.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"title", "say"},
			Properties: map[string]tool.Property{
				"title": {
					Type:        "string",
					Description: "A few words naming it, for a listing and for cancelling it later. Not what gets said.",
				},
				"say": {
					Type:        "string",
					Description: "Exactly what should be spoken when the time comes, as a whole sentence. It is read out with nothing around it, so make it make sense on its own.",
				},
				"seconds_from_now": {
					Type:    "integer",
					Minimum: tool.Bound(MinSeconds), Maximum: tool.Bound(MaxSeconds),
					Description: "For a length of time said in seconds. Thirty seconds is 30, ninety seconds is 90.",
				},
				"minutes_from_now": {
					Type:    "integer",
					Minimum: tool.Bound(1), Maximum: tool.Bound(MaxMinutes),
					Description: "For a length of time said in minutes or hours. Twenty minutes is 20, two hours is 120.",
				},
				"at": {
					Type: "string",
					Description: "For a time or a date, written as 2006-01-02 15:04 in the person's own local time. " +
						"You are told the current local time; work it out from that. Leave out if using minutes_from_now.",
				},
				"repeats": {
					Type: "string", Enum: repeatWords(),
					Description: "How often it comes back. Leave out for once only.",
				},
				"scope": {
					Type: "string", Enum: []string{string(remind.ScopeUser), string(remind.ScopeClient)},
					Description: "user follows the person and is almost always right. client ties it to the device this was asked on.",
					Default:     string(remind.ScopeUser),
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "set a timer for twenty minutes",
				Args: `{"title":"Timer","say":"Your twenty minute timer has finished.","minutes_from_now":20}`},
			{Ask: "set a timer for thirty seconds",
				Args: `{"title":"Timer","say":"Your thirty second timer has finished.","seconds_from_now":30}`},
			{Ask: "remind me to call the roofer at half past four",
				Args: `{"title":"Call the roofer","say":"Time to call the roofer.","at":"2026-09-26 16:30"}`},
			{Ask: "wake me at seven every weekday",
				Args: `{"title":"Wake up","say":"It is seven o'clock.","at":"2026-09-28 07:00","repeats":"weekdays"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Title   string `json:"title"`
				Say     string `json:"say"`
				Seconds int    `json:"seconds_from_now"`
				Minutes int    `json:"minutes_from_now"`
				At      string `json:"at"`
				Repeats string `json:"repeats"`
				Scope   string `json:"scope"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person, so there is nobody to remind.")
			}

			due, fail := when(clock, args.Seconds, args.Minutes, args.At)
			if fail != "" {
				return tool.Failed(fail)
			}

			scope := remind.Scope(args.Scope)
			if args.Scope == "" {
				scope = remind.ScopeUser
			}
			if scope == remind.ScopeClient && in.Caller.ClientID == "" {
				return tool.Failed("This did not come from a known device, so it cannot be tied to one. Set it without a scope.")
			}

			r, err := remind.New(in.Caller.UserID, in.Caller.ClientID, scope,
				args.Title, args.Say, due, remind.Repeat(args.Repeats))
			if err != nil {
				return tool.Failed(err.Error())
			}
			if err := store.Create(ctx, r); err != nil {
				return tool.Failed(err.Error())
			}

			return tool.OK(fmt.Sprintf("Set: %q, %s%s. Its identifier is %s. Tell the person when it will happen, "+
				"in their words rather than as a date.",
				r.Title, spell(r.DueAt, clock.where()), repeating(r.Repeats), r.ID))
		},
	}
}

// list : What is waiting to be said.
func list(store remind.Store, clock Clock) tool.Tool {
	return tool.Tool{
		Name:     "reminder_list",
		Purpose:  "List what is waiting to be said, soonest first, with their identifiers.",
		UseWhen:  "The person asks what reminders or timers they have, or you need an identifier in order to cancel one.",
		Avoid:    "Do not call it twice in one turn: nothing changes while you are answering.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"include_finished": {
					Type:        "boolean",
					Description: "Also list ones that have already happened, been missed or been called off.",
					Default:     false,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what timers do I have", Args: `{}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Finished bool `json:"include_finished"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			states := []remind.Status{remind.Pending}
			if args.Finished {
				states = nil
			}

			found, err := store.List(ctx, in.Caller.UserID, states...)
			if err != nil {
				return tool.Failed(err.Error())
			}
			if len(found) == 0 {
				return tool.OK("There is nothing waiting to be said.")
			}
			return tool.OK(describe(found, clock))
		},
	}
}

// cancel : Calls one off.
func cancel(store remind.Store) tool.Tool {
	return tool.Tool{
		Name:     "reminder_cancel",
		Purpose:  "Call off something that was going to be said.",
		UseWhen:  "The person asks to cancel or stop a reminder or timer.",
		Avoid:    "Do not guess the identifier: list them first. If more than one could be the one they mean, ask which.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {
					Type: "string", Description: "The reminder's identifier, from a listing.",
					Pattern: idPattern,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "cancel that timer", Args: `{"id":"rem_01M3D477HXQ4YNQX7BNXJZZCV0"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			existing, err := store.Get(ctx, in.Caller.UserID, args.ID)
			if err != nil {
				if errors.Is(err, remind.ErrNotFound) {
					return tool.Failed(fmt.Sprintf(
						"There is no reminder with the identifier %s. List them rather than guessing.", args.ID))
				}
				return tool.Failed(err.Error())
			}
			if err := store.Cancel(ctx, in.Caller.UserID, args.ID); err != nil {
				return tool.Failed(err.Error())
			}
			return tool.OK(fmt.Sprintf("Called off %q.", existing.Title))
		},
	}
}

// when : The moment a reminder is due, or why it cannot be worked out.
//
// Minutes are preferred to a written time wherever the person said a
// length, because the arithmetic is then the server's rather than the
// model's.
func when(clock Clock, seconds, minutes int, written string) (time.Time, string) {
	now := clock.now()
	written = strings.TrimSpace(written)

	var given int
	for _, set := range []bool{seconds > 0, minutes > 0, written != ""} {
		if set {
			given++
		}
	}

	switch {
	case given > 1:
		return time.Time{}, "Give exactly one of seconds_from_now, minutes_from_now or at. Which was meant?"

	case seconds > 0:
		switch {
		case seconds < MinSeconds:
			return time.Time{}, fmt.Sprintf(
				"seconds_from_now was %d, and the least allowed is %d: anything shorter goes off while you are still saying it is set.",
				seconds, MinSeconds)
		case seconds > MaxSeconds:
			return time.Time{}, fmt.Sprintf(
				"seconds_from_now was %d, and the most allowed is %d. Use minutes_from_now for anything longer.",
				seconds, MaxSeconds)
		}
		return now.Add(time.Duration(seconds) * time.Second), ""

	case minutes > 0:
		if minutes > MaxMinutes {
			return time.Time{}, fmt.Sprintf("minutes_from_now was %d, and the most allowed is %d.", minutes, MaxMinutes)
		}
		return now.Add(time.Duration(minutes) * time.Minute), ""

	case written != "":
		at, ok := parse(written, clock.where())
		if !ok {
			return time.Time{}, fmt.Sprintf(
				"%q is not a time this understands. Write it as 2006-01-02 15:04 in the person's local time.", written)
		}
		switch {
		case at.Before(now.Add(-Slack)):
			return time.Time{}, fmt.Sprintf(
				"%s is in the past. The current local time is %s; work it out from that.",
				spell(at, clock.where()), clock.now().In(clock.where()).Format("2006-01-02 15:04"))
		case at.After(now.Add(MaxAhead)):
			return time.Time{}, "That is further ahead than this will hold. Was the year right?"
		}
		return at, ""
	}

	return time.Time{}, "Say when: either minutes_from_now for a length of time, or at for a time of day."
}

// parse : A written time, read in the person's own zone.
func parse(written string, loc *time.Location) (time.Time, bool) {
	for _, layout := range layouts {
		if at, err := time.ParseInLocation(layout, written, loc); err == nil {
			return at.UTC(), true
		}
	}
	// A model may write the zone in. Taken at its word when it does.
	if at, err := time.Parse(time.RFC3339, written); err == nil {
		return at.UTC(), true
	}
	return time.Time{}, false
}

// describe : Reminders as the model is shown them.
func describe(found []remind.Reminder, clock Clock) string {
	var b strings.Builder
	b.WriteString("Soonest first. Each line is an identifier, when it happens, and what it says.\n")
	for i := range found {
		r := found[i]
		b.WriteString("\n")
		b.WriteString(r.ID)
		b.WriteString("  ")
		b.WriteString(spell(r.DueAt, clock.where()))
		b.WriteString(repeating(r.Repeats))
		if r.Status != remind.Pending {
			b.WriteString("  [" + string(r.Status) + "]")
		}
		b.WriteString("  ")
		b.WriteString(r.Title)
		b.WriteString(": ")
		b.WriteString(r.Body)
	}
	return b.String()
}

// spell : A moment written out in the person's own zone.
func spell(at time.Time, loc *time.Location) string {
	return at.In(loc).Format("3:04 pm on Monday 2 January 2006")
}

// repeating : How often it comes back, as a phrase, or nothing.
func repeating(r remind.Repeat) string {
	if r == remind.Once {
		return ""
	}
	return ", repeating " + string(r)
}

// repeatWords : The repeats a caller may choose.
func repeatWords() []string {
	out := make([]string, 0, len(remind.Repeats()))
	for _, r := range remind.Repeats() {
		out = append(out, string(r))
	}
	return out
}
