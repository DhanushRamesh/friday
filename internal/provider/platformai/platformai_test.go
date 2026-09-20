package platformai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DhanushRamesh/friday/internal/logging"
	"github.com/DhanushRamesh/friday/internal/provider"
	"github.com/DhanushRamesh/friday/internal/provider/platformai"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// fakeService : Stands in for Platform AI, recording what it was sent.
type fakeService struct {
	server *httptest.Server

	tokenCalls atomic.Int32
	chatCalls  atomic.Int32

	// lastChatBody : The most recent chat request body.
	lastChatBody atomic.Value
	// lastAuth : The Authorization header of the most recent chat call.
	lastAuth atomic.Value
	// lastPortal : The portal_id header of the most recent chat call.
	lastPortal atomic.Value

	// chatHandler : Writes the chat reply. Defaults to a plain-string answer.
	chatHandler func(w http.ResponseWriter, body []byte)
	// tokenHandler : Writes the token reply.
	tokenHandler func(w http.ResponseWriter)
	// expiresIn : Lifetime advertised for issued tokens.
	expiresIn int
}

// newFakeService : Starts a fake Platform AI and returns it with a Config
// pointing at it.
func newFakeService(t *testing.T) (*fakeService, platformai.Config) {
	t.Helper()

	f := &fakeService{expiresIn: 3600}
	mux := http.NewServeMux()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokenCalls.Add(1)
		if f.tokenHandler != nil {
			f.tokenHandler(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-abc",
			"expires_in":   f.expiresIn,
		})
	})

	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		f.chatCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		f.lastChatBody.Store(string(body))
		f.lastAuth.Store(r.Header.Get("Authorization"))
		f.lastPortal.Store(r.Header.Get("portal_id"))

		if f.chatHandler != nil {
			f.chatHandler(w, body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"messages":[{"content":"You have four open merge requests."}]}}`))
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	return f, platformai.Config{
		ClientID:        "client",
		ClientSecret:    logging.Secret("secret"),
		RefreshToken:    logging.Secret("refresh"),
		PortalID:        "TestPortal",
		TokenURL:        f.server.URL + "/token",
		ChatURL:         f.server.URL + "/chat",
		WorkingInterval: time.Hour, // out of the way unless a test wants it
	}
}

// collect : Drains a stream, returning every message.
func collect(t *testing.T, ch <-chan provider.Message) []provider.Message {
	t.Helper()
	var got []provider.Message
	for msg := range ch {
		got = append(got, msg)
	}
	return got
}

// run : Starts a run, failing the test if it could not be started.
func run(t *testing.T, p *platformai.Provider, prompt string) []provider.Message {
	t.Helper()
	ch, err := p.Run(context.Background(), provider.Request{Prompt: prompt})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return collect(t, ch)
}

// newProvider : Builds a Provider against the fake service.
func newProvider(t *testing.T, cfg platformai.Config) *platformai.Provider {
	t.Helper()
	p, err := platformai.New(cfg, discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// A run acknowledges at once, then answers. The acknowledgement is what a
// listener hears while the call is outstanding.
func TestRunAcknowledgesThenAnswers(t *testing.T) {
	_, cfg := newFakeService(t)
	got := run(t, newProvider(t, cfg), "check my merge requests")

	if len(got) < 2 {
		t.Fatalf("got %d messages, want an acknowledgement and an answer: %+v", len(got), got)
	}
	if got[0].Kind != provider.KindUpdate {
		t.Errorf("first message kind = %q, want update", got[0].Kind)
	}
	if got[0].Text == "" {
		t.Error("the acknowledgement has no text to speak")
	}

	last := got[len(got)-1]
	if last.Kind != provider.KindFinal {
		t.Fatalf("last message kind = %q, want final", last.Kind)
	}
	if last.Text != "You have four open merge requests." {
		t.Errorf("answer = %q, want the service's reply", last.Text)
	}
}

// The request must carry what the service requires: the prompt, the model, the
// system prompt in the context field, and the portal and token headers.
func TestRequestIsShapedForTheService(t *testing.T) {
	f, cfg := newFakeService(t)
	cfg.Model = "claude-sonnet-4-6"
	cfg.SystemPrompt = "answer in spoken sentences"

	run(t, newProvider(t, cfg), "what is the time")

	body, _ := f.lastChatBody.Load().(string)
	var sent struct {
		Vendor   string `json:"ai_vendor"`
		Model    string `json:"model"`
		Context  string `json:"context"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("unmarshal request %q: %v", body, err)
	}

	if sent.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q", sent.Model)
	}
	if sent.Vendor == "" {
		t.Error("no vendor sent")
	}
	// The API names the system prompt "context", not "content".
	if sent.Context != "answer in spoken sentences" {
		t.Errorf("context = %q, want the system prompt", sent.Context)
	}
	if len(sent.Messages) != 1 || sent.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v, want one user message", sent.Messages)
	}
	if sent.Messages[0].Content != "what is the time" {
		t.Errorf("prompt = %q", sent.Messages[0].Content)
	}

	if auth, _ := f.lastAuth.Load().(string); auth != "Zoho-oauthtoken token-abc" {
		t.Errorf("Authorization = %q, want the issued token", auth)
	}
	if portal, _ := f.lastPortal.Load().(string); portal != "TestPortal" {
		t.Errorf("portal_id = %q", portal)
	}
}

