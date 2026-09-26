// Package mysql stores memories in MySQL.
//
// MySQL Community has no vector distance function, so nothing here searches
// vectors. They are stored as bytes, read back, and compared in Go.
package mysql

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// row : The memories table, as GORM sees it.
//
// Kept separate from memory.Memory so the domain type carries no persistence
// tags. CreatedAt and UpdatedAt disable GORM's automatic timestamps, which
// would otherwise overwrite what the domain recorded.
type row struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	Tier       string     `gorm:"column:tier"`
	Subject    string     `gorm:"column:subject"`
	Body       string     `gorm:"column:body"`
	Embedding  []byte     `gorm:"column:embedding"`
	EmbedModel string     `gorm:"column:embed_model"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	LastUsedAt *time.Time `gorm:"column:last_used_at"`
	Uses       int        `gorm:"column:uses"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "memories" }

// toMemory : Converts a stored row back into a memory.
func (r *row) toMemory() memory.Memory {
	m := memory.Memory{
		ID:         r.ID,
		UserID:     r.UserID,
		Tier:       memory.Tier(r.Tier),
		Subject:    r.Subject,
		Body:       r.Body,
		EmbedModel: r.EmbedModel,
		Embedding:  toVector(r.Embedding),
		CreatedAt:  r.CreatedAt.UTC(),
		UpdatedAt:  r.UpdatedAt.UTC(),
		Uses:       r.Uses,
	}
	if r.LastUsedAt != nil {
		at := r.LastUsedAt.UTC()
		m.LastUsedAt = &at
	}
	return m
}

// toRow : Converts a memory into the row that stores it.
func toRow(m *memory.Memory) *row {
	return &row{
		ID:         m.ID,
		UserID:     m.UserID,
		Tier:       string(m.Tier),
		Subject:    m.Subject,
		Body:       m.Body,
		Embedding:  toBytes(m.Embedding),
		EmbedModel: m.EmbedModel,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
		LastUsedAt: m.LastUsedAt,
		Uses:       m.Uses,
	}
}

// toBytes : A vector as little-endian float32s, or nil when there is none.
func toBytes(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	out := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(f))
	}
	return out
}

// toVector : The float32s stored in b. A length that is not a multiple of
// four is treated as no vector rather than half of one.
func toVector(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

// Store : A memory.Store backed by MySQL.
type Store struct {
	db *gorm.DB
}

// New : Returns a Store reading and writing through db.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// Create : Stores a new memory.
func (s *Store) Create(ctx context.Context, m *memory.Memory) error {
	if err := m.Valid(); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(toRow(m)).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("memory: %s already exists", m.ID)
		}
		return fmt.Errorf("memory: creating %s: %w", m.ID, err)
	}
	return nil
}

// Get : Returns one memory, or memory.ErrNotFound.
func (s *Store) Get(ctx context.Context, userID, id string) (*memory.Memory, error) {
	var r row
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, memory.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("memory: reading %s: %w", id, err)
	}
	m := r.toMemory()
	return &m, nil
}

// Update : Replaces a memory's subject, body and tier.
//
// The embedding is cleared in the same statement. The text it described has
// changed, and a vector left behind would keep matching the old wording.
func (s *Store) Update(ctx context.Context, m *memory.Memory) error {
	if err := m.Valid(); err != nil {
		return err
	}
	m.UpdatedAt = time.Now().UTC()
	m.Embedding, m.EmbedModel = nil, ""

	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ?", m.ID, m.UserID).
		Updates(map[string]any{
			"tier":        string(m.Tier),
			"subject":     m.Subject,
			"body":        m.Body,
			"embedding":   nil,
			"embed_model": "",
			"updated_at":  m.UpdatedAt,
		})
	if out.Error != nil {
		return fmt.Errorf("memory: updating %s: %w", m.ID, out.Error)
	}
	if out.RowsAffected == 0 {
		return memory.ErrNotFound
	}
	return nil
}

