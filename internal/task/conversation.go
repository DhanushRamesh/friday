package task

import (
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	// ConversationIDPrefix : Marks an identifier as belonging to a
	// conversation.
	ConversationIDPrefix = "conv_"
	// conversationIDLen : The length of a prefixed conversation identifier.
	conversationIDLen = len(ConversationIDPrefix) + ulid.EncodedSize

	// DefaultHistoryTurns : How many turns of a conversation are sent to a
	// provider by default. Enough for a correction or a follow-up to make
	// sense, without paying for the whole exchange on every request.
	DefaultHistoryTurns = 20
)

// Conversation : One exchange, grouping the tasks that belong to it.
//
// It exists so that a follow-up or a correction can be understood in the light
// of what came before it. Without one, "no, make it four" reaches a model with
// nothing to make four.
type Conversation struct {
	// ID : The identifier, a ConversationIDPrefix followed by a ULID.
	ID string
	// ClientID : Who the conversation belongs to. Empty only for one created
	// before clients existed.
	ClientID string
	// Title : What to call it in a listing. May be empty.
	Title string
	// CreatedAt : When the exchange began.
	CreatedAt time.Time
	// UpdatedAt : When a task was last added to it.
	UpdatedAt time.Time
}

// NewConversation : Creates a conversation belonging to a client.
func NewConversation(clientID, title string) *Conversation {
	started := now()
	return &Conversation{
		ID:        NewConversationID(),
		ClientID:  clientID,
		Title:     strings.TrimSpace(title),
		CreatedAt: started,
		UpdatedAt: started,
	}
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

// Role : Who said something in a conversation.
type Role string

const (
	// RoleUser : The person asking.
	RoleUser Role = "user"
	// RoleAssistant : FRIDAY answering.
	RoleAssistant Role = "assistant"
)

// Turn : One thing said in a conversation.
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
