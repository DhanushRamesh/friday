// Package views : Renders domain values as the API publishes them.
//
// Every type here is a wire format, deliberately separate from the domain so
// that the two can change independently and so that a field added for
// The server's own use is never published by accident. The rendering lives in one
// package rather than beside each handler because a session detail carries
// chats, a login carries a user and a client, and those shapes must agree
// wherever they appear.
package views

import (
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// Chat : A chat as the API returns it.
type Chat struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	Prompt    string `json:"prompt"`
	// Channel : How the prompt arrived, "voice" or "direct".
	Channel  string `json:"channel,omitempty"`
	Status   string `json:"status"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
	// ErrorCode : Which kind of failure it was. A client keys off this rather
	// than matching on the sentence, which is prose and will change.
	ErrorCode string `json:"error_code,omitempty"`
	// ErrorDetail : Exactly what the service said. Shown behind "more info",
	// never in place of Error.
	ErrorDetail string     `json:"error_detail,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// OfChat : Renders a chat for the API.
func OfChat(t *chat.Chat) Chat {
	return Chat{
		ID:          t.ID,
		SessionID:   t.SessionID,
		Prompt:      t.Prompt,
		Channel:     string(t.Channel),
		Status:      string(t.Status),
		Response:    t.Response,
		Error:       t.Error,
		ErrorCode:   t.ErrorCode,
		ErrorDetail: t.ErrorDetail,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		StartedAt:   t.StartedAt,
		FinishedAt:  t.FinishedAt,
	}
}

// Summary : A chat in a listing, which carries no response body.
type Summary struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id,omitempty"`
	Prompt     string     `json:"prompt"`
	Channel    string     `json:"channel,omitempty"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	ErrorCode  string     `json:"error_code,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// OfSummary : Renders a chat summary for the API.
func OfSummary(s chat.Summary) Summary {
	return Summary{
		ID:         s.ID,
		SessionID:  s.SessionID,
		Prompt:     s.Prompt,
		Channel:    string(s.Channel),
		Status:     string(s.Status),
		Error:      s.Error,
		ErrorCode:  s.ErrorCode,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
		StartedAt:  s.StartedAt,
		FinishedAt: s.FinishedAt,
	}
}

// OfSummaries : Renders a listing of chat summaries.
func OfSummaries(summaries []chat.Summary) []Summary {
	out := make([]Summary, len(summaries))
	for i, s := range summaries {
		out[i] = OfSummary(s)
	}
	return out
}

// Message : One message recorded during a chat's run.
type Message struct {
	Seq       int       `json:"seq"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// OfMessages : Renders a chat's messages for the API.
func OfMessages(messages []chat.Message) []Message {
	out := make([]Message, len(messages))
	for i, m := range messages {
		out[i] = Message{
			Seq:       m.Seq,
			Kind:      m.Kind,
			Text:      m.Text,
			CreatedAt: m.CreatedAt,
		}
	}
	return out
}

// User : A user as the API returns it. The password hash is never included.
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// OfUser : Renders a user for the API.
func OfUser(u *chat.User) User {
	return User{ID: u.ID, Username: u.Username, CreatedAt: u.CreatedAt}
}

// Client : A client as the API returns it.
type Client struct {
	ID              string     `json:"id"`
	Name            string     `json:"name,omitempty"`
	Current         bool       `json:"current"`
	Revoked         bool       `json:"revoked"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	ActiveSessionID string     `json:"active_session_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// OfClient : Renders a client for the API. Current marks the one the request
// was made from.
func OfClient(d chat.Client, current bool) Client {
	return Client{
		ID:              d.ID,
		Name:            d.Name,
		Current:         current,
		Revoked:         d.Revoked(),
		RevokedAt:       d.RevokedAt,
		ActiveSessionID: d.ActiveSessionID,
		CreatedAt:       d.CreatedAt,
	}
}

// Session : A session as the API returns it.
type Session struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Active   bool   `json:"active"`
	Archived bool   `json:"archived,omitempty"`
	// ArchivedAt : When it was put away. Absent while it is in use.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// OfSession : Renders a session for the API.
func OfSession(c chat.Session, active bool) Session {
	return Session{
		ID:         c.ID,
		Title:      c.Title,
		Active:     active,
		Archived:   c.Archived(),
		ArchivedAt: c.ArchivedAt,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
}
