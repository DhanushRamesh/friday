package remind

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
)

// Speaker : Somewhere a reminder can be said.
type Speaker interface {
	// Say : Delivers the reminder, or reports why it could not.
	//
	// Reporting failure matters. A reminder nobody heard must stay pending
	// so it can be tried again, and be counted as missed rather than said.
	Say(ctx context.Context, r Reminder) error
}

// ErrNowhereToSay : Returned when nothing is configured to deliver a
// reminder. Not a fault, and not a delivery either.
var ErrNowhereToSay = errors.New("remind: nowhere to say it")

// Aloud : A Speaker that talks through a voice satellite.
type Aloud struct {
	// Announcer : Where it is said. Nil says nowhere.
	Announcer announce.Announcer
}

// Say : Speaks the reminder aloud.
func (a Aloud) Say(ctx context.Context, r Reminder) error {
	if a.Announcer == nil || !a.Announcer.Available() {
		return ErrNowhereToSay
	}
	return a.Announcer.Say(ctx, Spoken(r))
}

// Nowhere : A Speaker with nothing behind it, for a server that cannot
// speak. It refuses rather than reporting success, so a reminder nobody
// could hear is not recorded as delivered.
type Nowhere struct{}

// Say : Always fails with ErrNowhereToSay.
func (Nowhere) Say(context.Context, Reminder) error { return ErrNowhereToSay }

// Everywhere : A Speaker that tries several in turn.
//
// One delivery is enough. It fails only when every one of them did, so a
// browser that is closed does not stop the satellite saying it.
type Everywhere struct {
	// To : Where to try, in order.
	To []Speaker
	// Logger : Where a failure that did not matter goes. Optional.
	Logger *slog.Logger
}

// Say : Delivers to the first place that will take it.
func (e Everywhere) Say(ctx context.Context, r Reminder) error {
	if len(e.To) == 0 {
		return ErrNowhereToSay
	}

	var failed []error
	for _, to := range e.To {
		err := to.Say(ctx, r)
		if err == nil {
			return nil
		}
		failed = append(failed, err)
		if e.Logger != nil && !errors.Is(err, ErrNowhereToSay) {
			e.Logger.WarnContext(ctx, "a reminder could not be delivered there",
				slog.String("reminder_id", r.ID), slog.Any("error", err))
		}
	}
	return errors.Join(failed...)
}

// Spoken : What a reminder sounds like.
//
// The body alone, when it already reads as something said. A title is for
// a listing and saying it as well would have the assistant announce
// "Wake: time to get up".
func Spoken(r Reminder) string {
	body := strings.TrimSpace(r.Body)
	if body == "" {
		return strings.TrimSpace(r.Title)
	}
	return body
}
