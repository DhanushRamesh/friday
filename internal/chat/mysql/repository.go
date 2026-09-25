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

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
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
	// So that listing conversations brings the most recently used to the top.
	return r.touchConversation(ctx, t.ConversationID, t.CreatedAt)
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
		Select("prompt", "status", "response", "error", "error_code", "error_detail",
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
	if f.ConversationID != "" {
		query = query.Where("conversation_id = ?", f.ConversationID)
	}
	if f.UserID != "" {
		// One user must never see another's chats.
		query = query.Where("conversation_id IN (?)",
			r.db.Model(&conversationRow{}).Select("id").Where("user_id = ?", f.UserID))
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

// Repository implements the interface the rest of the server depends on.
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
		ID:                   d.ID,
		UserID:               nullable(d.UserID),
		Name:                 d.Name,
		Channel:              string(d.Channel),
		Vendor:               nullable(d.Model.Vendor),
		Model:                nullable(d.Model.ID),
		TokenHash:            nullable(d.TokenHash),
		ActiveConversationID: nullable(d.ActiveConversationID),
		CreatedAt:            d.CreatedAt,
		UpdatedAt:            d.UpdatedAt,
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
func (r *Repository) ListClients(ctx context.Context, userID string, revoked bool) ([]chat.Client, error) {
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if revoked {
		q = q.Where("revoked_at IS NOT NULL")
	} else {
		q = q.Where("revoked_at IS NULL")
	}

	var rows []clientRow
	err := q.Order("id DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("chat: listing clients: %w", err)
	}

	out := make([]chat.Client, len(rows))
	for i := range rows {
		out[i] = *rows[i].toClient()
	}
	return out, nil
}

// ReissueClientToken : Replaces a client's token with a new one.
func (r *Repository) ReissueClientToken(ctx context.Context, userID, clientID, tokenHash string) (*chat.Client, error) {
	var row clientRow
	err := r.db.WithContext(ctx).First(&row, "id = ?", clientID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: reading client %s: %w", clientID, err)
	}
	if value(row.UserID) != userID {
		return nil, chat.ErrNotOwned
	}
	// A revoked client is gone as far as signing in is concerned. Reviving
	// one by logging in would make revoking it mean nothing.
	if row.RevokedAt != nil {
		return nil, chat.ErrNotFound
	}

	now := time.Now().UTC().Truncate(chat.StoredPrecision)
	err = r.db.WithContext(ctx).
		Model(&clientRow{}).
		Where("id = ?", clientID).
		Updates(map[string]any{"token_hash": tokenHash, "updated_at": now}).Error
	if err != nil {
		return nil, fmt.Errorf("chat: reissuing token for %s: %w", clientID, err)
	}

	row.TokenHash = &tokenHash
	row.UpdatedAt = now
	return row.toClient(), nil
}

// SetClientChannel : Changes how a client's prompts are treated.
//
// This exists because a client cannot always say what it is when it
// registers. Home Assistant is handed a token and has no field to declare
// itself with, so a satellite's client would otherwise be stuck as direct --
// which is the answer that grants more, and the wrong one to be stuck on.
func (r *Repository) SetClientChannel(ctx context.Context, userID, clientID string, channel chat.Channel) error {
	if !channel.Valid() {
		return fmt.Errorf("%w: %q", chat.ErrUnknownChannel, channel)
	}

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

	err = r.db.WithContext(ctx).
		Model(&clientRow{}).
		Where("id = ?", clientID).
		Updates(map[string]any{
			"channel":    string(channel),
			"updated_at": time.Now().UTC().Truncate(chat.StoredPrecision),
		}).Error
	if err != nil {
		return fmt.Errorf("chat: setting channel on %s: %w", clientID, err)
	}
	return nil
}

// SetClientModel : Chooses which model answers a client's prompts.
//
// The zero Model clears the choice, so the client goes back to the server's
// configured one. That is a real thing to want and is not an error.
func (r *Repository) SetClientModel(ctx context.Context, userID, clientID string, model chat.Model) error {
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

	err = r.db.WithContext(ctx).
		Model(&clientRow{}).
		Where("id = ?", clientID).
		Updates(map[string]any{
			"vendor":     nullable(model.Vendor),
			"model":      nullable(model.ID),
			"updated_at": time.Now().UTC().Truncate(chat.StoredPrecision),
		}).Error
	if err != nil {
		return fmt.Errorf("chat: setting model on %s: %w", clientID, err)
	}
	return nil
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

// SetActiveConversation : Makes a conversation the one a prompt from this
// client lands in.
//
// The conversation must belong to the client's user, not to the client: a
// person may switch any client to any of their conversations.
// RenameConversation : Changes a conversation's title.
//
// Ownership is checked before the write rather than folded into its WHERE
// clause, so that somebody else's conversation is refused as not theirs instead of
// silently matching no rows and looking like success.
func (r *Repository) RenameConversation(ctx context.Context, userID, conversationID, title string) error {
	conversation, err := r.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}
	if err := conversation.Rename(title); err != nil {
		return err
	}

	// updated_at is left alone: it means when the conversation last moved,
	// and a listing is ordered by it. Renaming is not talking, and should not
	// jump a conversation to the top.
	result := r.db.WithContext(ctx).
		Model(&conversationRow{}).
		Where("id = ?", conversationID).
		Update("title", conversation.Title)
	if result.Error != nil {
		return fmt.Errorf("chat: renaming conversation %s: %w", conversationID, result.Error)
	}
	return nil
}

// SetConversationArchived : Puts a conversation away or brings it back.
func (r *Repository) SetConversationArchived(ctx context.Context, userID, conversationID string, archived bool) error {
	conversation, err := r.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}

	if archived {
		conversation.Archive()
	} else {
		conversation.Unarchive()
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&conversationRow{}).
			Where("id = ?", conversationID).
			Update("archived_at", conversation.ArchivedAt).Error; err != nil {
			return fmt.Errorf("chat: archiving conversation %s: %w", conversationID, err)
		}
		if !archived {
			return nil
		}
		// A client left pointing at an archived conversation would keep putting
		// prompts into it, which is the one thing archiving is meant to stop.
		return clearActiveConversation(tx, conversationID)
	})
}

