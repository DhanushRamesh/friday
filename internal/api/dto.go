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

// conversationView : A conversation as the API returns it.
type conversationView struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// viewOfConversation : Renders a conversation for the API.
func viewOfConversation(c task.Conversation) conversationView {
	return conversationView{ID: c.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
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
