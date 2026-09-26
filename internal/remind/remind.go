// Package remind holds things to be said at a time rather than because
// somebody asked.
//
// A reminder only ever says something. It carries words and speaks them: a
// timer that has finished, something to be told at seven. It does not run
// instructions unattended, because a task firing with nobody watching has
// nobody to catch it.
package remind

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// IDPrefix : Marks an identifier as belonging to a reminder.
	IDPrefix = "rem_"
	// idLen : The length of a prefixed reminder identifier.
	idLen = len(IDPrefix) + ulid.EncodedSize
)

const (
	// MaxTitle : The longest a reminder's name may be.
	MaxTitle = 160
	// MaxBody : The longest a reminder may be.
	//
	// It is going to be read aloud, so anything longer is a document being
	// recited at somebody who wanted a sentence.
	MaxBody = 500
)

// Scope : Where a reminder lands when it fires.
type Scope string

const (
	// ScopeClient : At the client that set it. What a timer wants: set on
	// the satellite, rings on the satellite.
	ScopeClient Scope = "client"
	// ScopeUser : Wherever the person is told things. What a reminder
	// wants, since they may not be where they were.
	ScopeUser Scope = "user"
)

// Valid : Whether the scope is one the code knows.
func (s Scope) Valid() bool { return s == ScopeClient || s == ScopeUser }

// Repeat : How often a reminder comes back.
//
// Words rather than a cron expression. Cron is a language, and nobody says
// it aloud.
type Repeat string

const (
	// Once : Not at all. The default.
	Once Repeat = ""
	// Daily : Every day at the same time.
	Daily Repeat = "daily"
	// Weekdays : Monday to Friday.
	Weekdays Repeat = "weekdays"
	// Weekly : The same day each week.
	Weekly Repeat = "weekly"
	// Monthly : The same date each month.
	Monthly Repeat = "monthly"
)

// Valid : Whether the repeat is one the code knows.
func (r Repeat) Valid() bool {
	switch r {
	case Once, Daily, Weekdays, Weekly, Monthly:
		return true
	}
	return false
}

// Repeats : Every repeat a caller may offer, without the empty one.
func Repeats() []Repeat { return []Repeat{Daily, Weekdays, Weekly, Monthly} }

// Status : Where a reminder is in its life.
type Status string

const (
	// Pending : Waiting for its time.
	Pending Status = "pending"
	// Done : It fired and will not come back.
	Done Status = "done"
	// Missed : Its time passed while nothing was listening, too long ago to
	// say now. Kept rather than deleted, so it can be mentioned once.
	Missed Status = "missed"
	// Cancelled : Called off before it fired.
	Cancelled Status = "cancelled"
)

// Valid : Whether the status is one the code knows.
func (s Status) Valid() bool {
	switch s {
	case Pending, Done, Missed, Cancelled:
		return true
	}
	return false
}

// Reminder : Something to be said at a time.
type Reminder struct {
	// ID : The identifier, an IDPrefix followed by a ULID.
	ID string
	// UserID : Whose reminder it is.
	UserID string
	// ClientID : Which client it belongs to. Set for ScopeClient, and empty
	// otherwise.
	ClientID string
	// Scope : Where it lands when it fires.
	Scope Scope
	// Title : What to call it in a listing, and when asking which one.
	Title string
	// Body : What is actually said.
	Body string
	// DueAt : When it is next to be said, in UTC.
	DueAt time.Time
	// Repeats : How often it comes back. Once for a one-shot.
	Repeats Repeat
	// Status : Where it is in its life.
	Status Status
	// CreatedAt : When it was made.
	CreatedAt time.Time
	// UpdatedAt : When it last changed.
	UpdatedAt time.Time
	// LastFiredAt : When it was last said, or nil if never.
	LastFiredAt *time.Time
	// Fires : How many times it has been said.
	Fires int
}

// Errors a reminder can be refused for.
var (
	// ErrNoUser : A reminder belonging to nobody.
	ErrNoUser = errors.New("remind: a reminder needs an owner")
	// ErrNoClient : A client-scoped reminder with no client to fire at.
	ErrNoClient = errors.New("remind: a reminder for one client must say which")
	// ErrNoTitle : A reminder with nothing to call it.
	ErrNoTitle = errors.New("remind: a reminder needs a name")
	// ErrNoBody : A reminder with nothing to say.
	ErrNoBody = errors.New("remind: a reminder needs something to say")
	// ErrTitleTooLong : A name that is a sentence.
	ErrTitleTooLong = errors.New("remind: the name is too long")
	// ErrBodyTooLong : More than anybody wants read aloud.
	ErrBodyTooLong = errors.New("remind: what it says is too long to be read out")
	// ErrNoTime : A reminder due at no particular moment.
	ErrNoTime = errors.New("remind: a reminder needs a time")
	// ErrBadScope : A scope the code does not know.
	ErrBadScope = errors.New("remind: unknown scope")
	// ErrBadRepeat : A repeat the code does not know.
	ErrBadRepeat = errors.New("remind: unknown repeat")
	// ErrBadStatus : A status the code does not know.
	ErrBadStatus = errors.New("remind: unknown status")
	// ErrNotFound : No such reminder.
	ErrNotFound = errors.New("remind: no such reminder")
)

// Valid : Whether the reminder can be stored.
func (r *Reminder) Valid() error {
	switch {
	case strings.TrimSpace(r.UserID) == "":
		return ErrNoUser
	case !r.Scope.Valid():
		return ErrBadScope
	case r.Scope == ScopeClient && strings.TrimSpace(r.ClientID) == "":
		return ErrNoClient
	case strings.TrimSpace(r.Title) == "":
		return ErrNoTitle
	case strings.TrimSpace(r.Body) == "":
		return ErrNoBody
	case utf8.RuneCountInString(r.Title) > MaxTitle:
		return ErrTitleTooLong
	case utf8.RuneCountInString(r.Body) > MaxBody:
		return ErrBodyTooLong
	case r.DueAt.IsZero():
		return ErrNoTime
	case !r.Repeats.Valid():
		return ErrBadRepeat
	case !r.Status.Valid():
		return ErrBadStatus
	}
	return nil
}

// NewID : A fresh reminder identifier.
func NewID() string { return IDPrefix + ulid.Make().String() }

// ValidID : Whether id is shaped like a reminder identifier. It checks the
// form only; no such reminder need exist.
func ValidID(id string) bool {
	if len(id) != idLen || !strings.HasPrefix(id, IDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, IDPrefix))
	return err == nil
}

// New : A reminder ready to be stored, or why it cannot be.
func New(userID, clientID string, scope Scope, title, body string, dueAt time.Time, repeats Repeat) (*Reminder, error) {
	at := now()
	if scope != ScopeClient {
		clientID = ""
	}

	r := &Reminder{
		ID:        NewID(),
		UserID:    strings.TrimSpace(userID),
		ClientID:  strings.TrimSpace(clientID),
		Scope:     scope,
		Title:     strings.TrimSpace(title),
		Body:      strings.TrimSpace(body),
		DueAt:     dueAt.UTC(),
		Repeats:   repeats,
		Status:    Pending,
		CreatedAt: at,
		UpdatedAt: at,
	}
	if err := r.Valid(); err != nil {
		return nil, err
	}
	return r, nil
}

// now : The clock, replaceable in tests.
var now = func() time.Time { return time.Now().UTC() }
