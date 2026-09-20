// Package platformai : Answers prompts using Zoho Platform AI.
//
// The service is request and response: one call returns one complete answer,
// with nothing in between. FRIDAY's Provider interface streams, because a user
// listening through earbuds needs to hear something long before the answer
// arrives. This provider therefore produces its own progress messages while it
// waits, and the service's reply becomes the final one.
package platformai

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DhanushRamesh/friday/internal/logging"
	"github.com/DhanushRamesh/friday/internal/provider"
)

const (
	// maxResponseBytes : The largest reply that will be read, so a
	// misbehaving endpoint cannot exhaust memory.
	maxResponseBytes = 32 << 20

	// DefaultTimeout : How long a single call may take.
	DefaultTimeout = 120 * time.Second

	// DefaultWorkingInterval : How often the user is reassured while waiting.
	// Silence longer than this is uncomfortable when listening rather than
	// watching a screen.
	DefaultWorkingInterval = 15 * time.Second

	// DefaultSystemPrompt : How FRIDAY is told to answer.
	//
	// It asks for speech rather than prose because replies are read aloud:
	// headings, bullet lists and code fences are noise when heard.
	DefaultSystemPrompt = "You are FRIDAY, a personal assistant. " +
		"Your replies are read aloud, so answer in plain spoken sentences. " +
		"Do not use markdown, headings, bullet points or code blocks. " +
		"Be brief and direct: say the answer first, then only the detail that matters."
)

// Default endpoints.
//
// These are the public addresses. The corresponding internal ones
// (accounts.csez.zohocorpin.com and platformai.csez.zohocorpin.com) serve the
// same paths but are reachable only from the corporate network, which FRIDAY
// cannot rely on once it is hosted anywhere else.
const (
	DefaultTokenURL    = "https://accounts.zoho.com/oauth/v2/token"
	DefaultChatURL     = "https://platformai.zoho.com/internalapi/v2/ai/chat"
	DefaultRedirectURI = "https://www.google.com/"
	DefaultScope       = "PlatformAI.organizations.all"
	DefaultVendor      = "anthropic"
	DefaultModel       = "claude-sonnet-4-6"
)

// Progress messages sent while waiting, written to be spoken.
const (
	defaultAckMessage     = "Let me look into that."
	defaultWorkingMessage = "Still working on it."
)

// Config : What the provider needs in order to reach the service.
type Config struct {
	// ClientID : The OAuth client. Required.
	ClientID string
	// ClientSecret : The OAuth client secret. Required.
	ClientSecret logging.Secret
	// RefreshToken : The long-lived token access tokens are minted from.
	// Required.
	RefreshToken logging.Secret

	// TokenURL : Where access tokens are obtained. Defaults to
	// DefaultTokenURL.
	TokenURL string
	// ChatURL : Where prompts are sent. Defaults to DefaultChatURL.
	ChatURL string
	// RedirectURI : Required by the OAuth endpoint. Defaults to
	// DefaultRedirectURI.
	RedirectURI string
	// Scope : The OAuth scope requested. Defaults to DefaultScope.
	Scope string
	// PortalID : Identifies the calling portal. Required.
	PortalID string

	// Vendor : Which model family to use. Defaults to DefaultVendor.
	Vendor string
	// Model : Which model to use. Defaults to DefaultModel.
	Model string
	// SystemPrompt : How the model is told to answer. Defaults to
	// DefaultSystemPrompt.
	SystemPrompt string

	// Timeout : How long one call may take. Zero selects DefaultTimeout.
	Timeout time.Duration
	// WorkingInterval : How often the user is reassured while waiting. Zero
	// selects DefaultWorkingInterval.
	WorkingInterval time.Duration
	// InsecureSkipVerify : Skips certificate verification. The public
	// endpoints present ordinary certificates, so this should stay false; it
	// exists only for the internal endpoints, whose certificates come from an
	// internal authority.
	InsecureSkipVerify bool
}

