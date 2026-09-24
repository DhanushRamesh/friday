package assist

import "testing"

// Inside the package, because what matters here is one unexported function
// and the cost of a wrong answer is high: Home Assistant reopens the
// microphone when an answer ends in a question mark and offers no way to turn
// that off, so a reply that slips through leaves the satellite listening and
// the wake word unnecessary.
func TestSettledRemovesOnlyWhatReopensTheMicrophone(t *testing.T) {
	// The three characters Home Assistant looks for, and the cases that must
	// be left alone.
	cases := map[string]struct{ in, want string }{
		"question mark":          {"What can I do for you?", "What can I do for you."},
		"fullwidth question":     {"What can I do for you？", "What can I do for you."},
		"greek question":         {"What can I do for you;", "What can I do for you."},
		"ordinary answer":        {"The capital of France is Paris.", "The capital of France is Paris."},
		"question in the middle": {"Is it raining? I cannot say.", "Is it raining? I cannot say."},
		"exclamation":            {"You are welcome!", "You are welcome!"},
		"empty":                  {"", ""},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := settled(c.in); got != c.want {
				t.Errorf("settled(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
