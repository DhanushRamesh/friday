package memory

import "strings"

// Standing : What the always-memories look like in a system prompt.
//
// Facts about the person that hold whatever they ask, so they are stated
// plainly and without hedging. Empty when there are none.
func Standing(all []Memory) string {
	if len(all) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("What you know about the person you are answering:")
	for i := range all {
		b.WriteString("\n- ")
		b.WriteString(all[i].Text())
	}
	return b.String()
}

// Offered : What the recalled memories look like in a system prompt.
//
// The instruction matters more than the notes, and it does two jobs that
// pull against each other.
//
// Answering: nearest is not relevant. A question with nothing stored about
// it still has a nearest memory, scoring in the same range as a real match,
// so the model is told that none of them fitting is the usual case. It is
// also told to judge the thing rather than the wording: asked what the
// builder charged upstairs, it once refused a note about the roofer and the
// terrace and said nothing was on record, which is both a miss and a false
// statement.
//
// Warning: an assistant that only answers is an instrument. A note that
// contradicts what someone is about to do is worth a line even though it is
// not what they asked, which is exactly the case the answering rule
// forbids. The licence is therefore separate and narrow: only against a
// stated intention, only when the note disagrees with it, one line, and
// attributed.
//
// Empty when there is nothing to offer.
func Offered(matches []Match) string {
	if len(matches) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Notes found by searching what you have been asked to remember. ")
	b.WriteString("They were chosen for resembling the question, which is not the ")
	b.WriteString("same as bearing on it: most of the time none of them will, and ")
	b.WriteString("ignoring all of them is then the right thing to do.\n\n")

	b.WriteString("To answer with: use a note when it holds what is being asked ")
	b.WriteString("for. The same thing is often named one way in the question and ")
	b.WriteString("another in the note -- a trade, a place, a person or a job ")
	b.WriteString("described differently -- so judge whether it is the same thing, ")
	b.WriteString("not whether the words match. Do not stretch a note to cover a ")
	b.WriteString("different thing. Where a note is plainly about what was asked, ")
	b.WriteString("never say there is nothing on record: say what the note says. Do ")
	b.WriteString("not mention a note you did not use, and do not tell the person a ")
	b.WriteString("note exists instead of answering them.\n\n")

	b.WriteString("To warn with: if the person says what they are about to do, and ")
	b.WriteString("a note disagrees with it -- a figure they agreed, a limit they ")
	b.WriteString("set, something they must avoid -- say so in one short line after ")
	b.WriteString("your answer, and say what it is you are going on. Only where the ")
	b.WriteString("note actually disagrees: not where it merely shares a subject, ")
	b.WriteString("and not where it agrees with what they intend. Say nothing beyond ")
	b.WriteString("what the note says, and invent no concern it does not support. ")
	b.WriteString("Most turns need no such line.")

	for i := range matches {
		b.WriteString("\n- ")
		b.WriteString(matches[i].Memory.Text())
	}
	return b.String()
}

// IDs : The identifiers of the memories in matches.
func IDs(matches []Match) []string {
	out := make([]string, 0, len(matches))
	for i := range matches {
		out = append(out, matches[i].Memory.ID)
	}
	return out
}
