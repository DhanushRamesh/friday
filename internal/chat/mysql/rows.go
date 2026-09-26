package mysql

import (
	"encoding/json"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// chatRow : The chats table, as GORM sees it.
//
// It is kept separate from chat.Chat so that the domain type carries no
// persistence tags and no knowledge of how it is stored.
//
// CreatedAt and UpdatedAt disable GORM's automatic timestamps. GORM would
// otherwise overwrite them on every write, discarding the times the domain
// recorded and making a chat's own history disagree with the row.
type chatRow struct {
	ID             string     `gorm:"column:id;primaryKey"`
	ClientID       *string    `gorm:"column:client_id"`
	ConversationID *string    `gorm:"column:conversation_id"`
	Prompt         string     `gorm:"column:prompt"`
	Channel        string     `gorm:"column:channel"`
	Vendor         *string    `gorm:"column:vendor"`
	Model          *string    `gorm:"column:model"`
	Status         string     `gorm:"column:status"`
	Response       *string    `gorm:"column:response"`
	Error          *string    `gorm:"column:error"`
	ErrorCode      string     `gorm:"column:error_code"`
	ErrorDetail    *string    `gorm:"column:error_detail"`
	Recalled       *string    `gorm:"column:recalled"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	FinishedAt     *time.Time `gorm:"column:finished_at"`
}

// TableName : Names the table this row maps to.
func (chatRow) TableName() string { return "chats" }

// summaryRow : The columns of chats that a listing needs, which is every one
// except the response.
type summaryRow struct {
	ID             string     `gorm:"column:id"`
	ConversationID *string    `gorm:"column:conversation_id"`
	Prompt         string     `gorm:"column:prompt"`
	Channel        string     `gorm:"column:channel"`
	Status         string     `gorm:"column:status"`
	Error          *string    `gorm:"column:error"`
	ErrorCode      string     `gorm:"column:error_code"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	FinishedAt     *time.Time `gorm:"column:finished_at"`
}

// summaryColumns : The columns a listing selects. Naming them is what keeps
// response bodies out of a query that does not need them.
var summaryColumns = []string{
	"id", "conversation_id", "prompt", "channel", "status", "error", "error_code",
	"created_at", "updated_at", "started_at", "finished_at",
}

// conversationRow : The conversations table, as GORM sees it.
type conversationRow struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     *string    `gorm:"column:user_id"`
	Title      string     `gorm:"column:title"`
	CreatedAt  time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt *time.Time `gorm:"column:archived_at"`
}

// userRow : The users table, as GORM sees it.
type userRow struct {
	ID           string    `gorm:"column:id;primaryKey"`
	Username     string    `gorm:"column:username"`
	PasswordHash string    `gorm:"column:password_hash"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

// TableName : Names the table this row maps to.
func (userRow) TableName() string { return "users" }

// toUser : Converts a stored row back into a user.
func (r *userRow) toUser() *chat.User {
	return &chat.User{
		ID:           r.ID,
		Username:     r.Username,
		PasswordHash: r.PasswordHash,
		CreatedAt:    r.CreatedAt.UTC(),
		UpdatedAt:    r.UpdatedAt.UTC(),
	}
}

// clientRow : The clients table, as GORM sees it.
type clientRow struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	UserID               *string    `gorm:"column:user_id"`
	Name                 string     `gorm:"column:name"`
	Channel              string     `gorm:"column:channel"`
	Vendor               *string    `gorm:"column:vendor"`
	Model                *string    `gorm:"column:model"`
	TokenHash            *string    `gorm:"column:token_hash"`
	ActiveConversationID *string    `gorm:"column:active_conversation_id"`
	RevokedAt            *time.Time `gorm:"column:revoked_at"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
}

// TableName : Names the table this row maps to.
func (clientRow) TableName() string { return "clients" }

// toClient : Converts a stored row back into a client.
func (r *clientRow) toClient() *chat.Client {
	return &chat.Client{
		ID:                   r.ID,
		UserID:               value(r.UserID),
		Name:                 r.Name,
		Channel:              chat.Channel(r.Channel),
		Model:                chat.NewModel(value(r.Vendor), value(r.Model)),
		TokenHash:            value(r.TokenHash),
		ActiveConversationID: value(r.ActiveConversationID),
		RevokedAt:            utc(r.RevokedAt),
		CreatedAt:            r.CreatedAt.UTC(),
		UpdatedAt:            r.UpdatedAt.UTC(),
	}
}

