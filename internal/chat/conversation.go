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
	// ConversationIDPrefix : Marks an identifier as belonging to a
	// conversation.
	ConversationIDPrefix = "conv_"
	// conversationIDLen : The length of a prefixed conversation identifier.
	conversationIDLen = len(ConversationIDPrefix) + ulid.EncodedSize
)

// Conversation : One exchange, grouping the chats that belong to it.
//
// It exists so that a follow-up or a correction can be understood in the light
// of what came before it. Without one, "no, make it four" reaches a model with
// nothing to make four.
type Conversation struct {
	// ID : The identifier, a ConversationIDPrefix followed by a ULID.
	ID string
	// UserID : Whose conversation it is. Empty only for one created before
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
	// An archived conversation keeps everything said in it and simply stops
	// appearing: it is not offered in a listing, and is never the one a
	// prompt lands in by default.
	ArchivedAt *time.Time
}

// Archived : Whether the conversation has been put away.
func (c *Conversation) Archived() bool { return c.ArchivedAt != nil }

// Archive : Puts the conversation away, keeping everything said in it.
//
// Archiving one already archived is not an error. The caller asked for it to
// be away and it is away; failing would only make a client that lost a
// response have to tell the difference.
func (c *Conversation) Archive() {
	if c.ArchivedAt == nil {
		at := now()
		c.ArchivedAt = &at
	}
}

// Unarchive : Brings the conversation back into the listing.
func (c *Conversation) Unarchive() { c.ArchivedAt = nil }

// NewConversation : Creates a conversation belonging to a user.
//
// It belongs to the person rather than to the client they happened to be
// using, so an exchange begun on a phone can be continued at a desk.
func NewConversation(userID, title string) *Conversation {
	started := now()
	return &Conversation{
		ID:        NewConversationID(),
		UserID:    userID,
		Title:     strings.TrimSpace(title),
		CreatedAt: started,
		UpdatedAt: started,
	}
}

// MaxTitleLen : The longest a conversation title may be.
//
// Matches the column, so a title that is accepted here is one that can be
// stored. Counted in runes rather than bytes: a name in Tamil should be
// allowed the same number of characters as one in English, and MySQL counts
// a VARCHAR in characters too.
const MaxTitleLen = 200

// ErrTitleTooLong : Returned when a title will not fit.
var ErrTitleTooLong = errors.New("chat: the title is too long")

// Rename : Changes what the conversation is called.
//
// An empty title is allowed and means the conversation goes back to having none,
// which a listing shows as untitled. That is a real thing to want: a name
// given by mistake should be removable without deleting the conversation.
func (c *Conversation) Rename(title string) error {
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > MaxTitleLen {
		return ErrTitleTooLong
	}
	c.Title = title
	return nil
}

// NewConversationID : Returns a fresh conversation identifier.
func NewConversationID() string { return ConversationIDPrefix + ulid.Make().String() }

// ValidConversationID : Reports whether id is shaped like a conversation
// identifier. It checks the form only; no such conversation need exist.
func ValidConversationID(id string) bool {
	if len(id) != conversationIDLen || !strings.HasPrefix(id, ConversationIDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, ConversationIDPrefix))
	return err == nil
}

// EnsureConversation : Returns a conversation for userID to talk in, reusing their most
// recent one and creating one only when there is none.
//
// A person always has somewhere to talk: logging in from a second client, or
// finding the active conversation gone, must not start a fresh thread and lose the
// history. Reuse rather than creation is therefore the default, and a new
// conversation is something the user asks for explicitly.
func EnsureConversation(ctx context.Context, repo Repository, userID string) (string, error) {
	existing, err := repo.ListConversations(ctx, userID, 1)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return existing[0].ID, nil
	}

	conversation := NewConversation(userID, "")
	if err := repo.CreateConversation(ctx, conversation); err != nil {
		return "", err
	}
	return conversation.ID, nil
}

// ActiveConversation : Returns the conversation a client should talk in.
//
// active is the conversation that client last used, which may be empty because it
// has just logged in, or may name a conversation that has since been removed. In
// either case the user's most recent conversation is taken up and remembered, so
// that a client never finds itself without somewhere to talk.
func ActiveConversation(ctx context.Context, repo Repository, userID, clientID, active string) (string, error) {
	if active != "" {
		return active, nil
	}

	id, err := EnsureConversation(ctx, repo, userID)
	if err != nil {
		return "", err
	}
	if err := repo.SetActiveConversation(ctx, userID, clientID, id); err != nil {
		return "", err
	}
	return id, nil
}
