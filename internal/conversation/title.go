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

// Whereabouts : What the assistant is told about the conversation it is
// answering in.
//
// Without it the assistant has no way to know where it is, and asked
// outright it answers from whatever it remembers doing -- which is how it
// came to name a conversation it had switched away from. A fact it cannot
// look up is a fact it will invent.
func Whereabouts(id, title string) string {
	if id == "" {
		return ""
	}
	if strings.TrimSpace(title) == "" {
		return "You are answering in a conversation that has no name yet, with the identifier " +
			id + ". Say so if asked which conversation this is; do not guess at a name."
	}
	return "You are answering in the conversation titled " + title +
		", with the identifier " + id + "."
}

// Heard : What the assistant is told when the words reached it as speech.
//
// Only for a spoken turn. Speech-to-text fails differently from typing: it
// does not produce a misspelling, it produces a different word that sounds
// like the right one, and the sentence stays grammatical while meaning
// something else. "Unarchive" arrived as "unlock". A model that does not know
// where the words came from has no reason to look past them.
//
// The caution at the end is the part that matters. Reading charitably is
// right until a charitable reading destroys something.
//
// Names are carved out of the charity altogether. Everywhere else a word
// that does not fit can be reasoned about from what does; a name cannot,
// because an unfamiliar name and a mangled one are indistinguishable. The
// decoder offered "Alikia", "Alakia" and "alakia chintada" for one name in
// a single evening. Guessing there is how the wrong person gets texted.
func Heard() string {
	return "What the person said reached you as speech turned into text, and it can be " +
		"wrong in ways typing is not: a word may be replaced by another that sounds like " +
		"it, leaving a sentence that reads correctly and means something else. Read for " +
		"what they meant. Where a word does not fit what is being discussed, consider what " +
		"similar-sounding word would, and act on that. " +
		"Where two readings are both plausible and one of them deletes or destroys " +
		"something, ask which was meant rather than choosing. " +

		"A name is the exception to all of that. Where what is being named is a " +
		"person, a place, a film, a song, a book or anything else with a spelling of " +
		"its own, do not reach for a name that sounds similar and do not settle on " +
		"the one you happen to know. Speech-to-text is at its worst on names, and a " +
		"name you have never heard and a name it has mangled look exactly alike, so " +
		"there is nothing to tell them apart by. Say back what you heard and ask them " +
		"to spell it. " +

		"Ask before you use it, not after. Do not write a heard name into a memory " +
		"or a reminder, do not search on it, and do not answer about it, until they " +
		"have spelt it: a name stored wrongly stays wrong, and nothing later will " +
		"find it to correct. If you had to complete or repair the name to recognise " +
		"it at all, say what you took it to be and have them confirm it before you " +
		"go on. " +

		"That is the one place to end on a question. A question mark keeps the " +
		"microphone open for the answer, which is the whole point of asking."
}

// AnnounceTitlesSetting : What the choice to hear a new name is stored under.
const AnnounceTitlesSetting = "announce_titles"

// TitleAnnouncement : What is said aloud when a conversation has been named,
// so that somebody who cannot see a screen knows what it is called.
func TitleAnnouncement(title string) string {
	return "I have called this conversation " + title + "."
}
