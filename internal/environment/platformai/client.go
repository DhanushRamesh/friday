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

	"github.com/DhanushRamesh/personal-assistant/internal/environment"
)

// tokenRefreshMargin : How long before expiry a cached token is replaced,
// so a request does not begin with a token that expires mid-flight.
const tokenRefreshMargin = 60 * time.Second

// oauthResponse : The token endpoint's reply.
type oauthResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// chatMessage : One message in a conversation.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	// ToolCalls : On an assistant message that asked for tools instead of
	// answering. Carries no content when set.
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
	// ToolResults : On a tool message, the answers it carries.
	//
	// An array on one message rather than OpenAI's one message per answer
	// with a tool_call_id. This endpoint wants them together, and sending
	// them apart makes the vendor behind it reject the whole conversation:
	// "tool_use ids were found without tool_result blocks immediately
	// after".
	ToolResults []wireToolResult `json:"tool_results,omitempty"`
}

// wireToolResult : One answer, in the shape this endpoint expects.
type wireToolResult struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

// wireToolCall : A tool call in the shape this endpoint uses, which is
// OpenAI's.
type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		// Arguments : JSON, as a string. The service sends it that way and
		// expects it back that way.
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// wireTool : A tool offered to the model.
type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
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
	Tools    []wireTool    `json:"tools,omitempty"`
}

// chatResponse : The reply to a chat call. Content is either a plain string or
// an array of blocks carrying text.
type chatResponse struct {
	Data struct {
		Messages []struct {
			Content   json.RawMessage `json:"content"`
			ToolCalls []wireToolCall  `json:"tool_calls"`
		} `json:"messages"`
	} `json:"data"`
}

