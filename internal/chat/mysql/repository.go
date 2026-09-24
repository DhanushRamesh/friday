// Package mysql : Stores chats in MySQL.
//
// It holds the only knowledge of how a chat becomes a row. The domain type in
// internal/chat carries no persistence tags, so every mapping between the two
// happens here.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/DhanushRamesh/friday/internal/chat"
	"github.com/DhanushRamesh/friday/internal/storage"
)

// Repository : A chat.Repository backed by MySQL.
type Repository struct {
	db *gorm.DB
}

// NewRepository : Returns a Repository reading and writing through db.
func NewRepository(db *storage.DB) *Repository {
	return &Repository{db: db.DB}
}

// Create : Stores a new chat.
func (r *Repository) Create(ctx context.Context, t *chat.Chat) error {
	if err := r.db.WithContext(ctx).Create(toRow(t)).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("chat: %s already exists", t.ID)
		}
		return fmt.Errorf("chat: creating %s: %w", t.ID, err)
	}
	// So that listing sessions brings the most recently used to the top.
	return r.touchSession(ctx, t.SessionID, t.CreatedAt)
}

// Get : Returns the chat with the given identifier, including its response.
func (r *Repository) Get(ctx context.Context, id string) (*chat.Chat, error) {
	var row chatRow
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: reading %s: %w", id, err)
	}
	return row.toChat(), nil
}

// Update : Writes a chat's current state over the stored one.
//
// Every column is written rather than only those GORM considers changed,
// because a field returning to its zero value is a real change: a response
// cleared, or a chat moving back to running.
func (r *Repository) Update(ctx context.Context, t *chat.Chat) error {
	row := toRow(t)

	result := r.db.WithContext(ctx).
		Model(&chatRow{}).
		Where("id = ?", t.ID).
		Select("prompt", "status", "response", "error",
			"created_at", "updated_at", "started_at", "finished_at").
		Updates(row)

	if result.Error != nil {
		return fmt.Errorf("chat: updating %s: %w", t.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		// No rows changed means either the chat is gone or nothing differed.
		// Only the first is an error, so check which.
		var exists int64
		if err := r.db.WithContext(ctx).Model(&chatRow{}).
			Where("id = ?", t.ID).Count(&exists).Error; err != nil {
			return fmt.Errorf("chat: updating %s: %w", t.ID, err)
		}
		if exists == 0 {
			return chat.ErrNotFound
		}
	}
	return nil
}

// List : Returns chats newest first, without their responses.
func (r *Repository) List(ctx context.Context, f chat.Filter) ([]chat.Summary, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = chat.DefaultListLimit
	}
	if limit > chat.MaxListLimit {
		limit = chat.MaxListLimit
	}

	query := r.db.WithContext(ctx).
		Model(&chatRow{}).
		Select(summaryColumns).
		// Identifiers are ULIDs, so ordering by the primary key orders by
		// creation time without a sort.
		Order("id DESC").
		Limit(limit)

	if f.Status != "" {
		query = query.Where("status = ?", string(f.Status))
	}
	if f.SessionID != "" {
		query = query.Where("session_id = ?", f.SessionID)
	}
	if f.UserID != "" {
		// One user must never see another's chats.
		query = query.Where("session_id IN (?)",
			r.db.Model(&sessionRow{}).Select("id").Where("user_id = ?", f.UserID))
	}

	var rows []summaryRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("chat: listing: %w", err)
	}

	summaries := make([]chat.Summary, len(rows))
	for i := range rows {
		summaries[i] = rows[i].toSummary()
	}
	return summaries, nil
}

// AppendMessage : Records a message against a chat, assigning it the next
// position in that chat's stream.
//
// The position is computed in the insert rather than read first and written
// after, so two appends cannot settle on the same number. Were they to race,
// the primary key on (chat_id, seq) rejects the second.
func (r *Repository) AppendMessage(ctx context.Context, chatID, kind, text string) (chat.Message, error) {
	now := time.Now().UTC().Truncate(chat.StoredPrecision)

	err := r.db.WithContext(ctx).Exec(
		"INSERT INTO chat_updates (chat_id, seq, kind, `text`, created_at) "+
			"SELECT ?, COALESCE(MAX(seq), 0) + 1, ?, ?, ? FROM chat_updates WHERE chat_id = ?",
		chatID, kind, text, now, chatID,
	).Error
	if err != nil {
		// The foreign key rejects a message for a chat that does not exist.
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return chat.Message{}, chat.ErrNotFound
		}
		return chat.Message{}, fmt.Errorf("chat: appending message to %s: %w", chatID, err)
	}

	var row messageRow
	if err := r.db.WithContext(ctx).
		Where("chat_id = ?", chatID).
		Order("seq DESC").
		First(&row).Error; err != nil {
		return chat.Message{}, fmt.Errorf("chat: reading back message for %s: %w", chatID, err)
	}
	return row.toMessage(), nil
}

