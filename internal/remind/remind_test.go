package remind_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// What a reminder needs before it can be stored.
func TestWhatAReminderNeeds(t *testing.T) {
	due := time.Now().Add(time.Hour)

	for name, tc := range map[string]struct {
		user, client, title, body string
		scope                     remind.Scope
		due                       time.Time
		repeats                   remind.Repeat
		want                      error
	}{
		"no owner":     {"", "cli_1", "Timer", "it is up", remind.ScopeClient, due, remind.Once, remind.ErrNoUser},
		"no client":    {"usr_1", "", "Timer", "it is up", remind.ScopeClient, due, remind.Once, remind.ErrNoClient},
		"no name":      {"usr_1", "", " ", "it is up", remind.ScopeUser, due, remind.Once, remind.ErrNoTitle},
		"nothing said": {"usr_1", "", "Timer", " ", remind.ScopeUser, due, remind.Once, remind.ErrNoBody},
		"no time":      {"usr_1", "", "Timer", "it is up", remind.ScopeUser, time.Time{}, remind.Once, remind.ErrNoTime},
		"bad scope":    {"usr_1", "", "Timer", "it is up", remind.Scope("somewhere"), due, remind.Once, remind.ErrBadScope},
		"bad repeat":   {"usr_1", "", "Timer", "it is up", remind.ScopeUser, due, remind.Repeat("hourly"), remind.ErrBadRepeat},
		"long name":    {"usr_1", "", strings.Repeat("a", remind.MaxTitle+1), "x", remind.ScopeUser, due, remind.Once, remind.ErrTitleTooLong},
		"long body":    {"usr_1", "", "Timer", strings.Repeat("a", remind.MaxBody+1), remind.ScopeUser, due, remind.Once, remind.ErrBodyTooLong},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := remind.New(tc.user, tc.client, tc.scope, tc.title, tc.body, tc.due, tc.repeats)
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// A good one is pending, identified, and due in UTC whatever it was given.
func TestAGoodReminderIsReady(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)
	due := time.Date(2026, 9, 27, 7, 0, 0, 0, india)

	r, err := remind.New("usr_1", "", remind.ScopeUser, " Wake ", " time to get up ", due, remind.Daily)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !remind.ValidID(r.ID) {
		t.Errorf("ID = %q, want a reminder identifier", r.ID)
	}
	if r.Status != remind.Pending {
		t.Errorf("status = %q, want pending", r.Status)
	}
	if r.Title != "Wake" || r.Body != "time to get up" {
		t.Errorf("stored %q / %q, want them trimmed", r.Title, r.Body)
	}
	if r.DueAt.Location() != time.UTC {
		t.Errorf("due in %v, want it stored as UTC", r.DueAt.Location())
	}
	if !r.DueAt.Equal(due) {
		t.Errorf("due = %v, want the same moment as %v", r.DueAt, due)
	}
}

// A client is only kept for a reminder that belongs to one, so a
// user-scoped reminder cannot quietly carry a client it does not use.
func TestAUserReminderKeepsNoClient(t *testing.T) {
	r, err := remind.New("usr_1", "cli_1", remind.ScopeUser, "Wake", "get up", time.Now().Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.ClientID != "" {
		t.Errorf("client = %q, want none on a user-scoped reminder", r.ClientID)
	}
}

// The words the code knows, and nothing else.
func TestTheWordsItKnows(t *testing.T) {
	for _, s := range []remind.Scope{remind.ScopeClient, remind.ScopeUser} {
		if !s.Valid() {
			t.Errorf("%q is not valid", s)
		}
	}
	if remind.Scope("elsewhere").Valid() {
		t.Error("an unknown scope was accepted")
	}

	for _, r := range append(remind.Repeats(), remind.Once) {
		if !r.Valid() {
			t.Errorf("%q is not valid", r)
		}
	}
	if remind.Repeat("fortnightly").Valid() {
		t.Error("an unknown repeat was accepted")
	}

	for _, s := range []remind.Status{remind.Pending, remind.Done, remind.Missed, remind.Cancelled} {
		if !s.Valid() {
			t.Errorf("%q is not valid", s)
		}
	}
	if remind.Status("snoozed").Valid() {
		t.Error("an unknown status was accepted")
	}
}

// An identifier is recognised by its shape, and nothing else is.
func TestOnlyAReminderIdentifierIsValid(t *testing.T) {
	if !remind.ValidID(remind.NewID()) {
		t.Error("a fresh identifier is not valid")
	}
	for _, bad := range []string{"", "rem_", "mem_01M3D477HXQ4YNQX7BNXJZZCV0", "rem_nonsense"} {
		if remind.ValidID(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
}
