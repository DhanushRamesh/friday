// Package hass speaks through a Home Assistant voice satellite.
//
// Home Assistant calls the server to have a question answered. This is the
// other way round: the server calling Home Assistant to have something said
// out loud, through the same speaker the person is already talking to.
package hass

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// DefaultTimeout : How long a single call may take.
//
// Short, because nothing waits on an announcement and a satellite that is not
// answering should not hold anything up.
const DefaultTimeout = 10 * time.Second

// DefaultQuietWait : How long to wait for the satellite to stop talking
// before speaking over it.
//
// Announcing interrupts: the satellite drops whatever it is playing and says
// the new thing instead. An announcement that follows an answer therefore
// cuts the answer off part-way, which is worse than not announcing at all.
// Long enough for a paragraph read aloud, and after that the announcement is
// abandoned rather than delivered on top of speech.
const DefaultQuietWait = 90 * time.Second

// idlePoll : How often the satellite is asked whether it has finished.
const idlePoll = 400 * time.Millisecond

// stateIdle : What the satellite calls doing nothing. Its other states are
// listening, processing and responding.
const stateIdle = "idle"

// maxResponseBytes : The most that will be read from a reply, so a
// misbehaving endpoint cannot exhaust memory.
const maxResponseBytes = 1 << 20

// Config : What is needed to reach Home Assistant.
type Config struct {
	// URL : Where Home Assistant answers, such as http://192.168.0.102:8123.
	// Empty disables announcing altogether.
	URL string
	// Token : A long-lived access token, created by the owner in Home
	// Assistant under their profile's security settings.
	Token logging.Secret
	// Satellite : The entity to speak through, such as
	// assist_satellite.laptop_lva_assist_satellite.
	Satellite string
	// Timeout : How long a call may take. Zero selects DefaultTimeout.
	Timeout time.Duration
	// QuietWait : How long to wait for the satellite to finish speaking
	// before announcing. Zero selects DefaultQuietWait; negative announces
	// at once and interrupts whatever is playing.
	QuietWait time.Duration
	// HTTP : The client to use. Optional.
	HTTP *http.Client
}

// Speaker : Says things aloud through one Home Assistant satellite.
type Speaker struct {
	cfg  Config
	http *http.Client
}

// ErrNotConfigured : Returned by New when there is not enough to reach Home
// Assistant. Not a fault: a server with nothing to announce through is a
// working server.
var ErrNotConfigured = errors.New("hass: no url, token or satellite")

// New : Builds a Speaker, or reports that there is nothing to build it from.
func New(cfg Config) (*Speaker, error) {
	if strings.TrimSpace(cfg.URL) == "" ||
		strings.TrimSpace(cfg.Token.Reveal()) == "" ||
		strings.TrimSpace(cfg.Satellite) == "" {
		return nil, ErrNotConfigured
	}

	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.QuietWait == 0 {
		cfg.QuietWait = DefaultQuietWait
	}

	client := cfg.HTTP
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}

	return &Speaker{cfg: cfg, http: client}, nil
}

// Available : Whether there is a satellite to speak through.
func (s *Speaker) Available() bool { return s != nil }

// announceRequest : The body of assist_satellite.announce.
type announceRequest struct {
	EntityID string `json:"entity_id"`
	Message  string `json:"message"`
	// Preannounce : Whether to play a chime first. Always false here: what
	// this says is a footnote to something already spoken, not a summons.
	Preannounce bool `json:"preannounce"`
}

// Say : Speaks the message through the configured satellite, once it has
// finished saying anything else.
//
// Announcing interrupts, so this waits for the satellite to fall idle first.
// Without that an announcement following an answer cuts the answer off
// mid-sentence, which is how this was found: a recitation stopped dead so the
// assistant could say what it had named the conversation.
func (s *Speaker) Say(ctx context.Context, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}

	if err := s.waitUntilQuiet(ctx); err != nil {
		return err
	}

	body, err := json.Marshal(announceRequest{
		EntityID:    s.cfg.Satellite,
		Message:     message,
		Preannounce: false,
	})
	if err != nil {
		return fmt.Errorf("hass: building announce request: %w", err)
	}

	url := s.cfg.URL + "/api/services/assist_satellite/announce"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("hass: building announce request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token.Reveal())
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("hass: announcing: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard, so the connection can be reused.
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode >= 400 {
		return fmt.Errorf("hass: announcing: %s: %s",
			resp.Status, strings.TrimSpace(string(answer)))
	}
	return nil
}

// waitUntilQuiet : Blocks until the satellite is doing nothing.
//
// A satellite that never falls quiet is reported rather than spoken over: the
// caller can then log it and drop the announcement, which is the right
// outcome for anything incidental.
func (s *Speaker) waitUntilQuiet(ctx context.Context) error {
	if s.cfg.QuietWait < 0 {
		return nil
	}

	deadline := time.Now().Add(s.cfg.QuietWait)
	for {
		state, err := s.state(ctx)
		if err != nil {
			// Not knowing is not a reason to interrupt. Treated as still
			// speaking, so the announcement is dropped rather than cutting
			// across an answer.
			return err
		}
		if state == stateIdle {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("hass: %s was still %s after %s", s.cfg.Satellite, state, s.cfg.QuietWait)
		}

		select {
		case <-time.After(idlePoll):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// state : What the satellite is doing, as Home Assistant reports it.
func (s *Speaker) state(ctx context.Context) (string, error) {
	url := s.cfg.URL + "/api/states/" + s.cfg.Satellite
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("hass: building state request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token.Reveal())

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("hass: reading satellite state: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("hass: reading satellite state: %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("hass: satellite state was not usable: %w", err)
	}
	return parsed.State, nil
}
