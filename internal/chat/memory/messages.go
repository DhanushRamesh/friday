package memory

import (
	"context"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// Append : Stores a message at the end of its session.
func (m *Repository) Append(_ context.Context, msg session.Message) (session.Message, error) {
	if err := msg.Valid(); err != nil {
		return session.Message{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.appendSaidErr != nil {
		return session.Message{}, m.appendSaidErr
	}
	if _, ok := m.sessions[msg.SessionID]; !ok {
		return session.Message{}, session.ErrNoSession
	}
	if msg.At.IsZero() {
		msg.At = time.Now().UTC()
	}

	msg.Seq = len(m.said[msg.SessionID]) + 1
	m.said[msg.SessionID] = append(m.said[msg.SessionID], msg)
	return msg, nil
}

// Before : Returns a session's messages up to but not including seq.
func (m *Repository) Before(_ context.Context, sessionID string, seq int) ([]session.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []session.Message
	for _, msg := range m.said[sessionID] {
		if seq > 0 && msg.Seq >= seq {
			break
		}
		out = append(out, msg)
	}
	return out, nil
}

// All : Returns everything said in a session, oldest first.
func (m *Repository) All(ctx context.Context, sessionID string) ([]session.Message, error) {
	return m.Before(ctx, sessionID, 0)
}

// FailAppendingSaid : Makes every Append fail with err, so a caller's
// handling of an unwritable message can be tested.
func (m *Repository) FailAppendingSaid(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendSaidErr = err
}

// Said : Returns what was said in a session, for a test to assert on.
func (m *Repository) Said(sessionID string) []session.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]session.Message(nil), m.said[sessionID]...)
}
