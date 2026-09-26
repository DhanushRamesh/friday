package memory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
)

// An exchange is the message and the reply together, both named, since a
// hit is quoted back and "you said" is not "I said".
func TestAnExchangeNamesBothHalves(t *testing.T) {
	got := memory.ExchangeText("what did the roofer quote", "forty thousand rupees")

	for _, want := range []string{"They said:", "what did the roofer quote", "You answered:", "forty thousand rupees"} {
		if !strings.Contains(got, want) {
			t.Errorf("exchange is missing %q:\n%s", want, got)
		}
	}
}

// A turn with no reply yet is still indexable, so the last thing said is
// not invisible until something answers it.
func TestAnExchangeSurvivesAMissingReply(t *testing.T) {
	got := memory.ExchangeText("what did the roofer quote", "  ")

	if !strings.Contains(got, "what did the roofer quote") {
		t.Errorf("exchange = %q", got)
	}
	if strings.Contains(got, "You answered") {
		t.Errorf("an empty reply was written down as one: %q", got)
	}
}

// Nothing said is no exchange at all.
func TestNothingSaidIsNoExchange(t *testing.T) {
	if got := memory.ExchangeText("   ", "a reply"); got != "" {
		t.Errorf("ExchangeText = %q, want empty", got)
	}
}

// The quoted block says it is a transcript, not a conclusion, and dates it.
func TestQuotedIsMarkedAsARecord(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)
	got := memory.Quoted([]memory.Heard{{
		Exchange: memory.Exchange{
			Text: "They said: what did the roofer quote\nYou answered: forty thousand",
			At:   time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC),
		},
	}}, india)

	for _, want := range []string{
		"never state one as a fact of your own",
		"may contain the wrong word",
		"ignoring all of them",
		"Saturday 12 September 2026 at 3:30 pm",
		"forty thousand",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("quoted block is missing %q:\n%s", want, got)
		}
	}
}

// Nothing found adds nothing to the prompt.
func TestNoExchangesProduceNoBlock(t *testing.T) {
	if got := memory.Quoted(nil, time.UTC); got != "" {
		t.Errorf("Quoted = %q, want empty", got)
	}
}

// fakeTranscript : A transcript held in a slice.
type fakeTranscript struct {
	pending []memory.Exchange
	indexed map[string][]float32
	skipped string
}

