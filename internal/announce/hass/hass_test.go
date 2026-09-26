package hass_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/announce/hass"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// satellite : A Home Assistant that reports the given states in turn and
// records what it was asked to say.
type satellite struct {
	states []string
	asked  atomic.Int32
	polls  atomic.Int32
	said   atomic.Value
	// stateWhenAsked : What the satellite was doing at the moment it was told
	// to announce. This is the whole point: announcing interrupts.
	stateWhenAsked atomic.Value
}

func (s *satellite) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/states/"):
			i := int(s.polls.Add(1)) - 1
			if i >= len(s.states) {
				i = len(s.states) - 1
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"state": s.states[i]})

		case strings.HasPrefix(r.URL.Path, "/api/services/"):
			s.asked.Add(1)
			at := len(s.states) - 1
			if i := int(s.polls.Load()) - 1; i >= 0 && i < len(s.states) {
				at = i
			}
			s.stateWhenAsked.Store(s.states[at])

			var body struct {
				Message string `json:"message"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.said.Store(body.Message)
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// speaker : A Speaker pointed at the given satellite.
//
// settle is given explicitly by every test: left at zero it would take the
// default second, and a suite that waits a second per case to prove
// something else stops being run.
func speaker(t *testing.T, s *satellite, quiet, settle time.Duration) *hass.Speaker {
	t.Helper()
	server := httptest.NewServer(s.handler())
	t.Cleanup(server.Close)

	sp, err := hass.New(hass.Config{
		URL:       server.URL,
		Token:     logging.Secret("token"),
		Satellite: "assist_satellite.test",
		QuietWait: quiet,
		Settle:    settle,
	})
	if err != nil {
		t.Fatalf("hass.New: %v", err)
	}
	return sp
}

// Announcing interrupts, so nothing is said while the satellite is still
// speaking. This is the bug it was written for: an answer being read aloud
// stopped dead so the assistant could say what it had named the conversation.
func TestNothingIsSaidWhileTheSatelliteIsSpeaking(t *testing.T) {
	sat := &satellite{states: []string{"responding", "responding", "idle"}}
	sp := speaker(t, sat, 10*time.Second, -1)

	if err := sp.Say(context.Background(), "I have called this conversation Roof Quotes."); err != nil {
		t.Fatalf("Say: %v", err)
	}

	if got := sat.stateWhenAsked.Load(); got != "idle" {
		t.Errorf("announced while the satellite was %v, want it to wait for idle", got)
	}
	if got, _ := sat.said.Load().(string); !strings.Contains(got, "Roof Quotes") {
		t.Errorf("said %q, want the message", got)
	}
	if sat.polls.Load() < 3 {
		t.Errorf("polled %d times, want it to have waited", sat.polls.Load())
	}
}

// A satellite that is already quiet is spoken to at once.
func TestAQuietSatelliteIsSpokenToAtOnce(t *testing.T) {
	sat := &satellite{states: []string{"idle"}}
	sp := speaker(t, sat, 10*time.Second, -1)

	start := time.Now()
	if err := sp.Say(context.Background(), "Your timer is up."); err != nil {
		t.Fatalf("Say: %v", err)
	}

	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("waited %s for an idle satellite", took)
	}
	if sat.asked.Load() != 1 {
		t.Errorf("announced %d times, want once", sat.asked.Load())
	}
}

// One that never falls quiet is left alone. Dropping something incidental is
// better than cutting across an answer to deliver it.
func TestASatelliteThatNeverStopsIsNotInterrupted(t *testing.T) {
	sat := &satellite{states: []string{"responding"}}
	sp := speaker(t, sat, 900*time.Millisecond, -1)

	err := sp.Say(context.Background(), "I have called this conversation Roof Quotes.")
	if err == nil {
		t.Fatal("Say succeeded, want it to give up rather than interrupt")
	}
	if !strings.Contains(err.Error(), "responding") {
		t.Errorf("error = %v, want it to say what the satellite was doing", err)
	}
	if sat.asked.Load() != 0 {
		t.Error("it announced anyway, cutting across whatever was playing")
	}
}

// An empty message is not worth a round trip, let alone an interruption.
func TestNothingIsSaidForAnEmptyMessage(t *testing.T) {
	sat := &satellite{states: []string{"idle"}}
	sp := speaker(t, sat, time.Second, -1)

	if err := sp.Say(context.Background(), "   "); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if sat.asked.Load() != 0 || sat.polls.Load() != 0 {
		t.Error("an empty message reached Home Assistant")
	}
}

// Falling idle is not the same as having finished. The state flips when the
// satellite stops feeding the speaker, so announcing the instant it reads
// idle runs the two sentences together.
func TestTheAnnouncementWaitsAfterTheSpeechEnds(t *testing.T) {
	sat := &satellite{states: []string{"idle"}}
	sp := speaker(t, sat, 10*time.Second, 400*time.Millisecond)

	start := time.Now()
	if err := sp.Say(context.Background(), "I have called this conversation Roof Quotes."); err != nil {
		t.Fatalf("Say: %v", err)
	}

	if took := time.Since(start); took < 400*time.Millisecond {
		t.Errorf("announced after %s, want it held back for the settle", took)
	}
	if sat.polls.Load() < 2 {
		t.Errorf("polled %d times, want the quiet confirmed after the pause", sat.polls.Load())
	}
	if sat.asked.Load() != 1 {
		t.Errorf("announced %d times, want once", sat.asked.Load())
	}
}

// The quiet has to hold. Speech starting again during the pause means the
// gap never happened, so the wait begins again rather than announcing into
// it.
func TestSpeechDuringThePauseStartsTheWaitAgain(t *testing.T) {
	sat := &satellite{states: []string{"idle", "responding", "idle", "idle"}}
	sp := speaker(t, sat, 10*time.Second, 100*time.Millisecond)

	if err := sp.Say(context.Background(), "I have called this conversation Roof Quotes."); err != nil {
		t.Fatalf("Say: %v", err)
	}

	if got := sat.stateWhenAsked.Load(); got != "idle" {
		t.Errorf("announced while the satellite was %v, want it to wait again", got)
	}
	if sat.polls.Load() < 4 {
		t.Errorf("polled %d times, want the interrupted pause restarted", sat.polls.Load())
	}
}

// A negative settle is the old behaviour, kept so it can be turned off.
func TestANegativeSettleSpeaksAsSoonAsItIsIdle(t *testing.T) {
	sat := &satellite{states: []string{"idle"}}
	sp := speaker(t, sat, 10*time.Second, -1)

	start := time.Now()
	if err := sp.Say(context.Background(), "Your timer is up."); err != nil {
		t.Fatalf("Say: %v", err)
	}

	if took := time.Since(start); took > 200*time.Millisecond {
		t.Errorf("waited %s, want no pause at all", took)
	}
	if sat.polls.Load() != 1 {
		t.Errorf("polled %d times, want one", sat.polls.Load())
	}
}

// A server with nothing configured is a working server: it answers when
// spoken to and says nothing otherwise.
func TestNotConfiguredIsNotAFailure(t *testing.T) {
	_, err := hass.New(hass.Config{URL: "", Satellite: "x"})
	if !errors.Is(err, hass.ErrNotConfigured) {
		t.Errorf("error = %v, want ErrNotConfigured", err)
	}
}
