package platformai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DhanushRamesh/friday/internal/provider"
)

// tokenRefreshMargin : How long before expiry a cached token is replaced,
// so a request does not begin with a token that expires mid-flight.
const tokenRefreshMargin = 60 * time.Second

// oauthResponse : The token endpoint's reply.
type oauthResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// chatMessage : One message in a session.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest : The body of a chat call.
//
// The system prompt is carried in a field named context rather than content,
// which the API requires.
type chatRequest struct {
	Vendor   string        `json:"ai_vendor"`
	Model    string        `json:"model"`
	Context  string        `json:"context"`
	Messages []chatMessage `json:"messages"`
}

// chatResponse : The reply to a chat call. Content is either a plain string or
// an array of blocks carrying text.
type chatResponse struct {
	Data struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	} `json:"data"`
}

// APIError : A message the service itself returned, in its own error envelope.
//
// It is distinguished from transport and decoding failures because the service
// wrote it to be read, so it can be passed on to the user. Anything else is
// reported in general terms and the detail kept in the log.
type APIError struct {
	// Message : What the service said went wrong.
	Message string
	// Status : The HTTP status it came with.
	Status int
}

// Error : Describes the failure.
func (e *APIError) Error() string {
	return fmt.Sprintf("platformai: %s (HTTP %d)", e.Message, e.Status)
}

// accessToken : Returns a valid token, refreshing it when it is missing or
// close to expiry.
//
// The whole refresh is serialised, so concurrent tasks share one token rather
// than each fetching their own.
func (p *Provider) accessToken(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	if p.token != "" && time.Now().Before(p.tokenExpiry.Add(-tokenRefreshMargin)) {
		return p.token, nil
	}

	params := url.Values{}
	params.Set("grant_type", "refresh_token")
	params.Set("refresh_token", p.cfg.RefreshToken.Reveal())
	params.Set("client_id", p.cfg.ClientID)
	params.Set("client_secret", p.cfg.ClientSecret.Reveal())
	params.Set("redirect_uri", p.cfg.RedirectURI)
	params.Set("scope", p.cfg.Scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL+"?"+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("platformai: building token request: %w", err)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("platformai: requesting token: %w", scrubURL(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("platformai: reading token response: %w", err)
	}

	var parsed oauthResponse
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
		if msg := errorMessage(body); msg != "" {
			return "", &APIError{Message: msg, Status: resp.StatusCode}
		}
		return "", fmt.Errorf("platformai: token response was not usable (HTTP %d)", resp.StatusCode)
	}

	expiresIn := parsed.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	p.token = parsed.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)

	return p.token, nil
}

// chat : Sends a prompt, preceded by what was said earlier, and returns the
// assistant's reply.
func (p *Provider) chat(ctx context.Context, prompt string, history []provider.Turn) (string, error) {
	token, err := p.accessToken(ctx)
	if err != nil {
		return "", err
	}

	messages := make([]chatMessage, 0, len(history)+1)
	for _, turn := range history {
		messages = append(messages, chatMessage{Role: string(turn.Role), Content: turn.Text})
	}
	messages = append(messages, chatMessage{Role: string(provider.RoleUser), Content: prompt})

	body, err := json.Marshal(chatRequest{
		Vendor:   p.cfg.Vendor,
		Model:    p.cfg.Model,
		Context:  p.cfg.SystemPrompt,
		Messages: messages,
	})
	if err != nil {
		return "", fmt.Errorf("platformai: building chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.ChatURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("platformai: building chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Zoho-oauthtoken "+token)
	req.Header.Set("portal_id", p.cfg.PortalID)
	req.Header.Set("chat-response-format", "msg-format")

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("platformai: sending chat request: %w", scrubURL(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("platformai: reading chat response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Data.Messages) == 0 {
		if msg := errorMessage(raw); msg != "" {
			return "", &APIError{Message: msg, Status: resp.StatusCode}
		}
		return "", fmt.Errorf("platformai: chat response was not usable (HTTP %d)", resp.StatusCode)
	}

	text := contentText(parsed.Data.Messages[0].Content)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("platformai: the reply was empty")
	}
	return text, nil
}

// contentText : Reads a message's content, which arrives either as a plain
// string or as an array of blocks each carrying text.
func contentText(raw json.RawMessage) string {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}

	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			sb.WriteString(b.Text)
		}
		return sb.String()
	}

	return string(raw)
}

// errorMessage : Reads the message out of the service's error envelope, or
// returns empty if the body is not one.
func errorMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	return strings.TrimSpace(envelope.Error.Message)
}

// scrubURL : Removes the query string from a transport error.
//
// The token endpoint takes the client secret and refresh token as query
// parameters, and a *url.Error carries the whole URL. Wrapped and logged as it
// comes, that writes the credentials into the log in plain text. Redaction in
// internal/logging cannot help: it matches attribute keys, and this is a
// secret buried inside an error's text.
//
// The operation, host and underlying cause are kept, since those are what make
// the failure diagnosable.
func scrubURL(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}

	scrubbed := urlErr.URL
	if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		scrubbed = parsed.String()
	} else {
		// Unparseable, so keep nothing after the query marker.
		if i := strings.IndexByte(scrubbed, '?'); i >= 0 {
			scrubbed = scrubbed[:i]
		}
	}

	return fmt.Errorf("%s %s: %w", urlErr.Op, scrubbed, urlErr.Err)
}

// tokenState : Guards the cached access token.
type tokenState struct {
	tokenMu     sync.Mutex
	token       string
	tokenExpiry time.Time
}