// Content arrives either as a plain string or as blocks of text; both must
// read the same.
func TestContentBlocksAreJoined(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		w.Write([]byte(`{"data":{"messages":[{"content":[{"text":"You have "},{"text":"four open "},{"text":"merge requests."}]}]}}`))
	}

	got := run(t, newProvider(t, cfg), "check")

	last := got[len(got)-1]
	if last.Kind != provider.KindFinal {
		t.Fatalf("last kind = %q, want final", last.Kind)
	}
	if last.Text != "You have four open merge requests." {
		t.Errorf("answer = %q, want the blocks joined", last.Text)
	}
}

// A token is fetched once and reused, rather than obtained per request.
func TestTokenIsCachedAcrossRuns(t *testing.T) {
	f, cfg := newFakeService(t)
	p := newProvider(t, cfg)

	for i := 0; i < 3; i++ {
		run(t, p, "again")
	}

	if got := f.tokenCalls.Load(); got != 1 {
		t.Errorf("token fetched %d times, want 1", got)
	}
	if got := f.chatCalls.Load(); got != 3 {
		t.Errorf("chat called %d times, want 3", got)
	}
}

// A token close to expiry must be replaced, not used until it fails.
func TestTokenIsRefreshedBeforeExpiry(t *testing.T) {
	f, cfg := newFakeService(t)
	f.expiresIn = 30 // shorter than the refresh margin, so never reusable
	p := newProvider(t, cfg)

	run(t, p, "one")
	run(t, p, "two")

	if got := f.tokenCalls.Load(); got != 2 {
		t.Errorf("token fetched %d times, want one per call when near expiry", got)
	}
}

// A message the service wrote is meant to be read, so it reaches the user.
func TestServiceErrorIsPassedOn(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"You have exceeded your quota for today."}}`))
	}

	got := run(t, newProvider(t, cfg), "too much")

	last := got[len(got)-1]
	if last.Kind != provider.KindError {
		t.Fatalf("last kind = %q, want error", last.Kind)
	}
	if last.Text != "You have exceeded your quota for today." {
		t.Errorf("error text = %q, want the service's own message", last.Text)
	}
}

// An unexpected failure must not put transport or decoding detail into
// something that will be spoken aloud.
func TestUnexpectedFailureIsDescribedInGeneralTerms(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<html>gateway exploded at 10.0.0.7:8443</html>`))
	}

	got := run(t, newProvider(t, cfg), "boom")

	last := got[len(got)-1]
	if last.Kind != provider.KindError {
		t.Fatalf("last kind = %q, want error", last.Kind)
	}
	for _, leak := range []string{"10.0.0.7", "html", "500"} {
		if strings.Contains(last.Text, leak) {
			t.Errorf("error text %q leaks internal detail (%q)", last.Text, leak)
		}
	}
}

// An empty reply is a failure, not an answer of nothing.
func TestEmptyReplyIsAFailure(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		w.Write([]byte(`{"data":{"messages":[{"content":"   "}]}}`))
	}

	got := run(t, newProvider(t, cfg), "say nothing")

	if last := got[len(got)-1]; last.Kind != provider.KindError {
		t.Errorf("last kind = %q, want error for an empty reply", last.Kind)
	}
}

// A long call must keep the listener company rather than leaving silence.
func TestReassuranceWhileWaiting(t *testing.T) {
	f, cfg := newFakeService(t)
	cfg.WorkingInterval = 30 * time.Millisecond
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		time.Sleep(120 * time.Millisecond)
		w.Write([]byte(`{"data":{"messages":[{"content":"done"}]}}`))
	}

	got := run(t, newProvider(t, cfg), "slow question")

	updates := 0
	for _, m := range got {
		if m.Kind == provider.KindUpdate {
			updates++
		}
	}
	if updates < 2 {
		t.Errorf("got %d updates, want the acknowledgement plus reassurance: %+v", updates, got)
	}
	if last := got[len(got)-1]; last.Kind != provider.KindFinal {
		t.Errorf("last kind = %q, want final", last.Kind)
	}
}