func (f *fakeTranscript) Unindexed(_ context.Context, _ string, limit int) ([]memory.Exchange, error) {
	var out []memory.Exchange
	for _, e := range f.pending {
		if f.indexed[e.MessageID] == nil && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeTranscript) Index(_ context.Context, e memory.Exchange, _ string, v []float32) error {
	if f.indexed == nil {
		f.indexed = map[string][]float32{}
	}
	f.indexed[e.MessageID] = v
	return nil
}

func (f *fakeTranscript) NearestTo(_ context.Context, _ string, q []float32, _ string,
	skip string, limit int) ([]memory.Heard, error) {

	f.skipped = skip
	var out []memory.Heard
	for _, e := range f.pending {
		if v := f.indexed[e.MessageID]; v != nil && e.ConversationID != skip {
			out = append(out, memory.Heard{Exchange: e, Score: embed.Similarity(q, v)})
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Indexing works through everything waiting, in batches.
func TestIndexingWorksThroughTheBacklog(t *testing.T) {
	f := &fakeTranscript{}
	for i := 0; i < 70; i++ {
		f.pending = append(f.pending, memory.Exchange{
			MessageID: string(rune('a'+i%26)) + string(rune('0'+i/26)),
			UserID:    "usr_1", Text: "They said: something", At: time.Now(),
		})
	}

	r := &memory.Recall{Transcript: f, Embedder: embed.Fake{}}
	done, err := r.IndexTranscript(context.Background(), 100)
	if err != nil {
		t.Fatalf("IndexTranscript: %v", err)
	}
	if done != len(f.pending) {
		t.Errorf("indexed %d of %d", done, len(f.pending))
	}

	again, err := r.IndexTranscript(context.Background(), 100)
	if err != nil || again != 0 {
		t.Errorf("IndexTranscript = %d, %v, want nothing left", again, err)
	}
}

// Indexing stops at the budget, since it runs while a chat slot is held.
func TestIndexingStopsAtTheBudget(t *testing.T) {
	f := &fakeTranscript{}
	for i := 0; i < 50; i++ {
		f.pending = append(f.pending, memory.Exchange{
			MessageID: string(rune('a'+i%26)) + string(rune('0'+i/26)),
			UserID:    "usr_1", Text: "They said: something", At: time.Now(),
		})
	}

	r := &memory.Recall{Transcript: f, Embedder: embed.Fake{}}
	done, err := r.IndexTranscript(context.Background(), 10)
	if err != nil {
		t.Fatalf("IndexTranscript: %v", err)
	}
	if done != 10 {
		t.Errorf("indexed %d, want the budget of 10", done)
	}
}

// The conversation in progress is left out: it is already in front of the
// model, and offering it back reads as the assistant quoting itself.
func TestTheCurrentConversationIsNotQuotedBack(t *testing.T) {
	f := &fakeTranscript{pending: []memory.Exchange{
		{MessageID: "m1", UserID: "usr_1", ConversationID: "conv_here", Text: "They said: the roof", At: time.Now()},
		{MessageID: "m2", UserID: "usr_1", ConversationID: "conv_other", Text: "They said: the roof quote", At: time.Now()},
	}}

	r := &memory.Recall{Transcript: f, Embedder: embed.Fake{}}
	if _, err := r.IndexTranscript(context.Background(), 10); err != nil {
		t.Fatalf("IndexTranscript: %v", err)
	}

	got, err := r.Said(context.Background(), "usr_1", "what about the roof", "conv_here")
	if err != nil {
		t.Fatalf("Said: %v", err)
	}
	if f.skipped != "conv_here" {
		t.Errorf("skipped %q, want the conversation in progress", f.skipped)
	}
	for _, h := range got {
		if h.Exchange.ConversationID == "conv_here" {
			t.Error("the conversation in progress was quoted back")
		}
	}
}

// Without an embedder the transcript is not searched by words. A word
// search over the whole record finds whatever repeated a common word.
func TestNoEmbedderMeansNoTranscriptSearch(t *testing.T) {
	f := &fakeTranscript{pending: []memory.Exchange{
		{MessageID: "m1", UserID: "usr_1", Text: "They said: the roof quote", At: time.Now()},
	}}

	r := &memory.Recall{Transcript: f, Embedder: embed.Off{}}
	got, err := r.Said(context.Background(), "usr_1", "the roof quote", "")
	if err != nil || len(got) != 0 {
		t.Errorf("Said = %v, %v, want nothing", got, err)
	}
}

// No transcript at all is not a failure.
func TestNoTranscriptIsQuiet(t *testing.T) {
	r := &memory.Recall{Embedder: embed.Fake{}}

	if got, err := r.Said(context.Background(), "usr_1", "anything", ""); err != nil || len(got) != 0 {
		t.Errorf("Said = %v, %v, want nothing", got, err)
	}
	if done, err := r.IndexTranscript(context.Background(), 10); err != nil || done != 0 {
		t.Errorf("IndexTranscript = %d, %v, want nothing", done, err)
	}
}

// When something was said is shown in the person's own time, not the
// server's. An evening in India is the day before in UTC, so a date alone
// in UTC is the wrong date.
func TestWhenItWasSaidIsInThePersonsOwnTime(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)

	// India is five and a half hours ahead, so two in the morning there is
	// still the evening before in UTC. Stamping in UTC would give the
	// person yesterday's date for something they said today.
	said := time.Date(2026, 9, 26, 20, 30, 0, 0, time.UTC)
	got := memory.Quoted([]memory.Heard{{Exchange: memory.Exchange{Text: "They said: hello", At: said}}}, india)

	if !strings.Contains(got, "Sunday 27 September 2026 at 2:00 am") {
		t.Errorf("stamped as the wrong moment:\n%s", got)
	}
	if strings.Contains(got, "26 September") {
		t.Errorf("stamped with the server's date rather than the person's:\n%s", got)
	}
}

// The model is told it may answer with when, or it says it has no record
// of the time while holding it.
func TestItIsToldItMaySayWhen(t *testing.T) {
	got := memory.Quoted([]memory.Heard{{Exchange: memory.Exchange{Text: "They said: hello", At: time.Now()}}}, time.UTC)

	if !strings.Contains(got, "when something was said as readily as what was said") {
		t.Errorf("nothing says the time may be used:\n%s", got)
	}
}