// Messages : Returns a chat's messages in the order they were produced.
func (r *Repository) Messages(ctx context.Context, chatID string) ([]chat.Message, error) {
	var rows []messageRow
	err := r.db.WithContext(ctx).
		Where("chat_id = ?", chatID).
		Order("seq ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("chat: reading messages for %s: %w", chatID, err)
	}

	messages := make([]chat.Message, len(rows))
	for i := range rows {
		messages[i] = rows[i].toMessage()
	}
	return messages, nil
}

// FailRunning : Marks every chat still recorded as running as failed.
func (r *Repository) FailRunning(ctx context.Context, reason string) (int64, error) {
	now := time.Now().UTC().Truncate(chat.StoredPrecision)

	result := r.db.WithContext(ctx).
		Model(&chatRow{}).
		Where("status = ?", string(chat.StatusRunning)).
		Updates(map[string]any{
			"status":      string(chat.StatusFailed),
			"error":       reason,
			"updated_at":  now,
			"finished_at": now,
		})
	if result.Error != nil {
		return 0, fmt.Errorf("chat: failing interrupted chats: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// Repository implements the interface the rest of FRIDAY depends on.
var _ chat.Repository = (*Repository)(nil)

// CreateUser : Stores a new user.
func (r *Repository) CreateUser(ctx context.Context, u *chat.User) error {
	row := &userRow{
		ID:           u.ID,
		Username:     u.Username,
		PasswordHash: u.PasswordHash,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return chat.ErrUsernameTaken
		}
		return fmt.Errorf("chat: creating user %s: %w", u.ID, err)
	}
	return nil
}

// UserByUsername : Returns the user with the given username.
func (r *Repository) UserByUsername(ctx context.Context, username string) (*chat.User, error) {
	var row userRow
	err := r.db.WithContext(ctx).First(&row, "username = ?", chat.NormaliseUsername(username)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: reading user: %w", err)
	}
	return row.toUser(), nil
}

// GetUser : Returns a user by identifier.
func (r *Repository) GetUser(ctx context.Context, id string) (*chat.User, error) {
	var row userRow
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: reading user %s: %w", id, err)
	}
	return row.toUser(), nil
}

// CreateClient : Stores a new client.
func (r *Repository) CreateClient(ctx context.Context, d *chat.Client) error {
	row := &clientRow{
		ID:              d.ID,
		UserID:          nullable(d.UserID),
		Name:            d.Name,
		TokenHash:       nullable(d.TokenHash),
		ActiveSessionID: nullable(d.ActiveSessionID),
		CreatedAt:       d.CreatedAt,
		UpdatedAt:       d.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("chat: creating client %s: %w", d.ID, err)
	}
	return nil
}

// ClientByTokenHash : Returns the client authenticating with the given token
// hash.
func (r *Repository) ClientByTokenHash(ctx context.Context, tokenHash string) (*chat.Client, error) {
	if tokenHash == "" {
		return nil, chat.ErrNotFound
	}

	var row clientRow
	err := r.db.WithContext(ctx).First(&row, "token_hash = ?", tokenHash).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: reading client by token: %w", err)
	}

	client := row.toClient()
	if client.Revoked() {
		return nil, chat.ErrRevoked
	}
	// A client with no user cannot authenticate: it predates users and
	// belongs to nobody.
	if client.UserID == "" {
		return nil, chat.ErrNotFound
	}
	return client, nil
}

// ListClients : Returns a user's clients, newest first.
func (r *Repository) ListClients(ctx context.Context, userID string) ([]chat.Client, error) {
	var rows []clientRow
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id DESC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("chat: listing clients: %w", err)
	}

	out := make([]chat.Client, len(rows))
	for i := range rows {
		out[i] = *rows[i].toClient()
	}
	return out, nil
}

