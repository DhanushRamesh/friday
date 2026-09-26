package memory

import (
	"context"
	"sort"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Append : Stores a message at the end of its conversation.
func (m *Repository) Append(_ context.Context, msg conversation.Message) (conversation.Message, error) {
	if err := msg.Valid(); err != nil {
		return conversation.Message{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.appendSaidErr != nil {
		return conversation.Message{}, m.appendSaidErr
	}
	if _, ok := m.conversations[msg.ConversationID]; !ok {
		return conversation.Message{}, conversation.ErrNoConversation
	}
	if msg.At.IsZero() {
		msg.At = time.Now().UTC()
	}

	msg.Seq = len(m.said[msg.ConversationID]) + 1
	m.said[msg.ConversationID] = append(m.said[msg.ConversationID], msg)
	return msg, nil
}

// Before : Returns a conversation's messages up to but not including seq.
func (m *Repository) Before(_ context.Context, conversationID string, seq int) ([]conversation.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []conversation.Message
	for _, msg := range m.said[conversationID] {
		if seq > 0 && msg.Seq >= seq {
			break
		}
		out = append(out, msg)
	}
	return out, nil
}

// All : Returns everything said in a conversation, oldest first.
func (m *Repository) All(ctx context.Context, conversationID string) ([]conversation.Message, error) {
	return m.Before(ctx, conversationID, 0)
}

// FailAppendingSaid : Makes every Append fail with err, so a caller's
// handling of an unwritable message can be tested.
// ByChat : Returns everything one turn wrote, oldest first.
func (m *Repository) ByChat(_ context.Context, chatID string) ([]conversation.Message, error) {
	if chatID == "" {
		return nil, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var out []conversation.Message
	for _, said := range m.said {
		for _, msg := range said {
			if msg.ChatID == chatID {
				out = append(out, msg)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func (m *Repository) FailAppendingSaid(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendSaidErr = err
}

// Said : Returns what was said in a conversation, for a test to assert on.
func (m *Repository) Said(conversationID string) []conversation.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]conversation.Message(nil), m.said[conversationID]...)
}

// Summary : Returns the conversation's condensed earlier conversation.
func (m *Repository) Summary(_ context.Context, conversationID string) (conversation.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.conversations[conversationID]; !ok {
		return conversation.Summary{}, conversation.ErrNoConversation
	}
	return m.summaries[conversationID], nil
}

// SetSummary : Replaces the conversation's condensed earlier conversation.
func (m *Repository) SetSummary(_ context.Context, conversationID string, s conversation.Summary) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.conversations[conversationID]; !ok {
		return conversation.ErrNoConversation
	}
	m.summaries[conversationID] = s
	return nil
}
