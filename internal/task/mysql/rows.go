package mysql

import (
	"time"

	"github.com/DhanushRamesh/friday/internal/task"
)

// taskRow : The tasks table, as GORM sees it.
//
// It is kept separate from task.Task so that the domain type carries no
// persistence tags and no knowledge of how it is stored.
//
// CreatedAt and UpdatedAt disable GORM's automatic timestamps. GORM would
// otherwise overwrite them on every write, discarding the times the domain
// recorded and making a task's own history disagree with the row.
type taskRow struct {
	ID             string     `gorm:"column:id;primaryKey"`
	ConversationID *string    `gorm:"column:conversation_id"`
	Prompt         string     `gorm:"column:prompt"`
	Status         string     `gorm:"column:status"`
	Response       *string    `gorm:"column:response"`
	Error          *string    `gorm:"column:error"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	FinishedAt     *time.Time `gorm:"column:finished_at"`
}

// TableName : Names the table this row maps to.
func (taskRow) TableName() string { return "tasks" }

// summaryRow : The columns of tasks that a listing needs, which is every one
// except the response.
type summaryRow struct {
	ID             string     `gorm:"column:id"`
	ConversationID *string    `gorm:"column:conversation_id"`
	Prompt         string     `gorm:"column:prompt"`
	Status         string     `gorm:"column:status"`
	Error          *string    `gorm:"column:error"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	FinishedAt     *time.Time `gorm:"column:finished_at"`
}

// summaryColumns : The columns a listing selects. Naming them is what keeps
// response bodies out of a query that does not need them.
var summaryColumns = []string{
	"id", "conversation_id", "prompt", "status", "error",
	"created_at", "updated_at", "started_at", "finished_at",
}

// conversationRow : The conversations table, as GORM sees it.
type conversationRow struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    *string   `gorm:"column:user_id"`
	Title     string    `gorm:"column:title"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
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
func (r *userRow) toUser() *task.User {
	return &task.User{
		ID:           r.ID,
		Username:     r.Username,
		PasswordHash: r.PasswordHash,
		CreatedAt:    r.CreatedAt.UTC(),
		UpdatedAt:    r.UpdatedAt.UTC(),
	}
}

// deviceRow : The devices table, as GORM sees it.
type deviceRow struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	UserID               *string    `gorm:"column:user_id"`
	Name                 string     `gorm:"column:name"`
	TokenHash            *string    `gorm:"column:token_hash"`
	ActiveConversationID *string    `gorm:"column:active_conversation_id"`
	RevokedAt            *time.Time `gorm:"column:revoked_at"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
}

// TableName : Names the table this row maps to.
func (deviceRow) TableName() string { return "devices" }

// toDevice : Converts a stored row back into a device.
func (r *deviceRow) toDevice() *task.Device {
	return &task.Device{
		ID:                   r.ID,
		UserID:               value(r.UserID),
		Name:                 r.Name,
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
func (r *conversationRow) toConversation() task.Conversation {
	return task.Conversation{
		ID:        r.ID,
		UserID:    value(r.UserID),
		Title:     r.Title,
		CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// messageRow : The task_messages table, as GORM sees it.
type messageRow struct {
	TaskID    string    `gorm:"column:task_id;primaryKey"`
	Seq       int       `gorm:"column:seq;primaryKey"`
	Kind      string    `gorm:"column:kind"`
	Text      string    `gorm:"column:text"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
}

// TableName : Names the table this row maps to.
func (messageRow) TableName() string { return "task_messages" }

// toRow : Converts a task into the row that stores it.
//
// An empty response or error becomes NULL rather than an empty string, so
// that "produced nothing" and "not finished" read the same way in the table.
func toRow(t *task.Task) *taskRow {
	return &taskRow{
		ID:             t.ID,
		ConversationID: nullable(t.ConversationID),
		Prompt:         t.Prompt,
		Status:         string(t.Status),
		Response:       nullable(t.Response),
		Error:          nullable(t.Error),
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
	}
}

// toTask : Converts a stored row back into a task.
func (r *taskRow) toTask() *task.Task {
	return &task.Task{
		ID:             r.ID,
		ConversationID: value(r.ConversationID),
		Prompt:         r.Prompt,
		Status:         task.Status(r.Status),
		Response:       value(r.Response),
		Error:          value(r.Error),
		CreatedAt:      r.CreatedAt.UTC(),
		UpdatedAt:      r.UpdatedAt.UTC(),
		StartedAt:      utc(r.StartedAt),
		FinishedAt:     utc(r.FinishedAt),
	}
}

// toSummary : Converts a listing row into a summary.
func (r *summaryRow) toSummary() task.Summary {
	return task.Summary{
		ID:             r.ID,
		ConversationID: value(r.ConversationID),
		Prompt:         r.Prompt,
		Status:         task.Status(r.Status),
		Error:          value(r.Error),
		CreatedAt:      r.CreatedAt.UTC(),
		UpdatedAt:      r.UpdatedAt.UTC(),
		StartedAt:      utc(r.StartedAt),
		FinishedAt:     utc(r.FinishedAt),
	}
}

// toMessage : Converts a stored row into a message.
func (r *messageRow) toMessage() task.Message {
	return task.Message{
		TaskID:    r.TaskID,
		Seq:       r.Seq,
		Kind:      r.Kind,
		Text:      r.Text,
		CreatedAt: r.CreatedAt.UTC(),
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
