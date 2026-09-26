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
// The instruction matters more than the notes. Nearest is not relevant: a
// question with nothing stored about it still has a nearest memory, and it
// scores in the same range as a real match, so the model is told that none
// of them fitting is the usual case and is the right answer when it is true.
// Empty when there is nothing to offer.
func Offered(matches []Match) string {
	if len(matches) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Notes found by searching what you have been asked to remember. ")
	b.WriteString("They were chosen for resembling the question, which is not the ")
	b.WriteString("same as answering it: most of the time none of them will, and ")
	b.WriteString("ignoring all of them is then the right thing to do. Use one only ")
	b.WriteString("if it contains what is being asked for, rather than merely a ")
	b.WriteString("related subject. Do not mention a note you did not use, and do ")
	b.WriteString("not tell the person a note exists instead of answering them.")
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
