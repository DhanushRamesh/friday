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
	mu            sync.Mutex
	tasks         map[string]*task.Task
	messages      map[string][]task.Message
	conversations map[string]task.Conversation
	clients       map[string]task.Client

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
		clients:       map[string]task.Client{},
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
		if f.ClientID != "" {
			owner, ok := m.conversations[t.ConversationID]
			if !ok || owner.ClientID != f.ClientID {
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
func (m *memRepo) ListConversations(_ context.Context, clientID string, limit int) ([]task.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []task.Conversation
	for _, c := range m.conversations {
		if c.ClientID != clientID {
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

// CreateClient : Stores a new client.
func (m *memRepo) CreateClient(_ context.Context, c *task.Client) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[c.ID] = *c
	return nil
}

// GetClient : Returns a stored client.
func (m *memRepo) GetClient(_ context.Context, id string) (*task.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[id]
	if !ok {
		return nil, task.ErrNotFound
	}
	return &c, nil
}

// ClientByTokenHash : Returns the client authenticating with a token hash.
func (m *memRepo) ClientByTokenHash(_ context.Context, tokenHash string) (*task.Client, error) {
	if tokenHash == "" {
		return nil, task.ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		if c.TokenHash == tokenHash {
			if c.Revoked() {
				return nil, task.ErrRevoked
			}
			found := c
			return &found, nil
		}
	}
	return nil, task.ErrNotFound
}

// RevokeClient : Stops a client authenticating.
func (m *memRepo) RevokeClient(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[id]
	if !ok {
		return task.ErrNotFound
	}
	if c.RevokedAt == nil {
		at := time.Now().UTC()
		c.RevokedAt = &at
		m.clients[id] = c
	}
	return nil
}

// SetActiveConversation : Points a client at a conversation it owns.
func (m *memRepo) SetActiveConversation(_ context.Context, clientID, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return task.ErrNotFound
	}
	if conversation.ClientID != clientID {
		return task.ErrNotOwned
	}
	client, ok := m.clients[clientID]
	if !ok {
		return task.ErrNotFound
	}
	client.ActiveConversationID = conversationID
	m.clients[clientID] = client
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
