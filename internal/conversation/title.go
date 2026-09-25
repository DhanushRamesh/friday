package conversation

import (
	"strings"
	"unicode"
)

// MaxTitleWords : The longest a name may be before it stops being a label and
// starts being a summary.
const MaxTitleWords = 4

// titleInstruction : What the model is told when asked to name a conversation.
//
// Written to be read by the model that also answers as the assistant, which
// will otherwise reply in its own manner and address the person. It is asked
// for a label, and told plainly that nothing else is wanted.
const titleInstruction = `Name this conversation.

Give a short label of two to four words describing what it is about, taken
from what was actually said. Not a summary, not a sentence, not a question.

Reply with the label alone. No quotation marks, no full stop, no preamble, no
explanation, and do not address anyone. If the exchange is too slight to name,
reply with nothing at all.

The exchange:
`

// TitlePrompt : The instruction that asks a model to name a conversation from
// what has been said in it.
func TitlePrompt(messages []Message) string {
	var b strings.Builder
	b.WriteString(titleInstruction)

	for _, m := range ForModel(messages) {
		b.WriteString(speaker(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	return b.String()
}

// CleanTitle : What a model's answer is worth keeping as a title, or empty
// when it is not worth keeping at all.
//
// A model asked for a bare label will sometimes wrap it in quotes, end it with
// a full stop, or preface it with "Title:". Taking that literally puts the
// punctuation in the sidebar for ever, so it is stripped here rather than
// asked for again. An answer that arrives as a sentence is refused: a
// conversation with no name reads better than one named with an apology.
func CleanTitle(answer string) string {
	title := strings.TrimSpace(answer)

	// Only the first line. Anything after it is the model explaining itself.
	if i := strings.IndexAny(title, "\r\n"); i >= 0 {
		title = title[:i]
	}

	// A label offered as "Title: Roof Quotes".
	if _, after, found := strings.Cut(title, ":"); found && len(after) > 0 {
		if before := title[:len(title)-len(after)-1]; len(strings.Fields(before)) <= 2 {
			title = after
		}
	}

	title = strings.TrimSpace(title)
	title = strings.Trim(title, `"'“”‘’.`)
	title = strings.TrimSpace(title)

	if title == "" || len(strings.Fields(title)) > MaxTitleWords {
		return ""
	}
	// A label does not end in punctuation that expects something to follow.
	if r := []rune(title); len(r) > 0 && (unicode.IsPunct(r[len(r)-1]) && r[len(r)-1] != ')') {
		return ""
	}

	return title
}

// TitleAnnouncement : What is said aloud when a conversation has been named,
// so that somebody who cannot see a screen knows what it is called.
func TitleAnnouncement(title string) string {
	return "I have called this conversation " + title + "."
}
