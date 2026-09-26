package remind_test

import (
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// india : Where the person is, for these.
var india = time.FixedZone("IST", 5*3600+1800)

// at : A moment in India, as a helper reads best.
func at(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, india)
}

// A one-shot never comes back.
func TestOnceHasNoNext(t *testing.T) {
	if _, ok := remind.Next(at(2026, 9, 26, 7, 0), remind.Once, at(2026, 9, 26, 8, 0), india); ok {
		t.Error("a one-shot reported a next time")
	}
}

// Daily keeps the hour the person chose.
func TestDailyKeepsTheHour(t *testing.T) {
	got, ok := remind.Next(at(2026, 9, 26, 7, 0), remind.Daily, at(2026, 9, 26, 8, 0), india)
	if !ok {
		t.Fatal("daily reported no next time")
	}
	if want := at(2026, 9, 27, 7, 0); !got.Equal(want) {
		t.Errorf("next = %v, want %v", got.In(india), want)
	}
}

// Weekdays skips the weekend. Friday's next is Monday.
func TestWeekdaysSkipTheWeekend(t *testing.T) {
	friday := at(2026, 9, 25, 7, 0)
	if friday.Weekday() != time.Friday {
		t.Fatalf("the fixture is a %v, not a Friday", friday.Weekday())
	}

	got, ok := remind.Next(friday, remind.Weekdays, at(2026, 9, 25, 8, 0), india)
	if !ok {
		t.Fatal("weekdays reported no next time")
	}
	if got.In(india).Weekday() != time.Monday {
		t.Errorf("next is a %v, want Monday", got.In(india).Weekday())
	}
}

// The 31st does not become the 3rd of the month after.
//
// AddDate normalises, so January the 31st plus a month is March the 3rd.
// Nobody setting something for the 31st means the 3rd.
func TestTheEndOfTheMonthDoesNotSlide(t *testing.T) {
	got, ok := remind.Next(at(2026, 1, 31, 9, 0), remind.Monthly, at(2026, 1, 31, 10, 0), india)
	if !ok {
		t.Fatal("monthly reported no next time")
	}

	next := got.In(india)
	if next.Month() != time.February || next.Day() != 28 {
		t.Errorf("next = %v, want 28 February", next)
	}
	if next.Hour() != 9 {
		t.Errorf("the hour moved to %d", next.Hour())
	}
}

// A month that has the date keeps it.
func TestAMonthThatHasTheDateKeepsIt(t *testing.T) {
	got, _ := remind.Next(at(2026, 3, 15, 9, 0), remind.Monthly, at(2026, 3, 15, 10, 0), india)

	if next := got.In(india); next.Month() != time.April || next.Day() != 15 {
		t.Errorf("next = %v, want 15 April", next)
	}
}

// A server that was off for a week resumes at the next real occurrence
// rather than firing every day it missed.
func TestItCatchesUpToNowRatherThanFiringSixTimes(t *testing.T) {
	got, ok := remind.Next(at(2026, 9, 1, 7, 0), remind.Daily, at(2026, 9, 26, 12, 0), india)
	if !ok {
		t.Fatal("daily reported no next time")
	}

	next := got.In(india)
	if next.Day() != 27 || next.Month() != time.September {
		t.Errorf("next = %v, want 27 September", next)
	}
	if next.Hour() != 7 {
		t.Errorf("the hour moved to %d", next.Hour())
	}
}

// Where the clocks change, the hour survives. Adding twenty-four hours
// would move a seven o'clock reminder to six.
func TestTheHourSurvivesAClockChange(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("no timezone data on this machine: %v", err)
	}

	// The clocks go back on 25 October 2026.
	before := time.Date(2026, 10, 24, 7, 0, 0, 0, london)
	got, ok := remind.Next(before, remind.Daily, before.Add(time.Hour), london)
	if !ok {
		t.Fatal("daily reported no next time")
	}

	next := got.In(london)
	if next.Hour() != 7 {
		t.Errorf("next is at %02d:00, want 07:00 the day the clocks changed", next.Hour())
	}
}
