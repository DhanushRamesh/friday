// Package memory holds what the assistant has been asked to remember.
//
// A conversation keeps what was said in it and is condensed as it grows.
// A memory outlives the conversation it came from and is reachable from any
// other one.
package memory

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// IDPrefix : Marks an identifier as belonging to a memory.
	IDPrefix = "mem_"
	// idLen : The length of a prefixed memory identifier.
	idLen = len(IDPrefix) + ulid.EncodedSize
)

// MaxSubject : The longest a subject line may be.
const MaxSubject = 160

// MaxBody : The longest a memory may be.
//
// A memory is a fact, not a document. Anything longer is a conversation that
// wanted condensing.
const MaxBody = 4000

// Tier : How a memory reaches the model.
type Tier string

const (
	// TierAlways : In every system prompt. Small, few, and capped.
	TierAlways Tier = "always"
	// TierRecall : Found by searching when it relates to what was asked.
	TierRecall Tier = "recall"
)

// Valid : Whether the tier is one the code knows.
func (t Tier) Valid() bool { return t == TierAlways || t == TierRecall }

// Tiers : Every tier, for a caller that has to offer a choice.
func Tiers() []Tier { return []Tier{TierAlways, TierRecall} }

// Memory : One thing worth keeping.
type Memory struct {
	// ID : The identifier, an IDPrefix followed by a ULID.
	ID string
	// UserID : Whose memory it is.
	UserID string
	// Tier : How it reaches the model.
	Tier Tier
	// Subject : One line saying what this is about, used to judge whether it
	// is worth reading.
	Subject string
	// Body : The memory itself.
	Body string
	// EmbedModel : What produced Embedding. Empty when it has none.
	EmbedModel string
	// Embedding : The vector for Text, or nil when it has not been embedded.
	Embedding []float32
	// CreatedAt : When it was first stored.
	CreatedAt time.Time
	// UpdatedAt : When it was last changed.
	UpdatedAt time.Time
	// LastUsedAt : When it was last given to the model, or nil if never.
	LastUsedAt *time.Time
	// Uses : How many times it has been given to the model.
	Uses int
}

// Text : What gets embedded and what the model is shown.
//
// Subject and body together, because a question resembles a description of a
// fact more often than the fact as it was written down.
func (m *Memory) Text() string {
	subject := strings.TrimSpace(m.Subject)
	body := strings.TrimSpace(m.Body)
	switch {
	case subject == "":
		return body
	case body == "":
		return subject
	default:
		return subject + ": " + body
	}
}

// Embedded : Whether it carries a vector from the named model.
func (m *Memory) Embedded(model string) bool {
	return model != "" && m.EmbedModel == model && len(m.Embedding) > 0
}

// Errors a memory can be refused for.
var (
	// ErrNoSubject : A memory with nothing to say what it is about.
	ErrNoSubject = errors.New("memory: a memory needs a subject")
	// ErrNoBody : A memory with nothing in it.
	ErrNoBody = errors.New("memory: a memory needs something to remember")
	// ErrSubjectTooLong : A subject that is a paragraph.
	ErrSubjectTooLong = errors.New("memory: the subject is too long")
	// ErrBodyTooLong : A memory that is a document.
	ErrBodyTooLong = errors.New("memory: the memory is too long")
	// ErrBadTier : A tier the code does not know.
	ErrBadTier = errors.New("memory: unknown tier")
	// ErrNoUser : A memory belonging to nobody.
	ErrNoUser = errors.New("memory: a memory needs an owner")
	// ErrNotFound : No such memory.
	ErrNotFound = errors.New("memory: no such memory")
)

// Valid : Whether the memory can be stored.
func (m *Memory) Valid() error {
	switch {
	case strings.TrimSpace(m.UserID) == "":
		return ErrNoUser
	case !m.Tier.Valid():
		return ErrBadTier
	case strings.TrimSpace(m.Subject) == "":
		return ErrNoSubject
	case strings.TrimSpace(m.Body) == "":
		return ErrNoBody
	case utf8.RuneCountInString(m.Subject) > MaxSubject:
		return ErrSubjectTooLong
	case utf8.RuneCountInString(m.Body) > MaxBody:
		return ErrBodyTooLong
	}
	return nil
}

// NewID : A fresh memory identifier.
func NewID() string { return IDPrefix + ulid.Make().String() }

// ValidID : Whether id is shaped like a memory identifier. It checks the
// form only; no such memory need exist.
func ValidID(id string) bool {
	if len(id) != idLen || !strings.HasPrefix(id, IDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, IDPrefix))
	return err == nil
}

// New : A memory ready to be stored, or why it cannot be.
func New(userID string, tier Tier, subject, body string) (*Memory, error) {
	at := now()
	m := &Memory{
		ID:        NewID(),
		UserID:    strings.TrimSpace(userID),
		Tier:      tier,
		Subject:   strings.TrimSpace(subject),
		Body:      strings.TrimSpace(body),
		CreatedAt: at,
		UpdatedAt: at,
	}
	if err := m.Valid(); err != nil {
		return nil, err
	}
	return m, nil
}

// now : The clock, replaceable in tests.
var now = func() time.Time { return time.Now().UTC() }
