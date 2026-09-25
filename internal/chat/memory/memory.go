// Package memory : Holds chats in memory rather than a database.
//
// It exists so that everything above the repository can be exercised without
// MySQL: the runner's execution logic, the API's handlers, and anything else
// that only needs somewhere to put a chat. It is a real implementation of
// chat.Repository, not a stub, and behaves as the MySQL one does wherever the
// difference would let a bug through — listings come back newest first, and
// ownership is enforced between users.
//
// It is not durable and is not meant to be.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Repository : An in-memory chat.Repository.
//
// The zero value is unusable; call New.
type Repository struct {
	mu    sync.Mutex
	chats map[string]*chat.Chat

	// said : Each conversation's conversation, in the order it was said.
	said          map[string][]conversation.Message
	appendSaidErr error
	// summaries : Each conversation's condensed earlier conversation.
	summaries     map[string]conversation.Summary
	conversations map[string]chat.Conversation
	clients       map[string]chat.Client
	users         map[string]chat.User

	// Fault injection, for exercising the paths a caller takes when storage
	// misbehaves. A real repository fails; one that never does lets those
	// paths go untested.
	//
	// updateErr : When set, every Update fails with it.
	updateErr error
	// appendErr : When set, every AppendMessage fails with it.
	appendErr error
	// failRunningReason : The reason passed to the last FailRunning call.
	failRunningReason string
}

// New : Returns an empty repository.
func New() *Repository {
	return &Repository{
		chats:         map[string]*chat.Chat{},
		said:          map[string][]conversation.Message{},
		summaries:     map[string]conversation.Summary{},
		conversations: map[string]chat.Conversation{},
		clients:       map[string]chat.Client{},
		users:         map[string]chat.User{},
	}
}

// copyChat : Returns a copy, so callers cannot mutate stored state by holding
// a pointer, as a real repository's callers cannot.
func copyChat(t *chat.Chat) *chat.Chat {
	c := *t
	return &c
}

// Create : Stores a new chat.
func (m *Repository) Create(_ context.Context, t *chat.Chat) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chats[t.ID] = copyChat(t)
	return nil
}

// Get : Returns a stored chat.
func (m *Repository) Get(_ context.Context, id string) (*chat.Chat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.chats[id]
	if !ok {
		return nil, chat.ErrNotFound
	}
	return copyChat(t), nil
}

// Update : Overwrites a stored chat.
func (m *Repository) Update(_ context.Context, t *chat.Chat) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.chats[t.ID]; !ok {
		return chat.ErrNotFound
	}
	m.chats[t.ID] = copyChat(t)
	return nil
}

// List : Returns stored chats, unordered, which is enough for these tests.
func (m *Repository) List(_ context.Context, f chat.Filter) ([]chat.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []chat.Summary
	for _, t := range m.chats {
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		if f.ConversationID != "" && t.ConversationID != f.ConversationID {
			continue
		}
		if f.UserID != "" {
			owner, ok := m.conversations[t.ConversationID]
			if !ok || owner.UserID != f.UserID {
				continue
			}
		}
		out = append(out, chat.Summary{
			ID: t.ID, ConversationID: t.ConversationID, Prompt: t.Prompt,
			Channel: t.Channel, Status: t.Status, Error: t.Error,
			ErrorCode: t.ErrorCode,
		})
	}
	// Newest first, as the real repository returns them. A fake that returns
	// an arbitrary order lets ordering bugs through.
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// FailRunning : Marks running chats as failed.
func (m *Repository) FailRunning(_ context.Context, reason string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failRunningReason = reason
	var changed int64
	for id, t := range m.chats {
		if t.Status == chat.StatusRunning {
			_ = t.Fail(reason)
			m.chats[id] = t
			changed++
		}
	}
	return changed, nil
}

var _ chat.Repository = (*Repository)(nil)

// CreateConversation : Stores a new conversation.
func (m *Repository) CreateConversation(_ context.Context, c *chat.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conversations == nil {
		m.conversations = map[string]chat.Conversation{}
	}
	m.conversations[c.ID] = *c
	return nil
}

// GetConversation : Returns a stored conversation.
func (m *Repository) GetConversation(_ context.Context, id string) (*chat.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[id]
	if !ok {
		return nil, chat.ErrNotFound
	}
	return &c, nil
}

// ListConversations : Returns stored conversations.
func (m *Repository) ListConversations(ctx context.Context, userID string, limit int) ([]chat.Conversation, error) {
	return m.listConversations(ctx, userID, limit, false)
}

// ListArchivedConversations : Returns stored conversations that have been put away.
func (m *Repository) ListArchivedConversations(ctx context.Context, userID string, limit int) ([]chat.Conversation, error) {
	return m.listConversations(ctx, userID, limit, true)
}

func (m *Repository) listConversations(_ context.Context, userID string, limit int, archived bool) ([]chat.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []chat.Conversation
	for _, c := range m.conversations {
		if c.UserID != userID || c.Archived() != archived {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SetConversationArchived : Puts a stored conversation away or brings it back.
func (m *Repository) SetConversationArchived(_ context.Context, userID, conversationID string, archived bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return chat.ErrNotFound
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}
	if archived {
		conversation.Archive()
		m.clearActive(conversationID)
	} else {
		conversation.Unarchive()
	}
	m.conversations[conversationID] = conversation
	return nil
}

// DeleteConversation : Removes a stored conversation and everything said in it.
func (m *Repository) DeleteConversation(_ context.Context, userID, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return chat.ErrNotFound
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}

	m.clearActive(conversationID)
	delete(m.conversations, conversationID)
	// Standing in for the database's cascades, so a test sees what a real
	// delete leaves behind rather than a conversation's chats outliving it.
	for id, t := range m.chats {
		if t.ConversationID == conversationID {
			delete(m.chats, id)
		}
	}
	delete(m.said, conversationID)
	return nil
}

// clearActive : Unpoints every client using a conversation. The caller holds mu.
func (m *Repository) clearActive(conversationID string) {
	for id, d := range m.clients {
		if d.ActiveConversationID == conversationID {
			d.ActiveConversationID = ""
			m.clients[id] = d
		}
	}
}

// CreateUser : Stores a new user.
func (m *Repository) CreateUser(_ context.Context, u *chat.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.users {
		if existing.Username == u.Username {
			return chat.ErrUsernameTaken
		}
	}
	m.users[u.ID] = *u
	return nil
}

// GetUser : Returns a stored user.
func (m *Repository) GetUser(_ context.Context, id string) (*chat.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, chat.ErrNotFound
	}
	return &u, nil
}

// UserByUsername : Returns the user with the given username.
func (m *Repository) UserByUsername(_ context.Context, username string) (*chat.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := chat.NormaliseUsername(username)
	for _, u := range m.users {
		if u.Username == want {
			found := u
			return &found, nil
		}
	}
	return nil, chat.ErrNotFound
}

// CreateClient : Stores a new client.
func (m *Repository) CreateClient(_ context.Context, d *chat.Client) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[d.ID] = *d
	return nil
}

