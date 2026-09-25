// Package platformai : Answers prompts using Zoho Platform AI.
//
// The service is request and response: one call returns one complete answer,
// with nothing in between. The server's Environment interface streams, because a user
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

	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/failure"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

const (
	// maxResponseBytes : The largest reply that will be read, so a
	// misbehaving endpoint cannot exhaust memory.
	maxResponseBytes = 32 << 20

	// DefaultTimeout : How long a single call may take.
	DefaultTimeout = 120 * time.Second

	// MaxMessages : The most messages this endpoint accepts in one request.
	//
	// It answers ARRAY_SIZE_OUT_OF_RANGE beyond this. The prompt occupies one
	// of the places, so the history sent alongside it is one shorter.
	MaxMessages = 100

	// DefaultSystemPrompt : What the model is told when a caller gives
	// nothing.
	//
	// A bare fallback. What the assistant says about itself, and how a reply
	// is worded, is composed in internal/persona and arrives with each
	// request, because the manner is chosen while the server runs. None of
	// that belongs to an endpoint adapter.
	DefaultSystemPrompt = "You are a personal assistant."
)

// Default endpoints.
//
// These are the public addresses. The corresponding internal ones
// (accounts.csez.zohocorpin.com and platformai.csez.zohocorpin.com) serve the
// same paths but are reachable only from the corporate network, which the server
// cannot rely on once it is hosted anywhere else.
const (
	DefaultTokenURL    = "https://accounts.zoho.com/oauth/v2/token"
	DefaultChatURL     = "https://platformai.zoho.com/internalapi/v2/ai/chat"
	DefaultRedirectURI = "https://www.google.com/"
	DefaultScope       = "PlatformAI.organizations.all"
	DefaultVendor      = "anthropic"
	DefaultModel       = "claude-sonnet-4-6"
)

// ModelRef : One model this endpoint will answer with.
type ModelRef struct{ Vendor, ID string }

// Models : The models this endpoint is known to answer with.
//
// Named one by one rather than by vendor, because a vendor the service speaks
// to is not the set of models it routes: it reaches Anthropic and refuses
// claude-haiku-4-5, while answering to the dated identifier for the same
// model. Every one of these was verified by asking it, and a model absent
// here is one nobody has tried rather than one known to fail.
//
// Google is not listed at all: the endpoint rejects the vendor outright.
func Models() []ModelRef {
	return []ModelRef{
		{"anthropic", "claude-sonnet-4-6"},
		{"anthropic", "claude-sonnet-4-5"},
		{"anthropic", "claude-opus-4-5"},
		{"anthropic", "claude-haiku-4-5-20251001"},
		{"openai", "gpt-4o"},
		{"openai", "gpt-4o-mini"},
		{"openai", "gpt-4.1"},
		{"openai", "gpt-4.1-mini"},
		{"openai", "gpt-4.1-nano"},
	}
}

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
	// InsecureSkipVerify : Skips certificate verification. The public
	// endpoints present ordinary certificates, so this should stay false; it
	// exists only for the internal endpoints, whose certificates come from an
	// internal authority.
	InsecureSkipVerify bool
}

// Environment : Zoho Platform AI, as one place to send a prompt.
type Environment struct {
	cfg    Config
	logger *slog.Logger
	http   *http.Client

	tokenState
}

// New : Builds an Environment, applying defaults and reporting missing
// credentials.
func New(cfg Config, logger *slog.Logger) (*Environment, error) {
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

	return &Environment{
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
}

// Name : Returns the provider's name.
func (p *Environment) Name() string { return "platformai" }

// Run : Sends the prompt and streams the reply. See environment.Environment for the
// contract it follows.
//
// The service answers in one piece, so the stream is the reply and nothing
// else. It deliberately sends no progress of its own: an update should be
// something that actually happened, and a phrase this package made up —
// "Let me look into that" before every answer — is filler. Read aloud on
// every question it grates, and it is worse than silence because it sounds
// like an answer beginning.
//
// The client shows that the server is working without needing to be told. When a
// provider has real progress to report, such as an agent loop naming the
// tool it is using, that is what an update is for.
func (p *Environment) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	// A continuation carries no new question: the person asked once, tools
	// ran, and the model is being asked to go on from what came back. So an
	// empty prompt is refused only when there is no history either, which is
	// a request with nothing in it at all.
	if strings.TrimSpace(req.Prompt) == "" && len(req.History) == 0 {
		return nil, environment.ErrEmptyPrompt
	}

	ch := make(chan environment.Message)
	go func() {
		defer close(ch)

		started := time.Now()
		got, err := p.chat(ctx, req)
		if err != nil {
			// A cancelled chat is the user's doing, not a failure worth
			// reporting to them.
			if ctx.Err() != nil {
				return
			}
			p.logger.ErrorContext(ctx, "platform ai call failed",
				slog.Duration("after", time.Since(started)),
				slog.Any("error", err))
			f := classify(err)
			send(ctx, ch, environment.Failure(f.Sentence(), string(f.Code), f.Full()))
			return
		}

		if len(got.ToolCalls) > 0 {
			names := make([]string, 0, len(got.ToolCalls))
			for _, c := range got.ToolCalls {
				names = append(names, c.Name)
			}
			p.logger.InfoContext(ctx, "platform ai asked for tools",
				slog.Duration("after", time.Since(started)),
				slog.Any("tools", names))
			send(ctx, ch, environment.ToolCalls(got.ToolCalls))
			return
		}

		p.logger.InfoContext(ctx, "platform ai answered",
			slog.Duration("after", time.Since(started)),
			slog.Int("reply_bytes", len(got.Text)))
		send(ctx, ch, environment.Final(got.Text))
	}()

	return ch, nil
}

// classify : Sorts a failure into a code, keeping what the service said.
//
// The sentence a listener hears comes from the code, not from the service.
// A service's own words are frequently a code rather than a sentence —
// INVALID_OAUTHTOKEN means nothing read aloud to somebody waiting — and even
// when they are prose they are written for whoever integrates with it. What
// they are good for is the detail, where being exact is the whole point.
func classify(err error) *failure.Error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return failure.FromStatus(apiErr.Status, apiErr.Message, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failure.New(failure.Timeout, err.Error(), err)
	}
	return failure.New(failure.Unreachable, err.Error(), err)
}

// send : Delivers a message, reporting false if ctx ends before the caller
// receives it.
func send(ctx context.Context, ch chan<- environment.Message, msg environment.Message) bool {
	select {
	case ch <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}

// Provider implements the interface the runner depends on.
var _ environment.Environment = (*Environment)(nil)
