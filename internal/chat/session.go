package chat

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// SessionIDPrefix : Marks an identifier as belonging to a
	// session.
	SessionIDPrefix = "sess_"
	// sessionIDLen : The length of a prefixed session identifier.
	sessionIDLen = len(SessionIDPrefix) + ulid.EncodedSize
)

// Session : One exchange, grouping the chats that belong to it.
//
// It exists so that a follow-up or a correction can be understood in the light
// of what came before it. Without one, "no, make it four" reaches a model with
// nothing to make four.
type Session struct {
	// ID : The identifier, a SessionIDPrefix followed by a ULID.
	ID string
	// UserID : Whose session it is. Empty only for one created before
	// users existed, which is therefore unreachable.
	UserID string
	// Title : What to call it in a listing. May be empty.
	Title string
	// CreatedAt : When the exchange began.
	CreatedAt time.Time
	// UpdatedAt : When a chat was last added to it.
	UpdatedAt time.Time
	// ArchivedAt : When it was put away, or nil while it is in use.
	//
	// An archived session keeps everything said in it and simply stops
	// appearing: it is not offered in a listing, and is never the one a
	// prompt lands in by default.
	ArchivedAt *time.Time
}

// Archived : Whether the session has been put away.
func (c *Session) Archived() bool { return c.ArchivedAt != nil }

// Archive : Puts the session away, keeping everything said in it.
//
// Archiving one already archived is not an error. The caller asked for it to
// be away and it is away; failing would only make a client that lost a
// response have to tell the difference.
func (c *Session) Archive() {
	if c.ArchivedAt == nil {
		at := now()
		c.ArchivedAt = &at
	}
}

// Unarchive : Brings the session back into the listing.
func (c *Session) Unarchive() { c.ArchivedAt = nil }

// NewSession : Creates a session belonging to a user.
//
// It belongs to the person rather than to the client they happened to be
// using, so an exchange begun on a phone can be continued at a desk.
func NewSession(userID, title string) *Session {
	started := now()
	return &Session{
		ID:        NewSessionID(),
		UserID:    userID,
		Title:     strings.TrimSpace(title),
		CreatedAt: started,
		UpdatedAt: started,
	}
}

// MaxTitleLen : The longest a session title may be.
//
// Matches the column, so a title that is accepted here is one that can be
// stored. Counted in runes rather than bytes: a name in Tamil should be
// allowed the same number of characters as one in English, and MySQL counts
// a VARCHAR in characters too.
const MaxTitleLen = 200

// ErrTitleTooLong : Returned when a title will not fit.
var ErrTitleTooLong = errors.New("chat: the title is too long")

// Rename : Changes what the session is called.
//
// An empty title is allowed and means the session goes back to having none,
// which a listing shows as untitled. That is a real thing to want: a name
// given by mistake should be removable without deleting the conversation.
func (c *Session) Rename(title string) error {
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > MaxTitleLen {
		return ErrTitleTooLong
	}
	c.Title = title
	return nil
}

// NewSessionID : Returns a fresh session identifier.
func NewSessionID() string { return SessionIDPrefix + ulid.Make().String() }

// ValidSessionID : Reports whether id is shaped like a session
// identifier. It checks the form only; no such session need exist.
func ValidSessionID(id string) bool {
	if len(id) != sessionIDLen || !strings.HasPrefix(id, SessionIDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, SessionIDPrefix))
	return err == nil
}

// EnsureSession : Returns a session for userID to talk in, reusing their most
// recent one and creating one only when there is none.
//
// A person always has somewhere to talk: logging in from a second client, or
// finding the active session gone, must not start a fresh thread and lose the
// history. Reuse rather than creation is therefore the default, and a new
// session is something the user asks for explicitly.
func EnsureSession(ctx context.Context, repo Repository, userID string) (string, error) {
	existing, err := repo.ListSessions(ctx, userID, 1)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return existing[0].ID, nil
	}

	session := NewSession(userID, "")
	if err := repo.CreateSession(ctx, session); err != nil {
		return "", err
	}
	return session.ID, nil
}

// ActiveSession : Returns the session a client should talk in.
//
// active is the session that client last used, which may be empty because it
// has just logged in, or may name a session that has since been removed. In
// either case the user's most recent session is taken up and remembered, so
// that a client never finds itself without somewhere to talk.
func ActiveSession(ctx context.Context, repo Repository, userID, clientID, active string) (string, error) {
	if active != "" {
		return active, nil
	}

	id, err := EnsureSession(ctx, repo, userID)
	if err != nil {
		return "", err
	}
	if err := repo.SetActiveSession(ctx, userID, clientID, id); err != nil {
		return "", err
	}
	return id, nil
}
