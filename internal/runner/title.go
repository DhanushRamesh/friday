package runner

import (
	"context"
	"log/slog"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
)

// title : Gives a conversation a name, once, from what has been said in it.
//
// Runs after the answer has been recorded and announced, like condensing,
// because nobody is waiting for it. Every failure is logged and dropped: a
// conversation without a name is exactly what every conversation was before
// this existed.
func (r *Runner) title(ctx context.Context, t *chat.Chat) {
	if t.ConversationID == "" || r.repo == nil {
		return
	}

	// Only once, and never over a name somebody chose. A conversation renamed
	// by hand keeps that name however much is said in it afterwards.
	c, err := r.repo.GetConversation(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read the conversation to name it",
			slog.Any("error", err))
		return
	}
	if c.Title != "" {
		return
	}

	said, err := r.messages.All(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read the conversation to name it",
			slog.Any("error", err))
		return
	}

	// One exchange is the least that can be named. Naming a conversation from
	// the question alone produces a label for a subject nobody has answered
	// yet, which is often not what it turns out to be about.
	if len(conversation.ForModel(said)) < 2 {
		return
	}

	askCtx, cancel := context.WithTimeout(ctx, r.condenseTimeout)
	defer cancel()

	answer, err := r.ask(askCtx, t.Model, environment.PurposeTitle, conversation.TitlePrompt(said))
	if err != nil {
		r.logger.WarnContext(ctx, "cannot name the conversation",
			slog.String("conversation_id", t.ConversationID), slog.Any("error", err))
		return
	}

	// A model asked for a bare label will sometimes answer with a sentence.
	// No name is better than a bad one, and it will not be asked again.
	name := conversation.CleanTitle(answer)
	if name == "" {
		r.logger.InfoContext(ctx, "the model did not offer a usable name",
			slog.String("conversation_id", t.ConversationID),
			slog.String("answer", answer))
		return
	}

	if err := r.repo.RenameConversation(ctx, c.UserID, t.ConversationID, name); err != nil {
		r.logger.ErrorContext(ctx, "cannot store the conversation's name",
			slog.String("conversation_id", t.ConversationID), slog.Any("error", err))
		return
	}

	r.logger.InfoContext(ctx, "conversation named",
		slog.String("conversation_id", t.ConversationID), slog.String("title", name))

	r.announceTitle(ctx, t, name)
}

// announceTitle : Says the new name aloud, when the person had no way to see
// it.
//
// Only for a chat that arrived by voice. Anywhere else the name appears in
// the listing the moment it is set, and saying it would be noise.
func (r *Runner) announceTitle(ctx context.Context, t *chat.Chat, name string) {
	if t.Channel != chat.ChannelVoice || r.announcer == nil || !r.announcer.Available() {
		return
	}

	if err := r.announcer.Say(ctx, conversation.TitleAnnouncement(name)); err != nil {
		r.logger.WarnContext(ctx, "cannot announce the conversation's name",
			slog.Any("error", err))
	}
}