// RevokeClient : Stops a client authenticating.
func (r *Repository) RevokeClient(ctx context.Context, userID, clientID string) error {
	var row clientRow
	err := r.db.WithContext(ctx).First(&row, "id = ?", clientID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return chat.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("chat: reading client %s: %w", clientID, err)
	}
	if value(row.UserID) != userID {
		return chat.ErrNotOwned
	}
	if row.RevokedAt != nil {
		return nil
	}

	now := time.Now().UTC().Truncate(chat.StoredPrecision)
	err = r.db.WithContext(ctx).
		Model(&clientRow{}).
		Where("id = ?", clientID).
		Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error
	if err != nil {
		return fmt.Errorf("chat: revoking client %s: %w", clientID, err)
	}
	return nil
}

// SetActiveSession : Makes a session the one a prompt from this
// client lands in.
//
// The session must belong to the client's user, not to the client: a
// person may switch any client to any of their sessions.
func (r *Repository) SetActiveSession(ctx context.Context, userID, clientID, sessionID string) error {
	session, err := r.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.UserID != userID {
		return chat.ErrNotOwned
	}

	result := r.db.WithContext(ctx).
		Model(&clientRow{}).
		Where("id = ? AND user_id = ?", clientID, userID).
		Updates(map[string]any{
			"active_session_id": sessionID,
			"updated_at":        time.Now().UTC().Truncate(chat.StoredPrecision),
		})
	if result.Error != nil {
		return fmt.Errorf("chat: activating session %s: %w", sessionID, result.Error)
	}
	if result.RowsAffected == 0 {
		var exists int64
		if err := r.db.WithContext(ctx).Model(&clientRow{}).
			Where("id = ? AND user_id = ?", clientID, userID).Count(&exists).Error; err != nil {
			return fmt.Errorf("chat: activating session %s: %w", sessionID, err)
		}
		if exists == 0 {
			return chat.ErrNotFound
		}
	}
	return nil
}

// CreateSession : Stores a new session.
func (r *Repository) CreateSession(ctx context.Context, c *chat.Session) error {
	row := &sessionRow{
		ID:        c.ID,
		UserID:    nullable(c.UserID),
		Title:     c.Title,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("chat: creating session %s: %w", c.ID, err)
	}
	return nil
}

// GetSession : Returns a session.
func (r *Repository) GetSession(ctx context.Context, id string) (*chat.Session, error) {
	var row sessionRow
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: reading session %s: %w", id, err)
	}
	session := row.toSession()
	return &session, nil
}

// ListSessions : Returns a user's sessions, most recently used
// first.
func (r *Repository) ListSessions(ctx context.Context, userID string, limit int) ([]chat.Session, error) {
	if limit <= 0 {
		limit = chat.DefaultListLimit
	}
	if limit > chat.MaxListLimit {
		limit = chat.MaxListLimit
	}

	var rows []sessionRow
	err := r.db.WithContext(ctx).
		Model(&sessionRow{}).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("chat: listing sessions: %w", err)
	}

	out := make([]chat.Session, len(rows))
	for i := range rows {
		out[i] = rows[i].toSession()
	}
	return out, nil
}

// touchSession : Records that a session was used, so listing brings
// the most recent to the top.
func (r *Repository) touchSession(ctx context.Context, id string, at time.Time) error {
	if id == "" {
		return nil
	}
	err := r.db.WithContext(ctx).
		Model(&sessionRow{}).
		Where("id = ?", id).
		Update("updated_at", at).Error
	if err != nil {
		return fmt.Errorf("chat: touching session %s: %w", id, err)
	}
	return nil
}

// Unfinished : Returns the identifiers of a session's chats that have not
// reached a terminal status, oldest first.
func (r *Repository) Unfinished(ctx context.Context, sessionID string) ([]string, error) {
	if sessionID == "" {
		return nil, nil
	}

	var ids []string
	err := r.db.WithContext(ctx).
		Model(&chatRow{}).
		Where("session_id = ? AND status IN ?", sessionID,
			[]string{string(chat.StatusPending), string(chat.StatusRunning)}).
		Order("id ASC").
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("chat: reading unfinished chats of %s: %w", sessionID, err)
	}
	return ids, nil
}