// Forget : Removes a memory. Removing one already gone is not an error: the
// caller asked for it to be absent and it is absent.
func (s *Store) Forget(ctx context.Context, userID, id string) error {
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&row{}).Error
	if err != nil {
		return fmt.Errorf("memory: forgetting %s: %w", id, err)
	}
	return nil
}

// All : Every memory in a tier, oldest first.
func (s *Store) All(ctx context.Context, userID string, tier memory.Tier) ([]memory.Memory, error) {
	var rows []row
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND tier = ?", userID, string(tier)).
		Order("created_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("memory: listing %s: %w", tier, err)
	}
	return toMemories(rows), nil
}

// SetEmbedding : Records the vector for a memory, and what produced it.
func (s *Store) SetEmbedding(ctx context.Context, id, model string, vector []float32) error {
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"embedding":   toBytes(vector),
			"embed_model": model,
		}).Error
	if err != nil {
		return fmt.Errorf("memory: storing the vector for %s: %w", id, err)
	}
	return nil
}

// Unembedded : Memories with no vector from the named model, oldest first.
func (s *Store) Unembedded(ctx context.Context, model string, limit int) ([]memory.Memory, error) {
	if limit <= 0 {
		return nil, nil
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Where("embedding IS NULL OR embed_model <> ?", model).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("memory: listing what needs embedding: %w", err)
	}
	return toMemories(rows), nil
}

// Used : Records that memories were given to the model.
func (s *Store) Used(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"last_used_at": time.Now().UTC(),
			"uses":         gorm.Expr("uses + 1"),
		}).Error
	if err != nil {
		return fmt.Errorf("memory: recording use: %w", err)
	}
	return nil
}

// toMemories : Converts stored rows back into memories.
func toMemories(rows []row) []memory.Memory {
	out := make([]memory.Memory, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toMemory())
	}
	return out
}

// Matching : Finds candidates with the full-text index.
//
// The fallback, for when there is no embedding server or a memory has not
// been embedded yet. It matches wording rather than meaning, so it finds a
// memory when the question reuses its words and misses when it does not.
func (s *Store) Matching(ctx context.Context, userID, question string, limit int) ([]memory.Match, error) {
	terms := searchTerms(question)
	if terms == "" {
		return nil, nil
	}

	var rows []row
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND MATCH(subject, body) AGAINST (? IN BOOLEAN MODE)", userID, terms).
		Order(gorm.Expr("MATCH(subject, body) AGAINST (? IN BOOLEAN MODE) DESC", terms)).
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("memory: searching by words: %w", err)
	}

	out := make([]memory.Match, 0, len(rows))
	for i := range rows {
		out = append(out, memory.Match{Memory: rows[i].toMemory(), ByWords: true})
	}
	return out, nil
}

// searchTerms : A question as a boolean-mode expression.
//
// Natural language mode ignores any word appearing in more than half the
// rows, which on a small table is most of them. Boolean mode has no such
// rule. Each word is given a trailing wildcard so a plural finds a singular.
func searchTerms(question string) string {
	var out []string
	for _, word := range strings.FieldsFunc(question, func(r rune) bool {
		return !isWordRune(r)
	}) {
		if len(word) > 2 {
			out = append(out, word+"*")
		}
	}
	return strings.Join(out, " ")
}

// isWordRune : Whether a rune can be part of a search term. Anything else
// would be punctuation that boolean mode reads as an operator.
func isWordRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// NearestTo : Finds candidates by comparing vectors in Go.
//
// Every embedded memory of this user is read and scored. MySQL cannot do the
// comparison, and at the size a person's memory reaches the scan is cheaper
// than the round trip that fetched the rows.
func (s *Store) NearestTo(ctx context.Context, userID string, q embed.Vector, model string, limit int) ([]memory.Match, error) {
	var rows []row
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND embed_model = ? AND embedding IS NOT NULL", userID, model).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("memory: reading vectors: %w", err)
	}

	out := make([]memory.Match, 0, len(rows))
	for i := range rows {
		m := rows[i].toMemory()
		out = append(out, memory.Match{Memory: m, Score: embed.Similarity(q, m.Embedding)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Ensure the store satisfies the interface it exists to provide.
var _ memory.Store = (*Store)(nil)
