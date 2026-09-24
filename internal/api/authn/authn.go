// Package authn : Establishes who is making a request, and issues the
// credential that lets them.
//
// It is the one module every other one depends on, because a handler that
// does not know the caller cannot decide what they may see. What it puts on
// the context — a user and the client they are using — is the only identity
// the rest of the API has.
package authn

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/auth"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// header : The header carrying a client's token.
const header = "Authorization"

// callerKey : The context key under which the authenticated caller is
// carried.
type callerKey struct{}

// Caller : Who is making a request: a user, and the client they are using.
//
// The user is the boundary — everything is owned by and visible to them. The
// client is only which credential was presented, and matters for where a
// prompt lands and which token to revoke.
type Caller struct {
	User   *chat.User
	Client *chat.Client
}

// Of : Returns who is making the request. Valid only inside a handler behind
// Require.
func Of(ctx context.Context) *Caller {
	c, _ := ctx.Value(callerKey{}).(*Caller)
	return c
}

// LoginRequest : The body of a request to log in.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// ClientName : What to call the client being logged in from, such as
	// "my phone". Optional.
	ClientName string `json:"client_name,omitempty"`
}

// LoginResponse : What logging in returns.
//
// The token appears here and nowhere else: only its hash is stored, so this
// response is the one chance to keep it.
type LoginResponse struct {
	Token  string       `json:"token"`
	User   views.User   `json:"user"`
	Client views.Client `json:"client"`
}

// Handler : Serves logging in, and guards everything that requires a token.
type Handler struct {
	httpx.Responder
	repo chat.Repository
}

// New : Builds the handler from the store it authenticates against.
func New(logger *slog.Logger, repo chat.Repository) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, repo: repo}
}

// Mount : Registers the endpoints that do not themselves require a token.
func (h *Handler) Mount(r chi.Router) {
	// Logging in is the one call that cannot present a token: it is what
	// issues one. A password stands in its place.
	r.Post("/v1/auth/login", h.Login)
}

// Login : Authenticates a user and registers the client they are logging in
// from, returning its token.
//
// Logging in and registering a client are one act: a token exists only for a
// client, and a client may be created only by someone who proved who they
// are. There is nothing to register separately.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req LoginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := h.repo.UserByUsername(ctx, req.Username)
	if errors.Is(err, chat.ErrNotFound) {
		// Spend what a real check costs. Answering an unknown username
		// faster than a wrong password tells whoever is guessing which
		// usernames exist.
		auth.DummyPasswordCheck()
		h.Logger.WarnContext(ctx, "login refused: no such user")
		Unauthorised(ctx, w, "That username or password is not correct.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "reading user", err)
		return
	}

	if !auth.PasswordMatches(user.PasswordHash, req.Password) {
		h.Logger.WarnContext(ctx, "login refused: wrong password",
			slog.String("user_id", user.ID))
		Unauthorised(ctx, w, "That username or password is not correct.")
		return
	}

	token, tokenHash, err := auth.NewToken()
	if err != nil {
		h.Fail(ctx, w, "issuing token", err)
		return
	}

	client, err := chat.NewClient(user.ID, req.ClientName, tokenHash)
	if errors.Is(err, chat.ErrClientNameTooLong) {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That client name is too long.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "registering client", err)
		return
	}
	if err := h.repo.CreateClient(ctx, client); err != nil {
		h.Fail(ctx, w, "registering client", err)
		return
	}

	// Somewhere to talk at once. A session belongs to the user, so a second
	// client joins whatever already exists rather than starting over.
	sessionID, err := chat.EnsureSession(ctx, h.repo, user.ID)
	if err != nil {
		h.Fail(ctx, w, "preparing a session", err)
		return
	}
	if err := h.repo.SetActiveSession(ctx, user.ID, client.ID, sessionID); err != nil {
		h.Fail(ctx, w, "activating session", err)
		return
	}
	client.ActiveSessionID = sessionID

	h.Logger.InfoContext(ctx, "client logged in",
		slog.String("user_id", user.ID),
		slog.String("client_id", client.ID))

	// The only moment the token exists outside the caller's hands.
	httpx.WriteJSON(ctx, w, http.StatusCreated, LoginResponse{
		Token:  token,
		User:   views.OfUser(user),
		Client: views.OfClient(*client, true),
	})
}

// Require : Authenticates the caller and refuses the request without a usable
// token.
//
// Every refusal reads the same and answers 401. Distinguishing a token never
// issued from one since revoked would tell whoever is guessing which of their
// guesses landed.
func (h *Handler) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		token := auth.BearerToken(r.Header.Get(header))
		if token == "" {
			Unauthorised(ctx, w, "Send your token as: Authorization: Bearer <token>.")
			return
		}
		if !auth.LooksLikeToken(token) {
			h.Logger.WarnContext(ctx, "rejected a malformed token")
			Unauthorised(ctx, w, "That token is not valid.")
			return
		}

		client, err := h.repo.ClientByTokenHash(ctx, auth.HashToken(token))
		switch {
		case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrRevoked):
			// Logged apart so a revocation is diagnosable, answered alike so
			// it is not discoverable.
			h.Logger.WarnContext(ctx, "rejected a token", slog.Any("reason", err))
			Unauthorised(ctx, w, "That token is not valid.")
			return
		case err != nil:
			h.Fail(ctx, w, "authenticating client", err)
			return
		}

		user, err := h.repo.GetUser(ctx, client.UserID)
		if err != nil {
			// The client outlived its user, so the token is worthless.
			h.Logger.WarnContext(ctx, "token belongs to a client with no user",
				slog.String("client_id", client.ID))
			Unauthorised(ctx, w, "That token is not valid.")
			return
		}

		ctx = context.WithValue(ctx, callerKey{}, &Caller{User: user, Client: client})
		ctx = logging.WithAttrs(ctx,
			slog.String("user_id", user.ID),
			slog.String("client_id", client.ID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Unauthorised : Refuses a request, naming the scheme a caller should use.
func Unauthorised(ctx context.Context, w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="assistant"`)
	httpx.WriteError(ctx, w, http.StatusUnauthorized, message)
}
