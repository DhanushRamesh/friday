package runner_test

import (
	"context"
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
		out = append(out, task.Summary{ID: t.ID, Prompt: t.Prompt, Status: t.Status})
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