// reply : What one call came back with: words, or a request to run tools.
type reply struct {
	Text      string
	ToolCalls []environment.ToolCall
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
// The whole refresh is serialised, so concurrent chats share one token rather
// than each fetching their own.
func (p *Environment) accessToken(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	if p.token != "" && time.Now().Before(p.tokenExpiry.Add(-tokenRefreshMargin)) {
		return p.token, nil
	}
	return p.mintToken(ctx)
}

// forgetToken : Drops the cached token, so the next call mints a new one.
//
// Called when the service refuses one that had not expired as far as we
// knew. That happens: Zoho invalidates an access token when another is
// issued for the same client, so authorising from anywhere else — or a
// second copy of the server running — silently revokes ours long before the
// expiry we calculated.
func (p *Environment) forgetToken() {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	p.token = ""
	p.tokenExpiry = time.Time{}
}

// mintToken : Exchanges the refresh token for a new access token. The
// caller holds tokenMu.
func (p *Environment) mintToken(ctx context.Context) (string, error) {

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
func (p *Environment) chat(ctx context.Context, ask environment.Request) (reply, error) {
	got, status, err := p.attemptChat(ctx, ask)
	if err == nil {
		return got, nil
	}

	// A token can be refused before we believe it has expired, so a
	// refusal is not final on the first try: drop the cached one, mint a
	// fresh one and go again. Once only — a refresh token that has itself
	// been revoked would otherwise loop.
	if status == http.StatusUnauthorized {
		p.forgetToken()
		got, _, err = p.attemptChat(ctx, ask)
		return got, err
	}
	return reply{}, err
}

// attemptChat : One try, returning the HTTP status alongside the failure
// so the caller can tell a refused token from anything else.
// withSummary : The system prompt with the condensed earlier conversation
// appended, or unchanged when there is none.
//
// It goes here rather than among the messages because this endpoint keeps the
// system prompt in a field of its own, and because a condensation is not
// something either side said.
func withSummary(prompt, summary string) string {
	if strings.TrimSpace(summary) == "" {
		return prompt
	}
	return prompt + "\n\nEarlier in this conversation, summarised:\n" + summary
}

func (p *Environment) attemptChat(ctx context.Context, ask environment.Request) (reply, int, error) {
	token, err := p.accessToken(ctx)
	if err != nil {
		// A refusal minting the token is the same problem as a refusal
		// using one, and is reported the same way.
		return reply{}, statusOf(err), err
	}

	messages := make([]chatMessage, 0, len(ask.History)+1)
	for _, turn := range ask.History {
		messages = append(messages, asWire(turn)...)
	}
	// Only when there is one. A continuation after tools ran has no new
	// question, and appending an empty user message would have the model
	// answer a thing nobody said.
	if strings.TrimSpace(ask.Prompt) != "" {
		messages = append(messages, chatMessage{
			Role:    string(environment.RoleUser),
			Content: ask.Prompt,
		})
	}

	vendor, model := p.cfg.Vendor, p.cfg.Model
	if ask.Model != "" {
		vendor, model = ask.Vendor, ask.Model
	}

	prompt := p.cfg.SystemPrompt
	if ask.SystemPrompt != "" {
		prompt = ask.SystemPrompt
	}

	body, err := json.Marshal(chatRequest{
		Vendor:   vendor,
		Model:    model,
		Context:  withSummary(prompt, ask.Summary),
		Messages: messages,
		Tools:    asWireTools(ask.Tools),
	})
	if err != nil {
		return reply{}, 0, fmt.Errorf("platformai: building chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.ChatURL, bytes.NewReader(body))
	if err != nil {
		return reply{}, 0, fmt.Errorf("platformai: building chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Zoho-oauthtoken "+token)
	req.Header.Set("portal_id", p.cfg.PortalID)
	req.Header.Set("chat-response-format", "msg-format")

	resp, err := p.http.Do(req)
	if err != nil {
		return reply{}, 0, fmt.Errorf("platformai: sending chat request: %w", scrubURL(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return reply{}, resp.StatusCode, fmt.Errorf("platformai: reading chat response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Data.Messages) == 0 {
		if msg := errorMessage(raw); msg != "" {
			return reply{}, resp.StatusCode, &APIError{Message: msg, Status: resp.StatusCode}
		}
		return reply{}, resp.StatusCode,
			fmt.Errorf("platformai: chat response was not usable (HTTP %d)", resp.StatusCode)
	}

	first := parsed.Data.Messages[0]

	// Tool calls before text. A model that asks for a tool sometimes sends a
	// sentence alongside it, and that sentence describes what it is about to
	// do rather than what happened -- reading it out and stopping would be
	// telling the person about work that never ran.
	if calls := fromWireCalls(first.ToolCalls); len(calls) > 0 {
		return reply{ToolCalls: calls}, resp.StatusCode, nil
	}

	text := contentText(first.Content)
	if strings.TrimSpace(text) == "" {
		return reply{}, resp.StatusCode, fmt.Errorf("platformai: the reply was empty")
	}
	return reply{Text: text}, resp.StatusCode, nil
}

// statusOf : The HTTP status a failure carries, or zero if it carries
// none.
func statusOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
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
	// The chat endpoint nests it: {"error": {"message": "..."}}, and nests it
	// again when the failure came from the model's own vendor rather than
	// from this service. The inner one says what is actually wrong with the
	// request; the outer one says only that something was.
	var nested struct {
		Error struct {
			Message  string `json:"message"`
			APIError struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"api_error"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &nested) == nil {
		if msg := strings.TrimSpace(nested.Error.APIError.Message); msg != "" {
			return msg
		}
		if msg := strings.TrimSpace(nested.Error.Message); msg != "" {
			return msg
		}
	}

	// The token endpoint does not, and puts a string where the other puts an
	// object: {"error": "Access Denied", "error_description": "..."}. Reading
	// only the first shape threw away the one sentence worth having, leaving
	// "token response was not usable" in its place -- which says nothing to
	// the person reading it and nothing to a model asked what went wrong.
	var flat struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &flat) == nil {
		if msg := strings.TrimSpace(flat.Description); msg != "" {
			return msg
		}
		if msg := strings.TrimSpace(flat.Error); msg != "" {
			return msg
		}
	}

	return ""
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

// asWire : One remembered turn as the messages this endpoint expects.
//
// A turn carrying tool results becomes a single message holding all of them,
// which is what this endpoint wants. Splitting them across a message each, as
// OpenAI's shape does, leaves the vendor behind it complaining that a
// tool_use had no tool_result after it.
func asWire(turn environment.Turn) []chatMessage {
	if len(turn.ToolResults) > 0 {
		msg := chatMessage{Role: "tool"}
		for _, r := range turn.ToolResults {
			msg.ToolResults = append(msg.ToolResults, wireToolResult{
				ID:      r.ID,
				Type:    "function",
				Content: r.Content,
			})
		}
		return []chatMessage{msg}
	}

	msg := chatMessage{Role: string(turn.Role), Content: turn.Text}
	for _, c := range turn.ToolCalls {
		wire := wireToolCall{ID: c.ID, Type: "function"}
		wire.Function.Name = c.Name
		wire.Function.Arguments = c.Arguments
		msg.ToolCalls = append(msg.ToolCalls, wire)
	}
	return []chatMessage{msg}
}

// asWireTools : The offered tools in the shape the endpoint expects.
func asWireTools(tools []environment.ToolSpec) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, 0, len(tools))
	for _, t := range tools {
		w := wireTool{Type: "function"}
		w.Function.Name = t.Name
		w.Function.Description = t.Description
		w.Function.Parameters = t.Parameters
		out = append(out, w)
	}
	return out
}

// fromWireCalls : The model's tool calls, in our own shape.
func fromWireCalls(calls []wireToolCall) []environment.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]environment.ToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, environment.ToolCall{
			ID:        c.ID,
			Name:      c.Function.Name,
			Arguments: c.Function.Arguments,
		})
	}
	return out
}