// DeleteConversation : Removes a conversation and everything said in it.
func (r *Repository) DeleteConversation(ctx context.Context, userID, conversationID string) error {
	conversation, err := r.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Before the row goes, not after: active_conversation_id has no foreign
		// key, so a client left pointing at a deleted conversation would be
		// holding an identifier nothing can resolve, and the next prompt
		// would fail on the chats foreign key instead.
		if err := clearActiveConversation(tx, conversationID); err != nil {
			return err
		}
		// The chats and the transcript follow by their own cascades.
		if err := tx.Where("id = ?", conversationID).Delete(&conversationRow{}).Error; err != nil {
			return fmt.Errorf("chat: deleting conversation %s: %w", conversationID, err)
		}
		return nil
	})
}

// clearActiveConversation : Unpoints every client that was using a conversation.
func clearActiveConversation(tx *gorm.DB, conversationID string) error {
	err := tx.Model(&clientRow{}).
		Where("active_conversation_id = ?", conversationID).
		Update("active_conversation_id", nil).Error
	if err != nil {
		return fmt.Errorf("chat: clearing active conversation %s: %w", conversationID, err)
	}
	return nil
}

func (r *Repository) SetActiveConversation(ctx context.Context, userID, clientID, conversationID string) error {
	conversation, err := r.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if conversation.UserID != userID {
		return chat.ErrNotOwned
	}

	result := r.db.WithContext(ctx).
		Model(&clientRow{}).
		Where("id = ? AND user_id = ?", clientID, userID).
		Updates(map[string]any{
			"active_conversation_id": conversationID,
			"updated_at":             time.Now().UTC().Truncate(chat.StoredPrecision),
		})
	if result.Error != nil {
		return fmt.Errorf("chat: activating conversation %s: %w", conversationID, result.Error)
	}
	if result.RowsAffected == 0 {
		var exists int64
		if err := r.db.WithContext(ctx).Model(&clientRow{}).
			Where("id = ? AND user_id = ?", clientID, userID).Count(&exists).Error; err != nil {
			return fmt.Errorf("chat: activating conversation %s: %w", conversationID, err)
		}
		if exists == 0 {
			return chat.ErrNotFound
		}
	}
	return nil
}

