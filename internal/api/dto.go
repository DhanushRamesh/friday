package api

import (
	"time"

	"github.com/DhanushRamesh/friday/internal/task"
)

// createTaskRequest : The body of a request to create a task.
type createTaskRequest struct {
	// Prompt : What the user asked for.
	Prompt string `json:"prompt"`
	// ConversationID : The exchange to continue. Empty starts a new one.
	ConversationID string `json:"conversation_id,omitempty"`
}

// taskView : A task as the API returns it.
//
// It is kept separate from task.Task so that the wire format can change
// without disturbing the domain, and so that fields added for FRIDAY's own
// use are not published by accident.
type taskView struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Prompt         string     `json:"prompt"`
	Status         string     `json:"status"`
	Response       string     `json:"response,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// viewOf : Renders a task for the API.
func viewOf(t *task.Task) taskView {
	return taskView{
		ID:             t.ID,
		ConversationID: t.ConversationID,
		Prompt:         t.Prompt,
		Status:         string(t.Status),
		Response:       t.Response,
		Error:          t.Error,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
	}
}

// summaryView : A task in a listing, which carries no response body.
type summaryView struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Prompt         string     `json:"prompt"`
	Status         string     `json:"status"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// viewOfSummary : Renders a task summary for the API.
func viewOfSummary(s task.Summary) summaryView {
	return summaryView{
		ID:             s.ID,
		ConversationID: s.ConversationID,
		Prompt:         s.Prompt,
		Status:         string(s.Status),
		Error:          s.Error,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		StartedAt:      s.StartedAt,
		FinishedAt:     s.FinishedAt,
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
	// DeviceName : What to call the device being logged in from, such as
	// "my phone". Optional.
	DeviceName string `json:"device_name,omitempty"`
}

// loginResponse : What logging in returns.
//
// The token appears here and nowhere else: only its hash is stored, so this
// response is the one chance to keep it.
type loginResponse struct {
	Token  string     `json:"token"`
	User   userView   `json:"user"`
	Device deviceView `json:"device"`
}

// meResponse : Who is calling and from what.
type meResponse struct {
	User   userView   `json:"user"`
	Device deviceView `json:"device"`
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

// deviceView : A device as the API returns it.
type deviceView struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name,omitempty"`
	Current              bool       `json:"current"`
	Revoked              bool       `json:"revoked"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	ActiveConversationID string     `json:"active_conversation_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

// viewOfDevice : Renders a device for the API. Current marks the one the
// request was made from.
func viewOfDevice(d task.Device, current bool) deviceView {
	return deviceView{
		ID:                   d.ID,
		Name:                 d.Name,
		Current:              current,
		Revoked:              d.Revoked(),
		RevokedAt:            d.RevokedAt,
		ActiveConversationID: d.ActiveConversationID,
		CreatedAt:            d.CreatedAt,
	}
}

// listDevicesResponse : The body of a listing of devices.
type listDevicesResponse struct {
	Devices []deviceView `json:"devices"`
}

// createConversationRequest : The body of a request to start a conversation.
type createConversationRequest struct {
	// Title : What to call it in a listing. Optional.
	Title string `json:"title,omitempty"`
	// Activate : Whether this device switches to it. Defaults to true.
	Activate bool `json:"activate,omitempty"`
}

// conversationView : A conversation as the API returns it.
type conversationView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// viewOfConversation : Renders a conversation for the API.
func viewOfConversation(c task.Conversation, active bool) conversationView {
	return conversationView{
		ID:        c.ID,
		Title:     c.Title,
		Active:    active,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// listConversationsResponse : The body of a listing of conversations.
type listConversationsResponse struct {
	Conversations []conversationView `json:"conversations"`
}

// conversationDetailResponse : A conversation together with its tasks.
type conversationDetailResponse struct {
	Conversation conversationView `json:"conversation"`
	Tasks        []summaryView    `json:"tasks"`
}

// errorResponse : The body returned with every failing status code.
type errorResponse struct {
	// Error : What went wrong, phrased for whoever reads it. Internal detail
	// stays in the log.
	Error string `json:"error"`
}