// Provider : A provider backed by Zoho Platform AI.
type Provider struct {
	cfg    Config
	logger *slog.Logger
	http   *http.Client

	tokenState
}

// New : Builds a Provider, applying defaults and reporting missing
// credentials.
func New(cfg Config, logger *slog.Logger) (*Provider, error) {
	if logger == nil {
		return nil, errors.New("platformai: a logger is required")
	}

	var missing []string
	if cfg.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if cfg.ClientSecret == "" {
		missing = append(missing, "client_secret")
	}
	if cfg.RefreshToken == "" {
		missing = append(missing, "refresh_token")
	}
	if cfg.PortalID == "" {
		missing = append(missing, "portal_id")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("platformai: missing %s", strings.Join(missing, ", "))
	}

	applyDefaults(&cfg)

	return &Provider{
		cfg:    cfg,
		logger: logger,
		http: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
			},
		},
	}, nil
}

// applyDefaults : Fills in whatever the caller left empty.
func applyDefaults(cfg *Config) {
	if cfg.TokenURL == "" {
		cfg.TokenURL = DefaultTokenURL
	}
	if cfg.ChatURL == "" {
		cfg.ChatURL = DefaultChatURL
	}
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = DefaultRedirectURI
	}
	if cfg.Scope == "" {
		cfg.Scope = DefaultScope
	}
	if cfg.Vendor == "" {
		cfg.Vendor = DefaultVendor
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = DefaultSystemPrompt
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.WorkingInterval <= 0 {
		cfg.WorkingInterval = DefaultWorkingInterval
	}
}

// Name : Returns the provider's name.
func (p *Provider) Name() string { return "platformai" }

// Run : Sends the prompt and streams the reply. See provider.Provider for the
// contract it follows.
//
// Because the service answers in one piece, the stream is an immediate
// acknowledgement, a reassurance every so often while the call is outstanding,
// and then the reply. The user hears something at once instead of silence.
func (p *Provider) Run(ctx context.Context, req provider.Request) (<-chan provider.Message, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, provider.ErrEmptyPrompt
	}

	ch := make(chan provider.Message)
	go func() {
		defer close(ch)

		if !send(ctx, ch, provider.Update(defaultAckMessage)) {
			return
		}

		// The call runs alongside, so progress can be sent while it is
		// outstanding.
		type result struct {
			text string
			err  error
		}
		done := make(chan result, 1)
		go func() {
			text, err := p.chat(ctx, req.Prompt)
			done <- result{text: text, err: err}
		}()

		working := time.NewTicker(p.cfg.WorkingInterval)
		defer working.Stop()

		started := time.Now()
		for {
			select {
			case <-ctx.Done():
				return

			case <-working.C:
				if !send(ctx, ch, provider.Update(defaultWorkingMessage)) {
					return
				}

			case r := <-done:
				if r.err != nil {
					p.logger.ErrorContext(ctx, "platform ai call failed",
						slog.Duration("after", time.Since(started)),
						slog.Any("error", r.err))
					send(ctx, ch, provider.Failure(userFacing(r.err)))
					return
				}
				p.logger.InfoContext(ctx, "platform ai answered",
					slog.Duration("after", time.Since(started)),
					slog.Int("reply_bytes", len(r.text)))
				send(ctx, ch, provider.Final(r.text))
				return
			}
		}
	}()

	return ch, nil
}

// userFacing : Turns a failure into something that can be read aloud.
//
// A message the service wrote is passed on, since it was written to be read.
// Anything else is described in general terms, because transport and decoding
// errors mean nothing to a listener and can carry internal detail.
func userFacing(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return apiErr.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The service did not answer in time."
	}
	return "I could not reach the service that answers this."
}

// send : Delivers a message, reporting false if ctx ends before the caller
// receives it.
func send(ctx context.Context, ch chan<- provider.Message, msg provider.Message) bool {
	select {
	case ch <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}

// Provider implements the interface the runner depends on.
var _ provider.Provider = (*Provider)(nil)
