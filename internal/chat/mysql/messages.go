package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// conversationMessageRow : The messages table, as GORM sees it.
type conversationMessageRow struct {
	ID             string    `gorm:"column:id;primaryKey"`
	ConversationID string    `gorm:"column:conversation_id"`
	Seq            int       `gorm:"column:seq"`
	Kind           string    `gorm:"column:kind"`
	Role           string    `gorm:"column:role"`
	Content        *string   `gorm:"column:content"`
	ToolCalls      *string   `gorm:"column:tool_calls"`
	ToolResults    *string   `gorm:"column:tool_results"`
	Detail         *string   `gorm:"column:detail"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

// TableName : Names the table this row maps to.
func (conversationMessageRow) TableName() string { return "messages" }

// toMessage : Converts a stored row back into a message.
func (r *conversationMessageRow) toMessage() conversation.Message {
	return conversation.Message{
		ID:             r.ID,
		ConversationID: r.ConversationID,
		Seq:            r.Seq,
		Kind:           conversation.Kind(r.Kind),
		Role:           conversation.Role(r.Role),
		Content:        value(r.Content),
		ToolCalls:      toToolCalls(r.ToolCalls),
		ToolResults:    toToolResults(r.ToolResults),
		Detail:         value(r.Detail),
		At:             r.CreatedAt.UTC(),
	}
}

// appendAttempts : How many times a position is retried before giving up.
//
// The next position is read and then written, so two appends to one conversation
// can choose the same one. The loser is refused by the primary key and tries
// again with the position the winner has now taken.
const appendAttempts = 5

// Append : Stores a message at the end of its conversation.
func (r *Repository) Append(ctx context.Context, m conversation.Message) (conversation.Message, error) {
	// Filled before validating, not after, so a caller building a message
	// literally does not have to know which fields the store supplies. The
	// constructors set both; this is for everything else.
	if m.ID == "" {
		m.ID = conversation.NewMessageID()
	}
	if m.At.IsZero() {
		m.At = time.Now().UTC()
	}
	if err := m.Valid(); err != nil {
		return conversation.Message{}, err
	}

	for attempt := 0; attempt < appendAttempts; attempt++ {
		var last int
		err := r.db.WithContext(ctx).
			Model(&conversationMessageRow{}).
			Where("conversation_id = ?", m.ConversationID).
			Select("COALESCE(MAX(seq), 0)").
			Scan(&last).Error
		if err != nil {
			return conversation.Message{}, fmt.Errorf(
				"conversation: reading the end of %s: %w", m.ConversationID, err)
		}

		m.Seq = last + 1
		row := &conversationMessageRow{
			ID:             m.ID,
			ConversationID: m.ConversationID,
			Seq:            m.Seq,
			Kind:           string(m.Kind),
			Role:           string(m.Role),
			Content:        nullable(m.Content),
			ToolCalls:      fromToolCalls(m.ToolCalls),
			ToolResults:    fromToolResults(m.ToolResults),
			Detail:         nullable(m.Detail),
			CreatedAt:      m.At,
		}

		err = r.db.WithContext(ctx).Create(row).Error
		if err == nil {
			return m, nil
		}
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			continue
		}
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return conversation.Message{}, conversation.ErrNoConversation
		}
		return conversation.Message{}, fmt.Errorf(
			"conversation: appending to %s: %w", m.ConversationID, err)
	}

	return conversation.Message{}, fmt.Errorf(
		"conversation: %s was being written to too fast to append", m.ConversationID)
}

// Before : Returns a conversation's messages up to but not including seq.
func (r *Repository) Before(ctx context.Context, conversationID string, seq int) ([]conversation.Message, error) {
	return r.messages(ctx, conversationID, seq)
}

// All : Returns everything said in a conversation, oldest first.
func (r *Repository) All(ctx context.Context, conversationID string) ([]conversation.Message, error) {
	return r.messages(ctx, conversationID, 0)
}

// messages : Reads a conversation's messages, stopping before seq when it is
// positive and reading all of them when it is not.
func (r *Repository) messages(ctx context.Context, conversationID string, seq int) ([]conversation.Message, error) {
	if conversationID == "" {
		return nil, nil
	}

	query := r.db.WithContext(ctx).
		Model(&conversationMessageRow{}).
		Where("conversation_id = ?", conversationID)
	if seq > 0 {
		query = query.Where("seq < ?", seq)
	}

	var rows []conversationMessageRow
	if err := query.Order("seq").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("conversation: reading %s: %w", conversationID, err)
	}

	messages := make([]conversation.Message, len(rows))
	for i := range rows {
		messages[i] = rows[i].toMessage()
	}
	return messages, nil
}

// conversationSummaryRow : The condensation columns of the conversations table.
//
// Separate from conversationRow because that one is written with an explicit list
// of columns, and a field added there but forgotten in the list is stored
// nowhere. These two are read and written on their own.
type conversationSummaryRow struct {
	Summary    *string `gorm:"column:summary"`
	ThroughSeq int     `gorm:"column:summarised_through_seq"`
}

// Summary : Returns the conversation's condensed earlier conversation.
func (r *Repository) Summary(ctx context.Context, conversationID string) (conversation.Summary, error) {
	if conversationID == "" {
		return conversation.Summary{}, conversation.ErrNoConversation
	}

	var row conversationSummaryRow
	err := r.db.WithContext(ctx).
		Model(&conversationRow{}).
		Select("summary", "summarised_through_seq").
		Where("id = ?", conversationID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return conversation.Summary{}, conversation.ErrNoConversation
	}
	if err != nil {
		return conversation.Summary{}, fmt.Errorf(
			"conversation: reading the summary of %s: %w", conversationID, err)
	}

	return conversation.Summary{Text: value(row.Summary), ThroughSeq: row.ThroughSeq}, nil
}

// SetSummary : Replaces the conversation's condensed earlier conversation.
func (r *Repository) SetSummary(ctx context.Context, conversationID string, s conversation.Summary) error {
	if conversationID == "" {
		return conversation.ErrNoConversation
	}

	// A map names the columns at the point of writing, so there is no
	// separate list to keep in step with it.
	res := r.db.WithContext(ctx).
		Model(&conversationRow{}).
		Where("id = ?", conversationID).
		Updates(map[string]any{
			"summary":                nullable(s.Text),
			"summarised_through_seq": s.ThroughSeq,
		})
	if res.Error != nil {
		return fmt.Errorf("conversation: writing the summary of %s: %w", conversationID, res.Error)
	}
	if res.RowsAffected > 0 {
		return nil
	}

	// MySQL counts rows it changed, not rows it matched, so writing the same
	// summary twice affects none. Only a conversation that is not there is an
	// error.
	return r.conversationExists(ctx, conversationID)
}

// conversationExists : Reports ErrNoConversation when the conversation is not there, and nil
// when it is.
func (r *Repository) conversationExists(ctx context.Context, conversationID string) error {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&conversationRow{}).
		Where("id = ?", conversationID).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("conversation: looking for %s: %w", conversationID, err)
	}
	if count == 0 {
		return conversation.ErrNoConversation
	}
	return nil
}

// The tool payloads are stored as JSON and read and written whole. They are
// never queried into, so a column holds them rather than a table of their
// own: a row per argument would buy nothing and cost a join on every turn.

// fromToolCalls : The calls as JSON, or nothing when there are none.
func fromToolCalls(calls []conversation.ToolCall) *string {
	if len(calls) == 0 {
		return nil
	}
	return nullable(asJSON(calls))
}

// fromToolResults : The results as JSON, or nothing when there are none.
func fromToolResults(results []conversation.ToolResult) *string {
	if len(results) == 0 {
		return nil
	}
	return nullable(asJSON(results))
}

// toToolCalls : The stored calls, or none.
//
// Unreadable JSON gives none rather than an error: a row written by another
// build, or damaged, should leave the rest of the conversation legible rather
// than make the whole of it unreadable.
func toToolCalls(raw *string) []conversation.ToolCall {
	var calls []conversation.ToolCall
	readJSON(raw, &calls)
	return calls
}

// toToolResults : The stored results, or none.
func toToolResults(raw *string) []conversation.ToolResult {
	var results []conversation.ToolResult
	readJSON(raw, &results)
	return results
}

// asJSON : Encodes a value, or empty when it somehow cannot be encoded.
//
// Only ever given slices of plain structs of strings, so the failing case is
// unreachable; empty reads back as no calls, which is the safe direction.
func asJSON(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}

// readJSON : Decodes into out, leaving it alone when there is nothing to read.
func readJSON(raw *string, out any) {
	if raw == nil || *raw == "" {
		return
	}
	_ = json.Unmarshal([]byte(*raw), out)
}
