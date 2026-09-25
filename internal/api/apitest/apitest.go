// Package apitest : Builds a running the server API for the handler tests of the
// modules beneath internal/api.
//
// It assembles the real server rather than mounting one module on a bare
// router, so a module's tests exercise it exactly as it is served: behind the
// real middleware stack, the real authentication, and the real routes. A
// module cannot then pass its own tests while being mounted wrongly.
//
// Every module's tests share this one fixture, so that adding a dependency to
// The server is a change in one place rather than in each of them. The tests
// themselves live beside the code they cover and are external test packages
// (package chats_test and so on), which is what lets this package import the
// API without a cycle.
package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/DhanushRamesh/personal-assistant/internal/api"
	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/auth"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/chat/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/provider"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
)

// Username and Password : The account every environment is given.
const (
	Username = "tester"
	Password = "correct horse battery staple"
)

// Identifiers that are well formed but belong to nothing.
//
// A test that expects a refusal uses these so that the refusal is the
// endpoint's doing and not the identifier failing validation first, which
// would pass whatever the endpoint did.
var (
	SomeChatID    = chat.NewID()
	SomeSessionID = chat.NewSessionID()
	SomeClientID  = chat.NewClientID()
)

// ErrStorage : A storage failure used to check that internal errors are
// logged but never returned to a caller. Its text looks like a credential on
// purpose.
var ErrStorage = errors.New("storage exploded: dsn=user:password@tcp(db)/assistant")

// fixtureHash : The test account's password hash, computed once at bcrypt's
// cheapest cost.
//
// Deliberately not auth.HashPassword: that uses the configured cost, which is
// a fifth of a second and more than two seconds under the race detector, and
// every environment built here creates an account. The cost is a property of
// the hash, so a login against this one is cheap to verify however
// auth.PasswordCost is set. A test that needs the configured cost to apply —
// only the login path for an unknown username does — lowers it itself.
var fixtureHash = sync.OnceValue(func() string {
	hash, err := bcrypt.GenerateFromPassword([]byte(Password), bcrypt.MinCost)
	if err != nil {
		panic("apitest: cannot hash the fixture password: " + err.Error())
	}
	return string(hash)
})

// Pinger : Stands in for a database handle.
type Pinger struct{ Err error }

// Ping : Returns the configured error.
func (p Pinger) Ping(context.Context) error { return p.Err }

// Options : What to vary about an environment. The zero value gives a stub
// provider and a reachable database.
type Options struct {
	// Provider : Answers chats. Nil selects a plain stub.
	Provider provider.Provider
	// DB : What the readiness endpoint checks. Nil selects a reachable one.
	DB api.Pinger
	// AllowCrossOrigin : Whether the server answers a browser's
	// cross-origin checks, as it does outside production.
	AllowCrossOrigin bool
}

// Env : A server, its dependencies, and a client already logged in.
type Env struct {
	// Server : The assembled API, with every module mounted.
	Server *api.Server
	// Repo : The store behind it, for arranging state a request cannot.
	Repo *memory.Repository
	// Runner : The runner executing its chats.
	Runner *runner.Runner
	// Logger and Logs : Where the server logs, and what it has written.
	Logger *slog.Logger
	Logs   *bytes.Buffer
	// Token : The bearer token the request helpers present by default.
	Token string
	// User and Client : Who that token belongs to.
	User   *chat.User
	Client *chat.Client
}

// New : Builds an environment with a stub provider and a reachable database.
func New(t *testing.T) *Env {
	t.Helper()
	return NewWith(t, Options{})
}

// NewWith : Builds an environment from opts.
func NewWith(t *testing.T, opts Options) *Env {
	t.Helper()

	if opts.Provider == nil {
		opts.Provider = &provider.Stub{}
	}
	if opts.DB == nil {
		opts.DB = Pinger{}
	}

	logs := &bytes.Buffer{}
	logger, err := logging.New(logs, logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	repo := memory.New()
	bus := events.NewBus(logger.Logger)
	t.Cleanup(bus.Close)

	chatRunner, err := runner.New(runner.Options{
		Repository: repo,
		Messages:   repo,
		Provider:   opts.Provider,
		Logger:     logger.Logger,
		Publisher:  bus,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = chatRunner.Shutdown(ctx)
	})

	e := &Env{
		Server: api.New(api.Options{
			Logger:           logger.Logger,
			DB:               opts.DB,
			Chats:            repo,
			Runner:           chatRunner,
			Events:           bus,
			AllowCrossOrigin: opts.AllowCrossOrigin,
		}),
		Repo:   repo,
		Runner: chatRunner,
		Logger: logger.Logger,
		Logs:   logs,
	}
	e.register(t)
	return e
}

// register : Creates the test account and a client holding a usable token.
//
// Done against the store rather than through the login endpoint, so that a
// module's tests do not fail because logging in is broken; authn's own tests
// cover that path.
func (e *Env) register(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	user, err := chat.NewUser(Username, fixtureHash())
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := e.Repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	token, tokenHash, err := auth.NewToken()
	if err != nil {
		t.Fatalf("auth.NewToken: %v", err)
	}
	client, err := chat.NewClient(user.ID, "test client", tokenHash, chat.ChannelDirect)
	if err != nil {
		t.Fatalf("chat.NewClient: %v", err)
	}
	if err := e.Repo.CreateClient(ctx, client); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}

	session := chat.NewSession(user.ID, "")
	if err := e.Repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := e.Repo.SetActiveSession(ctx, user.ID, client.ID, session.ID); err != nil {
		t.Fatalf("SetActiveSession: %v", err)
	}
	client.ActiveSessionID = session.ID

	e.Token, e.User, e.Client = token, user, client
}

