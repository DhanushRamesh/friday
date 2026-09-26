package reminders_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
)

const (
	user   = "usr_01M3D477HXQ4YNQX7BNXJZZCV0"
	client = "cli_01M3D477HXQ4YNQX7BNXJZZCV0"
)

// india : Where the person is, for these.
var india = time.FixedZone("IST", 5*3600+1800)

// noon : The moment the clock is held at.
var noon = time.Date(2026, 9, 26, 12, 0, 0, 0, india)

// harness : A registry over an empty store, with the clock held still.
func harness(t *testing.T) (*tool.Registry, *inmemory.Store) {
	t.Helper()

	store := inmemory.New()
	clock := reminders.Clock{Now: func() time.Time { return noon }, Location: india}

	r, err := tool.NewRegistry(reminders.All(store, clock)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r, store
}

// call : Calls a tool and returns its result.
func call(t *testing.T, r *tool.Registry, name, args string) tool.Result {
	t.Helper()
	return r.Call(context.Background(), name, tool.Invocation{
		Caller: tool.Caller{UserID: user, ClientID: client, Channel: chat.ChannelVoice},
		Args:   json.RawMessage(args),
	})
}

// Every reminder tool has to survive registration, which is where a
// missing description or an untyped argument is caught.
func TestEveryReminderToolRegisters(t *testing.T) {
	if _, err := tool.NewRegistry(reminders.All(nil, reminders.Clock{})...); err != nil {
		t.Fatalf("a reminder tool is not usable: %v", err)
	}
}

// Nothing here destroys anything, so all of it may be said out loud.
func TestVoiceMayUseAllOfThem(t *testing.T) {
	r, _ := harness(t)

	if got := len(r.For(chat.ChannelVoice)); got != 3 {
		t.Errorf("voice is offered %d of the 3 reminder tools", got)
	}
}

// A length of time is counted by the server, not by the model.
func TestMinutesAreCountedHere(t *testing.T) {
	r, store := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Timer","say":"Your twenty minute timer has finished.","minutes_from_now":20}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	all, _ := store.List(context.Background(), user, remind.Pending)
	if len(all) != 1 {
		t.Fatalf("stored %d, want 1", len(all))
	}
	if want := noon.Add(20 * time.Minute); !all[0].DueAt.Equal(want) {
		t.Errorf("due = %v, want %v", all[0].DueAt.In(india), want)
	}
}

// A written time is read in the person's own zone, not the server's.
func TestAWrittenTimeIsLocal(t *testing.T) {
	r, store := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Call","say":"Time to call the roofer.","at":"2026-09-26 16:30"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	all, _ := store.List(context.Background(), user, remind.Pending)
	if got := all[0].DueAt.In(india); got.Hour() != 16 || got.Minute() != 30 {
		t.Errorf("due at %v, want half past four in India", got)
	}
}

// Several spellings of the same moment are accepted, because a model
// writes it several ways and refusing it would refuse the reminder.
func TestSeveralSpellingsOfATimeWork(t *testing.T) {
	for _, written := range []string{
		"2026-09-26 16:30", "2026-09-26T16:30", "2026-09-26 16:30:00",
		"2026-09-26T16:30:00", "2026-09-26T16:30:00+05:30",
	} {
		r, store := harness(t)
		got := call(t, r, "reminder_set", `{"title":"Call","say":"Call them.","at":"`+written+`"}`)
		if got.Outcome != conversation.OutcomeOK {
			t.Errorf("%q was refused: %s", written, got.Content)
			continue
		}
		all, _ := store.List(context.Background(), user, remind.Pending)
		if at := all[0].DueAt.In(india); at.Hour() != 16 || at.Minute() != 30 {
			t.Errorf("%q landed at %v", written, at)
		}
	}
}

// Both ways of saying when is ambiguous, and asking is better than
// picking one.
func TestBothWaysOfSayingWhenIsRefused(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Timer","say":"Up.","minutes_from_now":20,"at":"2026-09-26 16:30"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want it refused", got.Outcome)
	}
	if !strings.Contains(got.Content, "Which was meant") {
		t.Errorf("content = %q, want it to ask", got.Content)
	}
}

// Neither way is refused too, rather than defaulting to some moment.
func TestNoTimeAtAllIsRefused(t *testing.T) {
	r, _ := harness(t)

	if got := call(t, r, "reminder_set", `{"title":"Timer","say":"Up."}`); got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want it refused", got.Outcome)
	}
}

// A time already gone is refused, and told the current time so the next
// attempt can be right.
func TestATimeInThePastIsRefused(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_set", `{"title":"Call","say":"Call them.","at":"2026-09-26 09:00"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("outcome = %s, want it refused", got.Outcome)
	}
	if !strings.Contains(got.Content, "2026-09-26 12:00") {
		t.Errorf("content = %q, want it to say what time it is now", got.Content)
	}
}