// TableName : Names the table this row maps to.
func (conversationRow) TableName() string { return "conversations" }

// toConversation : Converts a stored row back into a conversation.
func (r *conversationRow) toConversation() chat.Conversation {
	return chat.Conversation{
		ID:         r.ID,
		UserID:     value(r.UserID),
		Title:      r.Title,
		CreatedAt:  r.CreatedAt.UTC(),
		UpdatedAt:  r.UpdatedAt.UTC(),
		ArchivedAt: utc(r.ArchivedAt),
	}
}

// toRow : Converts a chat into the row that stores it.
//
// An empty response or error becomes NULL rather than an empty string, so
// that "produced nothing" and "not finished" read the same way in the table.
func toRow(t *chat.Chat) *chatRow {
	return &chatRow{
		ID:             t.ID,
		ConversationID: nullable(t.ConversationID),
		Prompt:         t.Prompt,
		ClientID:       nullable(t.ClientID),
		Channel:        string(t.Channel),
		Vendor:         nullable(t.Model.Vendor),
		Model:          nullable(t.Model.ID),
		Status:         string(t.Status),
		Response:       nullable(t.Response),
		Error:          nullable(t.Error),
		ErrorCode:      t.ErrorCode,
		ErrorDetail:    nullable(t.ErrorDetail),
		Recalled:       fromRecalled(t.Recalled),
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
	}
}

// toChat : Converts a stored row back into a chat.
func (r *chatRow) toChat() *chat.Chat {
	return &chat.Chat{
		ID:             r.ID,
		ConversationID: value(r.ConversationID),
		Prompt:         r.Prompt,
		ClientID:       value(r.ClientID),
		Channel:        chat.Channel(r.Channel),
		Model:          chat.NewModel(value(r.Vendor), value(r.Model)),
		Status:         chat.Status(r.Status),
		Response:       value(r.Response),
		Error:          value(r.Error),
		ErrorCode:      r.ErrorCode,
		ErrorDetail:    value(r.ErrorDetail),
		Recalled:       toRecalled(r.Recalled),
		CreatedAt:      r.CreatedAt.UTC(),
		UpdatedAt:      r.UpdatedAt.UTC(),
		StartedAt:      utc(r.StartedAt),
		FinishedAt:     utc(r.FinishedAt),
	}
}

// toSummary : Converts a listing row into a summary.
func (r *summaryRow) toSummary() chat.Summary {
	return chat.Summary{
		ID:             r.ID,
		ConversationID: value(r.ConversationID),
		Prompt:         r.Prompt,
		Channel:        chat.Channel(r.Channel),
		Status:         chat.Status(r.Status),
		Error:          value(r.Error),
		ErrorCode:      r.ErrorCode,
		CreatedAt:      r.CreatedAt.UTC(),
		UpdatedAt:      r.UpdatedAt.UTC(),
		StartedAt:      utc(r.StartedAt),
		FinishedAt:     utc(r.FinishedAt),
	}
}

// nullable : Returns nil for an empty string, so it is stored as NULL.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// value : Returns the empty string for a NULL column.
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// utc : Returns t in UTC, or nil if it is nil.
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// settingRow : The settings table, as GORM sees it.
type settingRow struct {
	Name      string    `gorm:"column:name;primaryKey"`
	Value     string    `gorm:"column:value"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

// TableName : Names the table this row maps to.
func (settingRow) TableName() string { return "settings" }

// fromRecalled : What was recalled, as the JSON column holds it.
func fromRecalled(r *chat.Recalled) *string {
	if r == nil || r.Empty() {
		return nil
	}
	raw, err := json.Marshal(r)
	if err != nil {
		// Only a type that cannot be encoded reaches this, which the tests
		// would have caught. Losing the timeline is not worth losing the chat.
		return nil
	}
	out := string(raw)
	return &out
}

// toRecalled : What the JSON column holds, as a Recalled.
//
// Unreadable JSON is treated as nothing recalled. It is a record of how an
// answer was made, and a chat is still a chat without it.
func toRecalled(raw *string) *chat.Recalled {
	if raw == nil || *raw == "" {
		return nil
	}
	var out chat.Recalled
	if err := json.Unmarshal([]byte(*raw), &out); err != nil {
		return nil
	}
	return &out
}
