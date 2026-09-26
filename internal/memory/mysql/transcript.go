package mysql

import (
	"context"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// MaxScanned : The most exchanges one search compares against.
//
// Every vector is read and scored in Go, so the cost of a search is the cost
// of reading them. At a few thousand that is nothing; the cap is what stops
// a transcript of years making every turn slow. The most recent are kept,
// because what was said lately is what a question is most often about.
const MaxScanned = 20000

// vectorRow : The message_vectors table, as GORM sees it.
type vectorRow struct {
	MessageID      string    `gorm:"column:message_id;primaryKey"`
	UserID         string    `gorm:"column:user_id"`
	ConversationID string    `gorm:"column:conversation_id"`
	Text           string    `gorm:"column:text"`
	Embedding      []byte    `gorm:"column:embedding"`
	EmbedModel     string    `gorm:"column:embed_model;primaryKey"`
	SaidAt         time.Time `gorm:"column:said_at"`
}

// TableName : Names the table this row maps to.
func (vectorRow) TableName() string { return "message_vectors" }

// toExchange : Converts a stored row back into an exchange.
func (r *vectorRow) toExchange() memory.Exchange {
	return memory.Exchange{
		MessageID:      r.MessageID,
		UserID:         r.UserID,
		ConversationID: r.ConversationID,
		Text:           r.Text,
		At:             r.SaidAt.UTC(),
	}
}

// Transcript : A memory.Transcript backed by MySQL.
type Transcript struct {
	db *gorm.DB
}

// NewTranscript : Returns a Transcript reading and writing through db.
func NewTranscript(db *storage.DB) *Transcript { return &Transcript{db: db.DB} }

// pendingRow : One exchange still to be embedded, as the query builds it.
type pendingRow struct {
	MessageID      string    `gorm:"column:message_id"`
	UserID         string    `gorm:"column:user_id"`
	ConversationID string    `gorm:"column:conversation_id"`
	Said           string    `gorm:"column:said"`
	Reply          *string   `gorm:"column:reply"`
	SaidAt         time.Time `gorm:"column:said_at"`
}

// Unindexed : Exchanges with no vector from the named model, oldest first.
//
// The reply is the next thing the assistant said in that conversation, found
// per row rather than by joining, since a message may have no reply yet and
// must still be indexable.
func (t *Transcript) Unindexed(ctx context.Context, model string, limit int) ([]memory.Exchange, error) {
	if limit <= 0 {
		return nil, nil
	}

	const query = `
SELECT m.id AS message_id, c.user_id, m.conversation_id,
       m.content AS said, m.created_at AS said_at,
       (SELECT r.content FROM messages r
         WHERE r.conversation_id = m.conversation_id AND r.seq > m.seq
           AND r.role = 'assistant' AND r.content IS NOT NULL
         ORDER BY r.seq LIMIT 1) AS reply
  FROM messages m
  JOIN conversations c ON c.id = m.conversation_id
  LEFT JOIN message_vectors v
         ON v.message_id = m.id AND v.embed_model = ?
 WHERE m.role = 'user' AND m.content IS NOT NULL AND m.content <> ''
   AND c.user_id <> '' AND v.message_id IS NULL
 ORDER BY m.created_at ASC, m.id ASC
 LIMIT ?`

	var rows []pendingRow
	if err := t.db.WithContext(ctx).Raw(query, model, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("memory: listing what needs indexing: %w", err)
	}

	out := make([]memory.Exchange, 0, len(rows))
	for i := range rows {
		var reply string
		if rows[i].Reply != nil {
			reply = *rows[i].Reply
		}
		text := memory.ExchangeText(rows[i].Said, reply)
		if text == "" {
			continue
		}
		out = append(out, memory.Exchange{
			MessageID:      rows[i].MessageID,
			UserID:         rows[i].UserID,
			ConversationID: rows[i].ConversationID,
			Text:           text,
			At:             rows[i].SaidAt.UTC(),
		})
	}
	return out, nil
}

// Index : Stores the vector for one exchange.
//
// Written over any existing one, so an exchange re-indexed after its reply
// arrived replaces the version that had only half of it.
func (t *Transcript) Index(ctx context.Context, e memory.Exchange, model string, vector []float32) error {
	if len(vector) == 0 {
		return fmt.Errorf("memory: indexing %s: no vector", e.MessageID)
	}

	row := vectorRow{
		MessageID:      e.MessageID,
		UserID:         e.UserID,
		ConversationID: e.ConversationID,
		Text:           e.Text,
		Embedding:      toBytes(vector),
		EmbedModel:     model,
		SaidAt:         e.At,
	}

	err := t.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "message_id"}, {Name: "embed_model"}},
		DoUpdates: clause.AssignmentColumns([]string{"text", "embedding", "said_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("memory: indexing %s: %w", e.MessageID, err)
	}
	return nil
}

// NearestTo : The user's past exchanges closest to a vector, nearest first.
func (t *Transcript) NearestTo(ctx context.Context, userID string, q []float32,
	model, skipConversation string, limit int) ([]memory.Heard, error) {

	if limit <= 0 || len(q) == 0 {
		return nil, nil
	}

	query := t.db.WithContext(ctx).Model(&vectorRow{}).
		Where("user_id = ? AND embed_model = ?", userID, model)
	if skipConversation != "" {
		query = query.Where("conversation_id <> ?", skipConversation)
	}

	var rows []vectorRow
	if err := query.Order("said_at DESC").Limit(MaxScanned).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("memory: reading the transcript index: %w", err)
	}

	out := make([]memory.Heard, 0, len(rows))
	for i := range rows {
		out = append(out, memory.Heard{
			Exchange: rows[i].toExchange(),
			Score:    embed.Similarity(q, toVector(rows[i].Embedding)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Ensure the transcript satisfies the interface it exists to provide.
var _ memory.Transcript = (*Transcript)(nil)
