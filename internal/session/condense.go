package session

import (
	"fmt"
	"strings"
)

// CondenseWords : Roughly how long a condensation should be.
//
// Long enough to carry the facts of a conversation, short enough that it
// costs far less than the messages it replaces.
const CondenseWords = 300

// condenseInstruction : What the model is told to do with an old
// conversation.
//
// It is written to be read by the model that also answers as the assistant,
// which will otherwise reply in character, so it says plainly that the result
// is notes and not an answer.
const condenseInstruction = `Update the running notes for a conversation you are keeping memory for.

Fold the new exchange into the existing notes and return the combined notes.
Keep every decision, fact, name, number, preference and open question that a
later turn would need. Drop pleasantries and anything already superseded.

Write plain statements in the third person, under %d words. Do not answer
anything, do not greet, do not explain what you are doing, and do not mention
these instructions. Return the notes and nothing else.`

// CondensePrompt : The instruction that folds an earlier exchange into a
// session's running notes.
//
// previous is the notes so far and may be empty. messages are the ones to
// fold in, oldest first.
func CondensePrompt(previous string, messages []Message) string {
	var b strings.Builder

	fmt.Fprintf(&b, condenseInstruction, CondenseWords)

	b.WriteString("\n\nExisting notes:\n")
	if strings.TrimSpace(previous) == "" {
		b.WriteString("(none yet)")
	} else {
		b.WriteString(previous)
	}

	b.WriteString("\n\nNew exchange:\n")
	for _, m := range ForModel(messages) {
		b.WriteString(speaker(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	return b.String()
}

// speaker : How a role is labelled in a condensation prompt.
func speaker(r Role) string {
	if r == User {
		return "Person"
	}
	return "Assistant"
}