// Cancelling ends the run without a result, as the contract requires.
func TestCancellationEndsTheRun(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		time.Sleep(3 * time.Second)
		w.Write([]byte(`{"data":{"messages":[{"content":"too late"}]}}`))
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := newProvider(t, cfg)
	ch, err := p.Run(ctx, provider.Request{Prompt: "stop me"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	<-ch // the acknowledgement
	cancel()

	for msg := range ch {
		if msg.Kind == provider.KindFinal {
			t.Error("a cancelled run produced a final message")
		}
	}
}

func TestRunRejectsEmptyPrompt(t *testing.T) {
	_, cfg := newFakeService(t)
	p := newProvider(t, cfg)

	for _, prompt := range []string{"", "   "} {
		ch, err := p.Run(context.Background(), provider.Request{Prompt: prompt})
		if err == nil {
			t.Errorf("Run(%q): want an error", prompt)
		}
		if ch != nil {
			t.Errorf("Run(%q): returned a channel alongside an error", prompt)
		}
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	full := platformai.Config{
		ClientID:     "id",
		ClientSecret: logging.Secret("secret"),
		RefreshToken: logging.Secret("refresh"),
		PortalID:     "portal",
	}

	cases := map[string]func(*platformai.Config){
		"client_id":     func(c *platformai.Config) { c.ClientID = "" },
		"client_secret": func(c *platformai.Config) { c.ClientSecret = "" },
		"refresh_token": func(c *platformai.Config) { c.RefreshToken = "" },
		"portal_id":     func(c *platformai.Config) { c.PortalID = "" },
	}
	for name, remove := range cases {
		cfg := full
		remove(&cfg)
		_, err := platformai.New(cfg, discard())
		if err == nil {
			t.Errorf("New without %s: want an error", name)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name the missing %s", err, name)
		}
	}

	if _, err := platformai.New(full, discard()); err != nil {
		t.Errorf("New with every credential: %v", err)
	}
}

// Credentials must not be reachable through a log or a printed config.
func TestCredentialsCannotBePrinted(t *testing.T) {
	cfg := platformai.Config{
		ClientSecret: logging.Secret("super-secret-value"),
		RefreshToken: logging.Secret("refresh-token-value"),
	}

	rendered := strings.Join([]string{
		cfg.ClientSecret.String(),
		cfg.RefreshToken.String(),
	}, " ")
	for _, secret := range []string{"super-secret-value", "refresh-token-value"} {
		if strings.Contains(rendered, secret) {
			t.Errorf("a credential printed as %q", rendered)
		}
	}
}

// The token endpoint takes the client secret as a query parameter, and Go's
// transport errors carry the whole URL. Wrapped as they come, they write the
// credential into the log in plain text.
func TestTransportFailureDoesNotLeakCredentials(t *testing.T) {
	const (
		secret  = "super-secret-client-value"
		refresh = "super-secret-refresh-value"
	)

	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// A port with nothing on it, so the call fails at the transport.
	cfg := platformai.Config{
		ClientID:        "client",
		ClientSecret:    logging.Secret(secret),
		RefreshToken:    logging.Secret(refresh),
		PortalID:        "portal",
		TokenURL:        "http://127.0.0.1:1/token",
		ChatURL:         "http://127.0.0.1:1/chat",
		Timeout:         2 * time.Second,
		WorkingInterval: time.Hour,
	}

	p, err := platformai.New(cfg, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := run(t, p, "will not connect")

	last := got[len(got)-1]
	if last.Kind != provider.KindError {
		t.Fatalf("last kind = %q, want error", last.Kind)
	}

	for name, value := range map[string]string{"client secret": secret, "refresh token": refresh} {
		if strings.Contains(logs.String(), value) {
			t.Errorf("the %s was written to the log:\n%s", name, logs.String())
		}
		if strings.Contains(last.Text, value) {
			t.Errorf("the %s reached the user-facing message: %q", name, last.Text)
		}
	}

	// The failure must still be diagnosable.
	if !strings.Contains(logs.String(), "127.0.0.1:1") {
		t.Errorf("the log does not say what could not be reached:\n%s", logs.String())
	}
}
