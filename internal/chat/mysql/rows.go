package mysql

import (
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
	ID          string     `gorm:"column:id;primaryKey"`
	SessionID   *string    `gorm:"column:session_id"`
	Prompt      string     `gorm:"column:prompt"`
	Channel     string     `gorm:"column:channel"`
	Vendor      *string    `gorm:"column:vendor"`
	Model       *string    `gorm:"column:model"`
	Status      string     `gorm:"column:status"`
	Response    *string    `gorm:"column:response"`
	Error       *string    `gorm:"column:error"`
	ErrorCode   string     `gorm:"column:error_code"`
	ErrorDetail *string    `gorm:"column:error_detail"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	StartedAt   *time.Time `gorm:"column:started_at"`
	FinishedAt  *time.Time `gorm:"column:finished_at"`
}

// TableName : Names the table this row maps to.
func (chatRow) TableName() string { return "chats" }

// summaryRow : The columns of chats that a listing needs, which is every one
// except the response.
type summaryRow struct {
	ID         string     `gorm:"column:id"`
	SessionID  *string    `gorm:"column:session_id"`
	Prompt     string     `gorm:"column:prompt"`
	Channel    string     `gorm:"column:channel"`
	Status     string     `gorm:"column:status"`
	Error      *string    `gorm:"column:error"`
	ErrorCode  string     `gorm:"column:error_code"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at"`
	StartedAt  *time.Time `gorm:"column:started_at"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
}

// summaryColumns : The columns a listing selects. Naming them is what keeps
// response bodies out of a query that does not need them.
var summaryColumns = []string{
	"id", "session_id", "prompt", "channel", "status", "error", "error_code",
	"created_at", "updated_at", "started_at", "finished_at",
}

// sessionRow : The sessions table, as GORM sees it.
type sessionRow struct {
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
	ID              string     `gorm:"column:id;primaryKey"`
	UserID          *string    `gorm:"column:user_id"`
	Name            string     `gorm:"column:name"`
	Channel         string     `gorm:"column:channel"`
	Vendor          *string    `gorm:"column:vendor"`
	Model           *string    `gorm:"column:model"`
	TokenHash       *string    `gorm:"column:token_hash"`
	ActiveSessionID *string    `gorm:"column:active_session_id"`
	RevokedAt       *time.Time `gorm:"column:revoked_at"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
}

// TableName : Names the table this row maps to.
func (clientRow) TableName() string { return "clients" }

// toClient : Converts a stored row back into a client.
func (r *clientRow) toClient() *chat.Client {
	return &chat.Client{
		ID:              r.ID,
		UserID:          value(r.UserID),
		Name:            r.Name,
		Channel:         chat.Channel(r.Channel),
		Model:           chat.NewModel(value(r.Vendor), value(r.Model)),
		TokenHash:       value(r.TokenHash),
		ActiveSessionID: value(r.ActiveSessionID),
		RevokedAt:       utc(r.RevokedAt),
		CreatedAt:       r.CreatedAt.UTC(),
		UpdatedAt:       r.UpdatedAt.UTC(),
	}
}

// TableName : Names the table this row maps to.
func (sessionRow) TableName() string { return "sessions" }

// toSession : Converts a stored row back into a session.
func (r *sessionRow) toSession() chat.Session {
	return chat.Session{
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
		ID:          t.ID,
		SessionID:   nullable(t.SessionID),
		Prompt:      t.Prompt,
		Channel:     string(t.Channel),
		Vendor:      nullable(t.Model.Vendor),
		Model:       nullable(t.Model.ID),
		Status:      string(t.Status),
		Response:    nullable(t.Response),
		Error:       nullable(t.Error),
		ErrorCode:   t.ErrorCode,
		ErrorDetail: nullable(t.ErrorDetail),
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		StartedAt:   t.StartedAt,
		FinishedAt:  t.FinishedAt,
	}
}

// toChat : Converts a stored row back into a chat.
func (r *chatRow) toChat() *chat.Chat {
	return &chat.Chat{
		ID:          r.ID,
		SessionID:   value(r.SessionID),
		Prompt:      r.Prompt,
		Channel:     chat.Channel(r.Channel),
		Model:       chat.NewModel(value(r.Vendor), value(r.Model)),
		Status:      chat.Status(r.Status),
		Response:    value(r.Response),
		Error:       value(r.Error),
		ErrorCode:   r.ErrorCode,
		ErrorDetail: value(r.ErrorDetail),
		CreatedAt:   r.CreatedAt.UTC(),
		UpdatedAt:   r.UpdatedAt.UTC(),
		StartedAt:   utc(r.StartedAt),
		FinishedAt:  utc(r.FinishedAt),
	}
}

// toSummary : Converts a listing row into a summary.
func (r *summaryRow) toSummary() chat.Summary {
	return chat.Summary{
		ID:         r.ID,
		SessionID:  value(r.SessionID),
		Prompt:     r.Prompt,
		Channel:    chat.Channel(r.Channel),
		Status:     chat.Status(r.Status),
		Error:      value(r.Error),
		ErrorCode:  r.ErrorCode,
		CreatedAt:  r.CreatedAt.UTC(),
		UpdatedAt:  r.UpdatedAt.UTC(),
		StartedAt:  utc(r.StartedAt),
		FinishedAt: utc(r.FinishedAt),
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