// Serve : Answers r, without adding anything to it.
func (e *Env) Serve(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.Server.ServeHTTP(rec, r)
	return rec
}

// Do : Issues a request as the registered client. An empty body sends none.
func (e *Env) Do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.Serve(e.request(method, path, body, "Bearer "+e.Token))
}

// Get : Issues a GET as the registered client.
func (e *Env) Get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return e.Do(t, http.MethodGet, path, "")
}

// Anonymous : Issues a request carrying the given Authorization header, which
// may be empty to send none.
func (e *Env) Anonymous(t *testing.T, method, path, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	return e.Serve(e.request(method, path, "", authorization))
}

// AsBody : Issues a request with a body as the holder of the given token.
//
// Separate from As rather than an optional argument, because most calls have
// no body and threading an empty string through every one of them reads
// worse than two functions.
func (e *Env) AsBody(t *testing.T, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.Serve(e.request(method, path, body, "Bearer "+token))
}

// As : Issues a request as the holder of the given token.
func (e *Env) As(t *testing.T, token, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	return e.Anonymous(t, method, path, "Bearer "+token)
}

// request : Builds a request, setting the headers a caller would.
func (e *Env) request(method, path, body, authorization string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	return r
}

// Preflight : Issues the request a browser sends before a cross-origin call
// it is unsure about.
func (e *Env) Preflight(t *testing.T, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodOptions, path, nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", method)
	r.Header.Set("Access-Control-Request-Headers", "authorization, last-event-id")
	return e.Serve(r)
}

// LoginRaw : Posts a login body verbatim, so a malformed one can be tested.
func (e *Env) LoginRaw(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.Serve(e.request(http.MethodPost, "/v1/auth/login", body, ""))
}

// Login : Logs the test account in from a newly named client, which speaks
// for itself as a direct one.
func (e *Env) Login(t *testing.T, clientName string) authn.LoginResponse {
	t.Helper()
	return e.LoginOn(t, clientName, "")
}

// LoginOn : Logs in declaring a channel, so a test can hold a token that
// belongs to something with a microphone.
func (e *Env) LoginOn(t *testing.T, clientName, channel string) authn.LoginResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": Username, "password": Password, "client_name": clientName,
		"channel": channel,
	})
	rec := e.LoginRaw(t, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("login: status %d, want 201: %s", rec.Code, rec.Body)
	}
	var out authn.LoginResponse
	e.Decode(t, rec, &out)
	return out
}

// Live : Serves the API on a real listener and returns its base URL.
//
// Needed by anything reading a response progressively: httptest.ResponseRecorder
// buffers everything until the handler returns, which a stream never does.
func (e *Env) Live(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(e.Server)
	t.Cleanup(ts.Close)
	return ts.URL
}

// Decode : Parses a JSON response body.
func (e *Env) Decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
}

// Records : Parses every log record the server has written.
func (e *Env) Records(t *testing.T) []map[string]any {
	t.Helper()
	return Records(t, e.Logs)
}

// Records : Parses every log record written to buf.
func Records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// CreateChat : Submits a prompt and returns the created chat. The path
// carries any query, such as a wait.
func (e *Env) CreateChat(t *testing.T, path, prompt string) views.Chat {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	rec := e.Do(t, http.MethodPost, path, string(body))
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}
	var view views.Chat
	e.Decode(t, rec, &view)
	return view
}

// CreateIn : Submits a prompt continuing the named session, or the active one
// when sessionID is empty.
func (e *Env) CreateIn(t *testing.T, sessionID, prompt, query string) views.Chat {
	t.Helper()
	body := map[string]string{"prompt": prompt}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	encoded, _ := json.Marshal(body)

	rec := e.Do(t, http.MethodPost, "/v1/chats"+query, string(encoded))
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("create: status %d: %s", rec.Code, rec.Body)
	}
	var view views.Chat
	e.Decode(t, rec, &view)
	return view
}

// AwaitStatus : Polls a chat until it reaches one of the given statuses.
func (e *Env) AwaitStatus(t *testing.T, id string, want ...string) views.Chat {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var view views.Chat

	for time.Now().Before(deadline) {
		rec := e.Get(t, "/v1/chats/"+id)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET chat: status %d, body %s", rec.Code, rec.Body)
		}
		e.Decode(t, rec, &view)
		for _, w := range want {
			if view.Status == w {
				return view
			}
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("chat %s stayed %q, waiting for one of %v", id, view.Status, want)
	return view
}

// RecordingProvider : A provider that records the history it was given, so a
// test can check what a model would actually have seen.
type RecordingProvider struct {
	// Delay : How long to think before answering.
	Delay time.Duration

	mu      sync.Mutex
	history []provider.Turn
	prompt  string
}

// Name : Identifies the provider.
func (r *RecordingProvider) Name() string { return "recording" }

// Run : Records the request's history and answers after Delay.
func (r *RecordingProvider) Run(ctx context.Context, req provider.Request) (<-chan provider.Message, error) {
	r.mu.Lock()
	r.history = append([]provider.Turn(nil), req.History...)
	r.prompt = req.Prompt
	r.mu.Unlock()

	ch := make(chan provider.Message)
	go func() {
		defer close(ch)
		if r.Delay > 0 {
			select {
			case <-time.After(r.Delay):
			case <-ctx.Done():
				return
			}
		}
		select {
		case ch <- provider.Final("an answer"):
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// LastHistory : Returns the history given to the most recent run.
func (r *RecordingProvider) LastHistory() []provider.Turn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]provider.Turn(nil), r.history...)
}

// LastPrompt : Returns the prompt given to the most recent run.
func (r *RecordingProvider) LastPrompt() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prompt
}
