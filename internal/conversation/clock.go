package conversation

import "time"

// Now : What the assistant is told the time is.
//
// A model knows nothing about when it is being asked. Without this it
// answers "what day is it" from whenever it was trained, or refuses, and
// "in twenty minutes" has nothing to be twenty minutes after.
//
// The time is given in the person's own zone, because that is the only one
// they mean. The zone is named as well, so an answer can say so when it
// matters and nothing has to be inferred from the offset.
func Now(at time.Time) string {
	return "The time where the person is: " +
		at.Format("3:04 pm on Monday 2 January 2006") +
		" (" + at.Format("MST-07:00") + "). " +
		"Work out anything they say about time from this, and never guess the date."
}