// A moment just gone is taken as now. The model works the time out from
// what it was told, and a second or two passes while it does.
func TestAMomentJustGoneIsAccepted(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_set", `{"title":"Now","say":"Now.","at":"2026-09-26 11:59"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Errorf("a minute ago was refused: %s", got.Content)
	}
}

// A repeat is kept, so it comes back.
func TestARepeatIsKept(t *testing.T) {
	r, store := harness(t)

	call(t, r, "reminder_set",
		`{"title":"Wake","say":"It is seven o'clock.","at":"2026-09-28 07:00","repeats":"weekdays"}`)

	all, _ := store.List(context.Background(), user, remind.Pending)
	if len(all) != 1 || all[0].Repeats != remind.Weekdays {
		t.Errorf("repeats = %q, want weekdays", all[0].Repeats)
	}
}

// A repeat the code does not know is refused rather than silently
// becoming a one-shot.
func TestAnUnknownRepeatIsRefused(t *testing.T) {
	r, store := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Wake","say":"Up.","minutes_from_now":20,"repeats":"hourly"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want it refused", got.Outcome)
	}
	if all, _ := store.List(context.Background(), user); len(all) != 0 {
		t.Error("an unknown repeat was stored as something else")
	}
}

// The default follows the person rather than the device they happened to
// use.
func TestTheDefaultScopeFollowsThePerson(t *testing.T) {
	r, store := harness(t)

	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","minutes_from_now":20}`)

	all, _ := store.List(context.Background(), user, remind.Pending)
	if all[0].Scope != remind.ScopeUser {
		t.Errorf("scope = %q, want user", all[0].Scope)
	}
}

// Asked for, a reminder can belong to the device it was set on.
func TestItCanBeTiedToTheDevice(t *testing.T) {
	r, store := harness(t)

	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","minutes_from_now":20,"scope":"client"}`)

	all, _ := store.List(context.Background(), user, remind.Pending)
	if all[0].Scope != remind.ScopeClient || all[0].ClientID != client {
		t.Errorf("scope = %q, client = %q", all[0].Scope, all[0].ClientID)
	}
}

// A listing says when, in the person's own words, and carries the
// identifier a cancel needs.
func TestTheListingIsUsable(t *testing.T) {
	r, _ := harness(t)
	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","minutes_from_now":20}`)

	got := call(t, r, "reminder_list", `{}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"rem_", "12:20 pm", "Timer"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("listing is missing %q:\n%s", want, got.Content)
		}
	}
}

// Nothing waiting says so, rather than returning an empty listing.
func TestAnEmptyListingSaysSo(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_list", `{}`)
	if !strings.Contains(got.Content, "nothing waiting") {
		t.Errorf("content = %q", got.Content)
	}
}

// Cancelling stops it happening.
func TestCancellingStopsIt(t *testing.T) {
	r, store := harness(t)
	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","minutes_from_now":20}`)
	all, _ := store.List(context.Background(), user, remind.Pending)

	got := call(t, r, "reminder_cancel", `{"id":"`+all[0].ID+`"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	left, _ := store.List(context.Background(), user, remind.Pending)
	if len(left) != 0 {
		t.Error("it is still waiting to happen")
	}
}

// An identifier that does not exist is refused with advice, not a guess.
func TestCancellingSomethingThatIsNotThere(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_cancel", `{"id":"rem_01M3D477HXQ4YNQX7BNXJZZCV0"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("outcome = %s, want a failure", got.Outcome)
	}
	if !strings.Contains(got.Content, "List them") {
		t.Errorf("content does not say what to do instead: %s", got.Content)
	}
}

// One person's reminder is not reachable by another.
func TestAnotherPersonCannotCancelIt(t *testing.T) {
	r, store := harness(t)
	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","minutes_from_now":20}`)
	all, _ := store.List(context.Background(), user, remind.Pending)

	got := r.Call(context.Background(), "reminder_cancel", tool.Invocation{
		Caller: tool.Caller{UserID: "usr_01M3D477HXQ4YNQX7BNXJZZCV1", Channel: chat.ChannelDirect},
		Args:   json.RawMessage(`{"id":"` + all[0].ID + `"}`),
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want a failure", got.Outcome)
	}
}

// A request from nobody stores nothing.
func TestARequestFromNobodyStoresNothing(t *testing.T) {
	r, store := harness(t)

	got := r.Call(context.Background(), "reminder_set", tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelDirect},
		Args:   json.RawMessage(`{"title":"Timer","say":"Up.","minutes_from_now":20}`),
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want a failure", got.Outcome)
	}
	if all, _ := store.List(context.Background(), user); len(all) != 0 {
		t.Error("something was stored for nobody")
	}
}
