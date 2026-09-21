package api

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/task"
)

// memRepo : An in-memory task.Repository, so the runner's logic can be tested
// without a database.
type memRepo struct {
	mu            sync.Mutex
	tasks         map[string]*task.Task
	messages      map[string][]task.Message
	conversations map[string]task.Conversation
	devices       map[string]task.Device
	users         map[string]task.User

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
		tasks:         map[string]*task.Task{},
		messages:      map[string][]task.Message{},
		conversations: map[string]task.Conversation{},
		devices:       map[string]task.Device{},
		users:         map[string]task.User{},
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
		if f.ConversationID != "" && t.ConversationID != f.ConversationID {
			continue
		}
		if f.UserID != "" {
			owner, ok := m.conversations[t.ConversationID]
			if !ok || owner.UserID != f.UserID {
				continue
			}
		}
		out = append(out, task.Summary{
			ID: t.ID, ConversationID: t.ConversationID, Prompt: t.Prompt, Status: t.Status,
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

// CreateConversation : Stores a new conversation.
func (m *memRepo) CreateConversation(_ context.Context, c *task.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conversations == nil {
		m.conversations = map[string]task.Conversation{}
	}
	m.conversations[c.ID] = *c
	return nil
}

// GetConversation : Returns a stored conversation.
func (m *memRepo) GetConversation(_ context.Context, id string) (*task.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[id]
	if !ok {
		return nil, task.ErrNotFound
	}
	return &c, nil
}

// ListConversations : Returns stored conversations.
func (m *memRepo) ListConversations(_ context.Context, userID string, limit int) ([]task.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []task.Conversation
	for _, c := range m.conversations {
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

// CreateDevice : Stores a new device.
func (m *memRepo) CreateDevice(_ context.Context, d *task.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[d.ID] = *d
	return nil
}

// DeviceByTokenHash : Returns the device authenticating with a token hash.
func (m *memRepo) DeviceByTokenHash(_ context.Context, tokenHash string) (*task.Device, error) {
	if tokenHash == "" {
		return nil, task.ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.devices {
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

// ListDevices : Returns a user's devices, newest first.
func (m *memRepo) ListDevices(_ context.Context, userID string) ([]task.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []task.Device
	for _, d := range m.devices {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// RevokeDevice : Stops a device authenticating.
func (m *memRepo) RevokeDevice(_ context.Context, userID, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[deviceID]
	if !ok {
		return task.ErrNotFound
	}
	if d.UserID != userID {
		return task.ErrNotOwned
	}
	if d.RevokedAt == nil {
		at := time.Now().UTC()
		d.RevokedAt = &at
		m.devices[deviceID] = d
	}
	return nil
}

// SetActiveConversation : Points a device at a conversation its user owns.
func (m *memRepo) SetActiveConversation(_ context.Context, userID, deviceID, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return task.ErrNotFound
	}
	if conversation.UserID != userID {
		return task.ErrNotOwned
	}
	d, ok := m.devices[deviceID]
	if !ok || d.UserID != userID {
		return task.ErrNotFound
	}
	d.ActiveConversationID = conversationID
	m.devices[deviceID] = d
	return nil
}

// History : Returns a conversation's turns, oldest first.
func (m *memRepo) History(_ context.Context, conversationID string, turns int) ([]task.Turn, error) {
	if conversationID == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, t := range m.tasks {
		if t.ConversationID == conversationID {
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

// Unfinished : Returns a conversation's tasks that have not finished.
func (m *memRepo) Unfinished(_ context.Context, conversationID string) ([]string, error) {
	if conversationID == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, t := range m.tasks {
		if t.ConversationID == conversationID && !t.Status.IsTerminal() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// errTestStorage : A storage failure used to check that internal errors are
// logged but never returned to a caller.
var errTestStorage = errors.New("storage exploded: dsn=user:password@tcp(db)/friday")

// recordingProvider : A provider that records the history it was given, so a
// test can check what a model would actually have seen.
type recordingProvider struct {
	delay time.Duration

	mu      sync.Mutex
	history []provider.Turn
}

// Name : Returns the provider's name.
func (r *recordingProvider) Name() string { return "recording" }

// Run : Records the request's history and answers at once.
func (r *recordingProvider) Run(ctx context.Context, req provider.Request) (<-chan provider.Message, error) {
	r.mu.Lock()
	r.history = append([]provider.Turn(nil), req.History...)
	r.mu.Unlock()

	ch := make(chan provider.Message)
	go func() {
		defer close(ch)
		if r.delay > 0 {
			select {
			case <-time.After(r.delay):
			case <-ctx.Done():
				return
			}
		}
		select {
		case ch <- provider.Final("an answer"):
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// lastHistory : Returns the history given to the most recent run.
func (r *recordingProvider) lastHistory() []provider.Turn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]provider.Turn(nil), r.history...)
}
