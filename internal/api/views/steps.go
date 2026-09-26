package views

import (
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Step kinds, in the order they can happen.
const (
	// StepAsked : What the person said.
	StepAsked = "asked"
	// StepRecalled : What memory put in front of the model.
	StepRecalled = "recalled"
	// StepToolCall : A tool the model asked for.
	StepToolCall = "tool_call"
	// StepToolResult : What that tool returned.
	StepToolResult = "tool_result"
	// StepAnswered : The reply.
	StepAnswered = "answered"
	// StepFailed : Why there was no reply.
	StepFailed = "failed"
)

// Timeline : How one answer was made.
type Timeline struct {
	ChatID         string     `json:"chat_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Status         string     `json:"status"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	// TookMS : How long the whole answer took, once it had started.
	TookMS int64 `json:"took_ms,omitempty"`
	// Complete : Whether the whole timeline is here. False for a chat from
	// before messages recorded which turn wrote them, whose steps cannot be
	// told from another turn's.
	Complete bool   `json:"complete"`
	Steps    []Step `json:"steps"`
}

// Step : One thing that happened while the answer was made.
type Step struct {
	Kind string     `json:"kind"`
	At   *time.Time `json:"at,omitempty"`
	// OffsetMS : Milliseconds after the answer started, where that is known.
	OffsetMS *int64 `json:"offset_ms,omitempty"`
	// Text : What was said, for asked, answered and failed.
	Text string `json:"text,omitempty"`
	// Detail : What actually went wrong, for failed.
	Detail string `json:"detail,omitempty"`
	// Name : Which tool, for a call and its result.
	Name string `json:"name,omitempty"`
	// Arguments : What the model asked for, as it wrote them.
	Arguments string `json:"arguments,omitempty"`
	// Outcome : ok, failed or partial.
	Outcome string `json:"outcome,omitempty"`
	// Content : What the tool returned, or exactly what went wrong.
	Content string `json:"content,omitempty"`
	// TookMS : How long this step took, where that is known.
	TookMS int64 `json:"took_ms,omitempty"`
	// Recalled : What memory offered, for the recalled step.
	Recalled *chat.Recalled `json:"recalled,omitempty"`
	// ByWords : Whether memory matched wording rather than meaning.
	ByWords bool `json:"by_words,omitempty"`
}

// OfTimeline : The timeline of one chat, from the chat and what it wrote.
//
// Messages are the record of what happened and are trusted for the order.
// The chat supplies what has no message of its own: what was recalled,
// which is not something anybody said, and the failure, which is recorded
// on the chat rather than always in the conversation.
func OfTimeline(t *chat.Chat, wrote []conversation.Message) Timeline {
	out := Timeline{
		ChatID:         t.ID,
		ConversationID: t.ConversationID,
		Status:         string(t.Status),
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
		Complete:       len(wrote) > 0 || t.ConversationID == "",
		Steps:          []Step{},
	}
	if t.StartedAt != nil && t.FinishedAt != nil {
		out.TookMS = t.FinishedAt.Sub(*t.StartedAt).Milliseconds()
	}

	at := func(when time.Time) (*time.Time, *int64) {
		w := when
		if t.StartedAt == nil {
			return &w, nil
		}
		ms := when.Sub(*t.StartedAt).Milliseconds()
		return &w, &ms
	}

	// What was recalled happens before anything is written down, so it is
	// placed first whatever the messages say.
	if t.Recalled != nil && !t.Recalled.Empty() {
		step := Step{
			Kind:     StepRecalled,
			Recalled: t.Recalled,
			TookMS:   t.Recalled.TookMS,
			ByWords:  t.Recalled.ByWords,
		}
		if t.StartedAt != nil {
			step.At = t.StartedAt
		}
		out.Steps = append(out.Steps, step)
	}

	for _, m := range wrote {
		when, offset := at(m.At)

		switch {
		case len(m.ToolCalls) > 0:
			for _, c := range m.ToolCalls {
				out.Steps = append(out.Steps, Step{
					Kind: StepToolCall, At: when, OffsetMS: offset,
					Name: c.Name, Arguments: c.Arguments,
				})
			}

		case len(m.ToolResults) > 0:
			for _, res := range m.ToolResults {
				out.Steps = append(out.Steps, Step{
					Kind: StepToolResult, At: when, OffsetMS: offset,
					Name: res.Name, Outcome: string(res.Outcome),
					Content: res.Content, TookMS: res.TookMS,
				})
			}

		case m.Role == conversation.User:
			// The question goes first, ahead of the recall it caused.
			out.Steps = append([]Step{{
				Kind: StepAsked, At: when, OffsetMS: offset, Text: m.Content,
			}}, out.Steps...)

		case m.Kind == conversation.Failure:
			out.Steps = append(out.Steps, Step{
				Kind: StepFailed, At: when, OffsetMS: offset,
				Text: m.Content, Detail: m.Detail,
			})

		default:
			out.Steps = append(out.Steps, Step{
				Kind: StepAnswered, At: when, OffsetMS: offset, Text: m.Content,
			})
		}
	}

	return out
}
