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
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// Repository : An in-memory chat.Repository.
//
// The zero value is unusable; call New.
type Repository struct {
	mu       sync.Mutex
	chats    map[string]*chat.Chat
	messages map[string][]chat.Message

	// said : Each session's conversation, in the order it was said.
	said          map[string][]session.Message
	appendSaidErr error
	sessions      map[string]chat.Session
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
		chats:    map[string]*chat.Chat{},
		messages: map[string][]chat.Message{},
		said:     map[string][]session.Message{},
		sessions: map[string]chat.Session{},
		clients:  map[string]chat.Client{},
		users:    map[string]chat.User{},
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
		if f.SessionID != "" && t.SessionID != f.SessionID {
			continue
		}
		if f.UserID != "" {
			owner, ok := m.sessions[t.SessionID]
			if !ok || owner.UserID != f.UserID {
				continue
			}
		}
		out = append(out, chat.Summary{
			ID: t.ID, SessionID: t.SessionID, Prompt: t.Prompt,
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

// AppendMessage : Records a message against a chat.
func (m *Repository) AppendMessage(_ context.Context, chatID, kind, text string) (chat.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.appendErr != nil {
		return chat.Message{}, m.appendErr
	}
	if _, ok := m.chats[chatID]; !ok {
		return chat.Message{}, chat.ErrNotFound
	}
	msg := chat.Message{
		ChatID:    chatID,
		Seq:       len(m.messages[chatID]) + 1,
		Kind:      kind,
		Text:      text,
		CreatedAt: time.Now().UTC(),
	}
	m.messages[chatID] = append(m.messages[chatID], msg)
	return msg, nil
}

// Messages : Returns a chat's messages in order.
func (m *Repository) Messages(_ context.Context, chatID string) ([]chat.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]chat.Message(nil), m.messages[chatID]...), nil
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

// CreateSession : Stores a new session.
func (m *Repository) CreateSession(_ context.Context, c *chat.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = map[string]chat.Session{}
	}
	m.sessions[c.ID] = *c
	return nil
}

// GetSession : Returns a stored session.
func (m *Repository) GetSession(_ context.Context, id string) (*chat.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.sessions[id]
	if !ok {
		return nil, chat.ErrNotFound
	}
	return &c, nil
}

// ListSessions : Returns stored sessions.
func (m *Repository) ListSessions(ctx context.Context, userID string, limit int) ([]chat.Session, error) {
	return m.listSessions(ctx, userID, limit, false)
}

// ListArchivedSessions : Returns stored sessions that have been put away.
func (m *Repository) ListArchivedSessions(ctx context.Context, userID string, limit int) ([]chat.Session, error) {
	return m.listSessions(ctx, userID, limit, true)
}

func (m *Repository) listSessions(_ context.Context, userID string, limit int, archived bool) ([]chat.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []chat.Session
	for _, c := range m.sessions {
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

// SetSessionArchived : Puts a stored session away or brings it back.
func (m *Repository) SetSessionArchived(_ context.Context, userID, sessionID string, archived bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return chat.ErrNotFound
	}
	if session.UserID != userID {
		return chat.ErrNotOwned
	}
	if archived {
		session.Archive()
		m.clearActive(sessionID)
	} else {
		session.Unarchive()
	}
	m.sessions[sessionID] = session
	return nil
}

// DeleteSession : Removes a stored session and everything said in it.
func (m *Repository) DeleteSession(_ context.Context, userID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return chat.ErrNotFound
	}
	if session.UserID != userID {
		return chat.ErrNotOwned
	}

	m.clearActive(sessionID)
	delete(m.sessions, sessionID)
	// Standing in for the database's cascades, so a test sees what a real
	// delete leaves behind rather than a session's chats outliving it.
	for id, t := range m.chats {
		if t.SessionID == sessionID {
			delete(m.chats, id)
			delete(m.messages, id)
		}
	}
	delete(m.said, sessionID)
	return nil
}

// clearActive : Unpoints every client using a session. The caller holds mu.
func (m *Repository) clearActive(sessionID string) {
	for id, d := range m.clients {
		if d.ActiveSessionID == sessionID {
			d.ActiveSessionID = ""
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
func (m *Repository) ListClients(_ context.Context, userID string) ([]chat.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []chat.Client
	for _, d := range m.clients {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
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

// RenameSession : Changes a stored session's title.
func (m *Repository) RenameSession(_ context.Context, userID, sessionID, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return chat.ErrNotFound
	}
	if session.UserID != userID {
		return chat.ErrNotOwned
	}
	if err := session.Rename(title); err != nil {
		return err
	}
	m.sessions[sessionID] = session
	return nil
}

// SetActiveSession : Points a client at a session its user owns.
func (m *Repository) SetActiveSession(_ context.Context, userID, clientID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return chat.ErrNotFound
	}
	if session.UserID != userID {
		return chat.ErrNotOwned
	}
	d, ok := m.clients[clientID]
	if !ok || d.UserID != userID {
		return chat.ErrNotFound
	}
	d.ActiveSessionID = sessionID
	m.clients[clientID] = d
	return nil
}

// Unfinished : Returns a session's chats that have not finished.
func (m *Repository) Unfinished(_ context.Context, sessionID string) ([]string, error) {
	if sessionID == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, t := range m.chats {
		if t.SessionID == sessionID && !t.Status.IsTerminal() {
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

// Texts : Returns the text of a chat's messages, in the order they arrived.
func (m *Repository) Texts(chatID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	texts := make([]string, 0, len(m.messages[chatID]))
	for _, msg := range m.messages[chatID] {
		texts = append(texts, msg.Text)
	}
	return texts
}
