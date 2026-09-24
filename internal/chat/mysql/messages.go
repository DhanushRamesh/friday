package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/session"
)

// sessionMessageRow : The messages table, as GORM sees it.
type sessionMessageRow struct {
	SessionID string    `gorm:"column:session_id;primaryKey"`
	Seq       int       `gorm:"column:seq;primaryKey"`
	Kind      string    `gorm:"column:kind"`
	Role      string    `gorm:"column:role"`
	Content   string    `gorm:"column:content"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

// TableName : Names the table this row maps to.
func (sessionMessageRow) TableName() string { return "messages" }

// toMessage : Converts a stored row back into a message.
func (r *sessionMessageRow) toMessage() session.Message {
	return session.Message{
		SessionID: r.SessionID,
		Seq:       r.Seq,
		Kind:      session.Kind(r.Kind),
		Role:      session.Role(r.Role),
		Content:   r.Content,
		At:        r.CreatedAt.UTC(),
	}
}

// appendAttempts : How many times a position is retried before giving up.
//
// The next position is read and then written, so two appends to one session
// can choose the same one. The loser is refused by the primary key and tries
// again with the position the winner has now taken.
const appendAttempts = 5

// Append : Stores a message at the end of its session.
func (r *Repository) Append(ctx context.Context, m session.Message) (session.Message, error) {
	if err := m.Valid(); err != nil {
		return session.Message{}, err
	}
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}

	for attempt := 0; attempt < appendAttempts; attempt++ {
		var last int
		err := r.db.WithContext(ctx).
			Model(&sessionMessageRow{}).
			Where("session_id = ?", m.SessionID).
			Select("COALESCE(MAX(seq), 0)").
			Scan(&last).Error
		if err != nil {
			return session.Message{}, fmt.Errorf(
				"session: reading the end of %s: %w", m.SessionID, err)
		}

		m.Seq = last + 1
		row := &sessionMessageRow{
			SessionID: m.SessionID,
			Seq:       m.Seq,
			Kind:      string(m.Kind),
			Role:      string(m.Role),
			Content:   m.Content,
			CreatedAt: m.At,
		}

		err = r.db.WithContext(ctx).Create(row).Error
		if err == nil {
			return m, nil
		}
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			continue
		}
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return session.Message{}, session.ErrNoSession
		}
		return session.Message{}, fmt.Errorf(
			"session: appending to %s: %w", m.SessionID, err)
	}

	return session.Message{}, fmt.Errorf(
		"session: %s was being written to too fast to append", m.SessionID)
}

// Before : Returns a session's messages up to but not including seq.
func (r *Repository) Before(ctx context.Context, sessionID string, seq int) ([]session.Message, error) {
	return r.messages(ctx, sessionID, seq)
}

// All : Returns everything said in a session, oldest first.
func (r *Repository) All(ctx context.Context, sessionID string) ([]session.Message, error) {
	return r.messages(ctx, sessionID, 0)
}

// messages : Reads a session's messages, stopping before seq when it is
// positive and reading all of them when it is not.
func (r *Repository) messages(ctx context.Context, sessionID string, seq int) ([]session.Message, error) {
	if sessionID == "" {
		return nil, nil
	}

	query := r.db.WithContext(ctx).
		Model(&sessionMessageRow{}).
		Where("session_id = ?", sessionID)
	if seq > 0 {
		query = query.Where("seq < ?", seq)
	}

	var rows []sessionMessageRow
	if err := query.Order("seq").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("session: reading %s: %w", sessionID, err)
	}

	messages := make([]session.Message, len(rows))
	for i := range rows {
		messages[i] = rows[i].toMessage()
	}
	return messages, nil
}