// ClientByTokenHash : Returns the client authenticating with a token hash.
func (m *Repository) ClientByTokenHash(_ context.Context, tokenHash string) (*chat.Client, error) {
	if tokenHash == "" {
		return nil, chat.ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.clients {
		if d.TokenHash == tokenHash {
			if d.Revoked() {
				return nil, chat.ErrRevoked
			}
			found := d
			return &found, nil
		}
	}
	return nil, chat.ErrNotFound
}

// ListClients : Returns a user's clients, newest first.
func (m *Repository) ListClients(_ context.Context, userID string, revoked bool) ([]chat.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []chat.Client
	for _, d := range m.clients {
		if d.UserID == userID && d.Revoked() == revoked {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// ReissueClientToken : Replaces a stored client's token.
func (m *Repository) ReissueClientToken(_ context.Context, userID, clientID, tokenHash string) (*chat.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, ok := m.clients[clientID]
	if !ok {
		return nil, chat.ErrNotFound
	}
	if d.UserID != userID {
		return nil, chat.ErrNotOwned
	}
	if d.Revoked() {
		return nil, chat.ErrNotFound
	}
	d.TokenHash = tokenHash
	m.clients[clientID] = d
	out := d
	return &out, nil
}

// SetClientChannel : Changes how a stored client's prompts are treated.
func (m *Repository) SetClientChannel(_ context.Context, userID, clientID string, channel chat.Channel) error {
	if !channel.Valid() {
		return fmt.Errorf("%w: %q", chat.ErrUnknownChannel, channel)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	d, ok := m.clients[clientID]
	if !ok {
		return chat.ErrNotFound
	}
	if d.UserID != userID {
		return chat.ErrNotOwned
	}
	d.Channel = channel
	m.clients[clientID] = d
	return nil
}

// SetClientModel : Chooses which model answers a client's prompts.
func (m *Repository) SetClientModel(_ context.Context, userID, clientID string, model chat.Model) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, ok := m.clients[clientID]
	if !ok {
		return chat.ErrNotFound
	}
	if client.UserID != userID {
		return chat.ErrNotOwned
	}

	client.Model = model
	client.UpdatedAt = time.Now().UTC().Truncate(chat.StoredPrecision)
	m.clients[clientID] = client
	return nil
}

// RevokeClient : Stops a client authenticating.
func (m *Repository) RevokeClient(_ context.Context, userID, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.clients[clientID]
	if !ok {
		return chat.ErrNotFound
	}
	if d.UserID != userID {
		return chat.ErrNotOwned
	}
	if d.RevokedAt == nil {
		at := time.Now().UTC()
		d.RevokedAt = &at
		m.clients[clientID] = d
	}
	return nil
}

// RenameConversation : Changes a stored conversation's title.
func (m *Repository) RenameConversation(_ context.Context, userID, conversationID, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return chat.ErrNotFound
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}
	if err := conversation.Rename(title); err != nil {
		return err
	}
	m.conversations[conversationID] = conversation
	return nil
}

// SetActiveConversation : Points a client at a conversation its user owns.
func (m *Repository) SetActiveConversation(_ context.Context, userID, clientID, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return chat.ErrNotFound
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}
	d, ok := m.clients[clientID]
	if !ok || d.UserID != userID {
		return chat.ErrNotFound
	}
	d.ActiveConversationID = conversationID
	m.clients[clientID] = d
	return nil
}

// Unfinished : Returns a conversation's chats that have not finished.
func (m *Repository) Unfinished(_ context.Context, conversationID string) ([]string, error) {
	if conversationID == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, t := range m.chats {
		if t.ConversationID == conversationID && !t.Status.IsTerminal() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// FailUpdates : Makes every subsequent Update fail with err, or stop failing
// when err is nil.
func (m *Repository) FailUpdates(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateErr = err
}

// FailAppends : Makes every subsequent AppendMessage fail with err, or stop
// failing when err is nil.
func (m *Repository) FailAppends(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendErr = err
}

// FailRunningReason : Returns the reason given to the last FailRunning call,
// or the empty string if there has not been one.
func (m *Repository) FailRunningReason() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failRunningReason
}
