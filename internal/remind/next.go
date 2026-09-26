package remind

import "time"

// Next : When a repeating reminder is next due, after the given moment.
//
// Worked out in the person's own zone so the hour they chose survives.
// Adding twenty-four hours would not: where the clocks change, it moves a
// seven o'clock reminder to six or eight. Adding a day keeps it at seven.
//
// It steps until it lands in the future rather than returning the very next
// occurrence, so a server that was off for a week resumes at the next real
// one instead of firing six times to catch up.
//
// A one-shot has no next, and reports so.
func Next(due time.Time, repeats Repeat, after time.Time, loc *time.Location) (time.Time, bool) {
	if repeats == Once || !repeats.Valid() {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.UTC
	}

	at := due.In(loc)
	after = after.In(loc)

	// Bounded, so a rule that somehow never advances cannot spin. Ten years
	// of daily steps is far past anything a person would set.
	for i := 0; i < 4000; i++ {
		at = step(at, repeats)
		if at.After(after) {
			return at.UTC(), true
		}
	}
	return time.Time{}, false
}

// step : One occurrence later.
func step(at time.Time, repeats Repeat) time.Time {
	switch repeats {
	case Daily:
		return at.AddDate(0, 0, 1)

	case Weekdays:
		next := at.AddDate(0, 0, 1)
		for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.AddDate(0, 0, 1)
		}
		return next

	case Weekly:
		return at.AddDate(0, 0, 7)

	case Monthly:
		return nextMonth(at)
	}
	return at
}

// nextMonth : The same date next month, or the last day of it.
//
// AddDate normalises, so the 31st of January plus one month is the 3rd of
// March. Nobody setting something for the 31st means the 3rd, so a date the
// next month does not have becomes its last day.
func nextMonth(at time.Time) time.Time {
	year, month, day := at.Date()
	hour, min, sec := at.Clock()

	first := time.Date(year, month, 1, hour, min, sec, at.Nanosecond(), at.Location())
	next := first.AddDate(0, 1, 0)

	if last := daysIn(next.Year(), next.Month()); day > last {
		day = last
	}
	return time.Date(next.Year(), next.Month(), day, hour, min, sec, at.Nanosecond(), at.Location())
}

// daysIn : How many days that month has.
func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
