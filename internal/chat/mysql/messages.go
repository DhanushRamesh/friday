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
	ID        string    `gorm:"column:id;primaryKey"`
	SessionID string    `gorm:"column:session_id"`
	Seq       int       `gorm:"column:seq"`
	Kind      string    `gorm:"column:kind"`
	Role      string    `gorm:"column:role"`
	Content   string    `gorm:"column:content"`
	Detail    *string   `gorm:"column:detail"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

// TableName : Names the table this row maps to.
func (sessionMessageRow) TableName() string { return "messages" }

// toMessage : Converts a stored row back into a message.
func (r *sessionMessageRow) toMessage() session.Message {
	return session.Message{
		ID:        r.ID,
		SessionID: r.SessionID,
		Seq:       r.Seq,
		Kind:      session.Kind(r.Kind),
		Role:      session.Role(r.Role),
		Content:   r.Content,
		Detail:    value(r.Detail),
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
	// Filled before validating, not after, so a caller building a message
	// literally does not have to know which fields the store supplies. The
	// constructors set both; this is for everything else.
	if m.ID == "" {
		m.ID = session.NewMessageID()
	}
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}
	if err := m.Valid(); err != nil {
		return session.Message{}, err
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
			ID:        m.ID,
			SessionID: m.SessionID,
			Seq:       m.Seq,
			Kind:      string(m.Kind),
			Role:      string(m.Role),
			Content:   m.Content,
			Detail:    nullable(m.Detail),
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

// sessionSummaryRow : The condensation columns of the sessions table.
//
// Separate from sessionRow because that one is written with an explicit list
// of columns, and a field added there but forgotten in the list is stored
// nowhere. These two are read and written on their own.
type sessionSummaryRow struct {
	Summary    *string `gorm:"column:summary"`
	ThroughSeq int     `gorm:"column:summarised_through_seq"`
}

// Summary : Returns the session's condensed earlier conversation.
func (r *Repository) Summary(ctx context.Context, sessionID string) (session.Summary, error) {
	if sessionID == "" {
		return session.Summary{}, session.ErrNoSession
	}

	var row sessionSummaryRow
	err := r.db.WithContext(ctx).
		Model(&sessionRow{}).
		Select("summary", "summarised_through_seq").
		Where("id = ?", sessionID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return session.Summary{}, session.ErrNoSession
	}
	if err != nil {
		return session.Summary{}, fmt.Errorf(
			"session: reading the summary of %s: %w", sessionID, err)
	}

	return session.Summary{Text: value(row.Summary), ThroughSeq: row.ThroughSeq}, nil
}

// SetSummary : Replaces the session's condensed earlier conversation.
func (r *Repository) SetSummary(ctx context.Context, sessionID string, s session.Summary) error {
	if sessionID == "" {
		return session.ErrNoSession
	}

	// A map names the columns at the point of writing, so there is no
	// separate list to keep in step with it.
	res := r.db.WithContext(ctx).
		Model(&sessionRow{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"summary":                nullable(s.Text),
			"summarised_through_seq": s.ThroughSeq,
		})
	if res.Error != nil {
		return fmt.Errorf("session: writing the summary of %s: %w", sessionID, res.Error)
	}
	if res.RowsAffected > 0 {
		return nil
	}

	// MySQL counts rows it changed, not rows it matched, so writing the same
	// summary twice affects none. Only a session that is not there is an
	// error.
	return r.sessionExists(ctx, sessionID)
}

// sessionExists : Reports ErrNoSession when the session is not there, and nil
// when it is.
func (r *Repository) sessionExists(ctx context.Context, sessionID string) error {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sessionRow{}).
		Where("id = ?", sessionID).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("session: looking for %s: %w", sessionID, err)
	}
	if count == 0 {
		return session.ErrNoSession
	}
	return nil
}
