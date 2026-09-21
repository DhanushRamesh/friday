package task

import (
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	// SessionIDPrefix : Marks an identifier as belonging to a
	// session.
	SessionIDPrefix = "sess_"
	// sessionIDLen : The length of a prefixed session identifier.
	sessionIDLen = len(SessionIDPrefix) + ulid.EncodedSize

	// DefaultHistoryTurns : How many turns of a session are sent to a
	// provider by default. Enough for a correction or a follow-up to make
	// sense, without paying for the whole exchange on every request.
	DefaultHistoryTurns = 20
)

// Session : One exchange, grouping the tasks that belong to it.
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
	// UpdatedAt : When a task was last added to it.
	UpdatedAt time.Time
}

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

// Role : Who said something in a session.
type Role string

const (
	// RoleUser : The person asking.
	RoleUser Role = "user"
	// RoleAssistant : FRIDAY answering.
	RoleAssistant Role = "assistant"
)

// Turn : One thing said in a session.
type Turn struct {
	Role Role
	Text string
}

// MergeTurns : Joins consecutive turns by the same speaker into one.
//
// A task that was cancelled or failed contributes a prompt with no answer, so
// two questions can end up adjacent. Models that require the roles to
// alternate reject that, and joining them also reads correctly: a question
// followed by its correction becomes one request.
func MergeTurns(turns []Turn) []Turn {
	merged := make([]Turn, 0, len(turns))
	for _, turn := range turns {
		if turn.Text == "" {
			continue
		}
		if n := len(merged); n > 0 && merged[n-1].Role == turn.Role {
			merged[n-1].Text += "\n\n" + turn.Text
			continue
		}
		merged = append(merged, turn)
	}
	return merged
}
