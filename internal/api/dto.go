package api

import (
	"time"

	"github.com/DhanushRamesh/friday/internal/task"
)

// createTaskRequest : The body of a request to create a task.
type createTaskRequest struct {
	// Prompt : What the user asked for.
	Prompt string `json:"prompt"`
	// SessionID : The exchange to continue. Empty starts a new one.
	SessionID string `json:"session_id,omitempty"`
}

// taskView : A task as the API returns it.
//
// It is kept separate from task.Task so that the wire format can change
// without disturbing the domain, and so that fields added for FRIDAY's own
// use are not published by accident.
type taskView struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id,omitempty"`
	Prompt     string     `json:"prompt"`
	Status     string     `json:"status"`
	Response   string     `json:"response,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// viewOf : Renders a task for the API.
func viewOf(t *task.Task) taskView {
	return taskView{
		ID:         t.ID,
		SessionID:  t.SessionID,
		Prompt:     t.Prompt,
		Status:     string(t.Status),
		Response:   t.Response,
		Error:      t.Error,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}

// summaryView : A task in a listing, which carries no response body.
type summaryView struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id,omitempty"`
	Prompt     string     `json:"prompt"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// viewOfSummary : Renders a task summary for the API.
func viewOfSummary(s task.Summary) summaryView {
	return summaryView{
		ID:         s.ID,
		SessionID:  s.SessionID,
		Prompt:     s.Prompt,
		Status:     string(s.Status),
		Error:      s.Error,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
		StartedAt:  s.StartedAt,
		FinishedAt: s.FinishedAt,
	}
}

// listTasksResponse : The body of a listing of tasks.
type listTasksResponse struct {
	Tasks []summaryView `json:"tasks"`
}

// messageView : One message recorded during a task's run.
type messageView struct {
	Seq       int       `json:"seq"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// viewOfMessages : Renders a task's messages for the API.
func viewOfMessages(messages []task.Message) []messageView {
	out := make([]messageView, len(messages))
	for i, m := range messages {
		out[i] = messageView{
			Seq:       m.Seq,
			Kind:      m.Kind,
			Text:      m.Text,
			CreatedAt: m.CreatedAt,
		}
	}
	return out
}

// loginRequest : The body of a request to log in.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// ClientName : What to call the client being logged in from, such as
	// "my phone". Optional.
	ClientName string `json:"client_name,omitempty"`
}

// loginResponse : What logging in returns.
//
// The token appears here and nowhere else: only its hash is stored, so this
// response is the one chance to keep it.
type loginResponse struct {
	Token  string     `json:"token"`
	User   userView   `json:"user"`
	Client clientView `json:"client"`
}

// meResponse : Who is calling and from what.
type meResponse struct {
	User   userView   `json:"user"`
	Client clientView `json:"client"`
}

// userView : A user as the API returns it. The password hash is never
// included.
type userView struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// viewOfUser : Renders a user for the API.
func viewOfUser(u *task.User) userView {
	return userView{ID: u.ID, Username: u.Username, CreatedAt: u.CreatedAt}
}

// clientView : A client as the API returns it.
type clientView struct {
	ID              string     `json:"id"`
	Name            string     `json:"name,omitempty"`
	Current         bool       `json:"current"`
	Revoked         bool       `json:"revoked"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	ActiveSessionID string     `json:"active_session_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// viewOfClient : Renders a client for the API. Current marks the one the
// request was made from.
func viewOfClient(d task.Client, current bool) clientView {
	return clientView{
		ID:              d.ID,
		Name:            d.Name,
		Current:         current,
		Revoked:         d.Revoked(),
		RevokedAt:       d.RevokedAt,
		ActiveSessionID: d.ActiveSessionID,
		CreatedAt:       d.CreatedAt,
	}
}

// listClientsResponse : The body of a listing of clients.
type listClientsResponse struct {
	Clients []clientView `json:"clients"`
}

// createSessionRequest : The body of a request to start a session.
type createSessionRequest struct {
	// Title : What to call it in a listing. Optional.
	Title string `json:"title,omitempty"`
	// Activate : Whether this client switches to it. Defaults to true.
	Activate bool `json:"activate,omitempty"`
}

// sessionView : A session as the API returns it.
type sessionView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// viewOfSession : Renders a session for the API.
func viewOfSession(c task.Session, active bool) sessionView {
	return sessionView{
		ID:        c.ID,
		Title:     c.Title,
		Active:    active,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// listSessionsResponse : The body of a listing of sessions.
type listSessionsResponse struct {
	Sessions []sessionView `json:"sessions"`
}

// sessionDetailResponse : A session together with its tasks.
type sessionDetailResponse struct {
	Session sessionView   `json:"session"`
	Tasks   []summaryView `json:"tasks"`
}

// errorResponse : The body returned with every failing status code.
type errorResponse struct {
	// Error : What went wrong, phrased for whoever reads it. Internal detail
	// stays in the log.
	Error string `json:"error"`
}
