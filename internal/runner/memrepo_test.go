package runner_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/friday/internal/task"
)

// memRepo : An in-memory task.Repository, so the runner's logic can be tested
// without a database.
type memRepo struct {
	mu       sync.Mutex
	tasks    map[string]*task.Task
	messages map[string][]task.Message
	sessions map[string]task.Session
	clients  map[string]task.Client
	users    map[string]task.User

	// updateErr : When set, every Update fails with it.
	updateErr error
	// appendErr : When set, every AppendMessage fails with it.
	appendErr error
	// failRunningReason : The reason passed to the last FailRunning call.
	failRunningReason string
}

// newMemRepo : Returns an empty repository.
func newMemRepo() *memRepo {
	return &memRepo{
		tasks:    map[string]*task.Task{},
		messages: map[string][]task.Message{},
		sessions: map[string]task.Session{},
		clients:  map[string]task.Client{},
		users:    map[string]task.User{},
	}
}

// copyTask : Returns a copy, so callers cannot mutate stored state by holding
// a pointer, as a real repository's callers cannot.
func copyTask(t *task.Task) *task.Task {
	c := *t
	return &c
}

// Create : Stores a new task.
func (m *memRepo) Create(_ context.Context, t *task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = copyTask(t)
	return nil
}

// Get : Returns a stored task.
func (m *memRepo) Get(_ context.Context, id string) (*task.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, task.ErrNotFound
	}
	return copyTask(t), nil
}

// Update : Overwrites a stored task.
func (m *memRepo) Update(_ context.Context, t *task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.tasks[t.ID]; !ok {
		return task.ErrNotFound
	}
	m.tasks[t.ID] = copyTask(t)
	return nil
}

// List : Returns stored tasks, unordered, which is enough for these tests.
func (m *memRepo) List(_ context.Context, f task.Filter) ([]task.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []task.Summary
	for _, t := range m.tasks {
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
		out = append(out, task.Summary{
			ID: t.ID, SessionID: t.SessionID, Prompt: t.Prompt, Status: t.Status,
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

// AppendMessage : Records a message against a task.
func (m *memRepo) AppendMessage(_ context.Context, taskID, kind, text string) (task.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.appendErr != nil {
		return task.Message{}, m.appendErr
	}
	if _, ok := m.tasks[taskID]; !ok {
		return task.Message{}, task.ErrNotFound
	}
	msg := task.Message{
		TaskID:    taskID,
		Seq:       len(m.messages[taskID]) + 1,
		Kind:      kind,
		Text:      text,
		CreatedAt: time.Now().UTC(),
	}
	m.messages[taskID] = append(m.messages[taskID], msg)
	return msg, nil
}

// Messages : Returns a task's messages in order.
func (m *memRepo) Messages(_ context.Context, taskID string) ([]task.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]task.Message(nil), m.messages[taskID]...), nil
}

// FailRunning : Marks running tasks as failed.
func (m *memRepo) FailRunning(_ context.Context, reason string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failRunningReason = reason
	var changed int64
	for id, t := range m.tasks {
		if t.Status == task.StatusRunning {
			_ = t.Fail(reason)
			m.tasks[id] = t
			changed++
		}
	}
	return changed, nil
}

// texts : Returns the text of a task's stored messages.
func (m *memRepo) texts(taskID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, msg := range m.messages[taskID] {
		out = append(out, msg.Text)
	}
	return out
}

var _ task.Repository = (*memRepo)(nil)

// CreateSession : Stores a new session.
func (m *memRepo) CreateSession(_ context.Context, c *task.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = map[string]task.Session{}
	}
	m.sessions[c.ID] = *c
	return nil
}

// GetSession : Returns a stored session.
func (m *memRepo) GetSession(_ context.Context, id string) (*task.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.sessions[id]
	if !ok {
		return nil, task.ErrNotFound
	}
	return &c, nil
}

// ListSessions : Returns stored sessions.
func (m *memRepo) ListSessions(_ context.Context, userID string, limit int) ([]task.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []task.Session
	for _, c := range m.sessions {
		if c.UserID != userID {
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

// CreateUser : Stores a new user.
func (m *memRepo) CreateUser(_ context.Context, u *task.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.users {
		if existing.Username == u.Username {
			return task.ErrUsernameTaken
		}
	}
	m.users[u.ID] = *u
	return nil
}

// GetUser : Returns a stored user.
func (m *memRepo) GetUser(_ context.Context, id string) (*task.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return nil, task.ErrNotFound
	}
	return &u, nil
}

// UserByUsername : Returns the user with the given username.
func (m *memRepo) UserByUsername(_ context.Context, username string) (*task.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := task.NormaliseUsername(username)
	for _, u := range m.users {
		if u.Username == want {
			found := u
			return &found, nil
		}
	}
	return nil, task.ErrNotFound
}

// CreateClient : Stores a new client.
func (m *memRepo) CreateClient(_ context.Context, d *task.Client) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[d.ID] = *d
	return nil
}

// ClientByTokenHash : Returns the client authenticating with a token hash.
func (m *memRepo) ClientByTokenHash(_ context.Context, tokenHash string) (*task.Client, error) {
	if tokenHash == "" {
		return nil, task.ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.clients {
		if d.TokenHash == tokenHash {
			if d.Revoked() {
				return nil, task.ErrRevoked
			}
			found := d
			return &found, nil
		}
	}
	return nil, task.ErrNotFound
}

// ListClients : Returns a user's clients, newest first.
func (m *memRepo) ListClients(_ context.Context, userID string) ([]task.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []task.Client
	for _, d := range m.clients {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// RevokeClient : Stops a client authenticating.
func (m *memRepo) RevokeClient(_ context.Context, userID, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.clients[clientID]
	if !ok {
		return task.ErrNotFound
	}
	if d.UserID != userID {
		return task.ErrNotOwned
	}
	if d.RevokedAt == nil {
		at := time.Now().UTC()
		d.RevokedAt = &at
		m.clients[clientID] = d
	}
	return nil
}

// SetActiveSession : Points a client at a session its user owns.
func (m *memRepo) SetActiveSession(_ context.Context, userID, clientID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return task.ErrNotFound
	}
	if session.UserID != userID {
		return task.ErrNotOwned
	}
	d, ok := m.clients[clientID]
	if !ok || d.UserID != userID {
		return task.ErrNotFound
	}
	d.ActiveSessionID = sessionID
	m.clients[clientID] = d
	return nil
}

// History : Returns a session's turns, oldest first.
func (m *memRepo) History(_ context.Context, sessionID string, turns int) ([]task.Turn, error) {
	if sessionID == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, t := range m.tasks {
		if t.SessionID == sessionID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids) // ULIDs order by creation time

	var history []task.Turn
	for _, id := range ids {
		t := m.tasks[id]
		history = append(history, task.Turn{Role: task.RoleUser, Text: t.Prompt})
		if t.Response != "" {
			history = append(history, task.Turn{Role: task.RoleAssistant, Text: t.Response})
		}
	}
	if turns > 0 && len(history) > turns {
		history = history[len(history)-turns:]
	}
	return task.MergeTurns(history), nil
}

// Unfinished : Returns a session's tasks that have not finished.
func (m *memRepo) Unfinished(_ context.Context, sessionID string) ([]string, error) {
	if sessionID == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, t := range m.tasks {
		if t.SessionID == sessionID && !t.Status.IsTerminal() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