// CreateConversation : Stores a new conversation.
func (r *Repository) CreateConversation(ctx context.Context, c *chat.Conversation) error {
	row := &conversationRow{
		ID:        c.ID,
		UserID:    nullable(c.UserID),
		Title:     c.Title,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("chat: creating conversation %s: %w", c.ID, err)
	}
	return nil
}

// GetConversation : Returns a conversation.
func (r *Repository) GetConversation(ctx context.Context, id string) (*chat.Conversation, error) {
	var row conversationRow
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("chat: reading conversation %s: %w", id, err)
	}
	conversation := row.toConversation()
	return &conversation, nil
}

// ListConversations : Returns a user's conversations, most recently used
// first.
func (r *Repository) ListConversations(ctx context.Context, userID string, limit int) ([]chat.Conversation, error) {
	if limit <= 0 {
		limit = chat.DefaultListLimit
	}
	if limit > chat.MaxListLimit {
		limit = chat.MaxListLimit
	}

	return r.listConversations(ctx, userID, limit, false)
}

// ListArchivedConversations : Returns a user's archived conversations.
func (r *Repository) ListArchivedConversations(ctx context.Context, userID string, limit int) ([]chat.Conversation, error) {
	return r.listConversations(ctx, userID, limit, true)
}

// listConversations : The listing both views share.
//
// Archived conversations are excluded from the ordinary one rather than mixed in
// and filtered by the caller, because EnsureConversation takes the first row of it
// to decide where a prompt lands. An archived conversation reaching that would put
// a prompt into a conversation the user had put away.
func (r *Repository) listConversations(ctx context.Context, userID string, limit int, archived bool) ([]chat.Conversation, error) {
	if limit <= 0 {
		limit = chat.DefaultListLimit
	}
	if limit > chat.MaxListLimit {
		limit = chat.MaxListLimit
	}

	q := r.db.WithContext(ctx).Model(&conversationRow{}).Where("user_id = ?", userID)
	if archived {
		q = q.Where("archived_at IS NOT NULL")
	} else {
		q = q.Where("archived_at IS NULL")
	}

	var rows []conversationRow
	err := q.Order("updated_at DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("chat: listing conversations: %w", err)
	}

	out := make([]chat.Conversation, len(rows))
	for i := range rows {
		out[i] = rows[i].toConversation()
	}
	return out, nil
}

// touchConversation : Records that a conversation was used, so listing brings
// the most recent to the top.
func (r *Repository) touchConversation(ctx context.Context, id string, at time.Time) error {
	if id == "" {
		return nil
	}
	err := r.db.WithContext(ctx).
		Model(&conversationRow{}).
		Where("id = ?", id).
		Update("updated_at", at).Error
	if err != nil {
		return fmt.Errorf("chat: touching conversation %s: %w", id, err)
	}
	return nil
}

// Unfinished : Returns the identifiers of a conversation's chats that have not
// reached a terminal status, oldest first.
func (r *Repository) Unfinished(ctx context.Context, conversationID string) ([]string, error) {
	if conversationID == "" {
		return nil, nil
	}

	var ids []string
	err := r.db.WithContext(ctx).
		Model(&chatRow{}).
		Where("conversation_id = ? AND status IN ?", conversationID,
			[]string{string(chat.StatusPending), string(chat.StatusRunning)}).
		Order("id ASC").
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("chat: reading unfinished chats of %s: %w", conversationID, err)
	}
	return ids, nil
}
