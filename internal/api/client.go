package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/friday/internal/auth"
	"github.com/DhanushRamesh/friday/internal/logging"
	"github.com/DhanushRamesh/friday/internal/task"
)

// authHeader : The header carrying a client's token.
//
// The token identifies the client and proves it is that client, so no separate
// identifier is sent alongside: one that could be presented without the other
// would be a name with no password.
const authHeader = "Authorization"

// clientKey : The context key under which the calling client is carried.
type clientKey struct{}

// requireClient : Authenticates the caller and refuses the request without a
// usable token.
//
// Every refusal reads the same and answers 401. Distinguishing a token that
// was never issued from one that was revoked would tell whoever is guessing
// which of their guesses landed.
func (s *Server) requireClient(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		token := auth.BearerToken(r.Header.Get(authHeader))
		if token == "" {
			unauthorised(ctx, w, "Send your token as: Authorization: Bearer <token>.")
			return
		}
		if !auth.LooksLikeToken(token) {
			s.logger.WarnContext(ctx, "rejected a malformed token")
			unauthorised(ctx, w, "That token is not valid.")
			return
		}

		client, err := s.tasks.ClientByTokenHash(ctx, auth.HashToken(token))
		switch {
		case errors.Is(err, task.ErrNotFound), errors.Is(err, task.ErrRevoked):
			// Logged apart so a revocation is diagnosable, answered alike so
			// it is not discoverable.
			s.logger.WarnContext(ctx, "rejected a token", slog.Any("reason", err))
			unauthorised(ctx, w, "That token is not valid.")
			return
		case err != nil:
			s.fail(ctx, w, "authenticating client", err)
			return
		}

		ctx = context.WithValue(ctx, clientKey{}, client)
		ctx = logging.WithAttrs(ctx, slog.String("client_id", client.ID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// unauthorised : Refuses a request, naming the scheme a client should use.
func unauthorised(ctx context.Context, w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="friday"`)
	writeError(ctx, w, http.StatusUnauthorized, message)
}

// requireRegistrationSecret : Guards registration.
//
// Registration that anyone may perform is no authentication at all, since a
// stranger would simply issue themselves a token. The secret is held in
// configuration and typed into a device once.
func (s *Server) requireRegistrationSecret(r *http.Request) error {
	if s.registrationSecret == "" {
		return errRegistrationDisabled
	}
	if !auth.SecretMatches(s.registrationSecret, auth.BearerToken(r.Header.Get(authHeader))) {
		return errRegistrationRefused
	}
	return nil
}

// Reasons registration may be refused.
var (
	errRegistrationDisabled = errors.New("registration is not enabled")
	errRegistrationRefused  = errors.New("registration secret does not match")
)

// callingClient : Returns the client the request belongs to. It is only valid
// inside a handler behind requireClient.
func callingClient(ctx context.Context) *task.Client {
	client, _ := ctx.Value(clientKey{}).(*task.Client)
	return client
}

// handleRegisterClient : Registers a client and gives it a first conversation
// to talk in.
//
// The first conversation is created here so a new client can ask something
// immediately. Creating one explicitly is for wanting a second.
func (s *Server) handleRegisterClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch err := s.requireRegistrationSecret(r); {
	case errors.Is(err, errRegistrationDisabled):
		s.logger.WarnContext(ctx, "registration attempted while disabled")
		writeError(ctx, w, http.StatusServiceUnavailable,
			"Registration is not enabled. Set [auth] registration_secret to allow it.")
		return
	case errors.Is(err, errRegistrationRefused):
		s.logger.WarnContext(ctx, "registration refused: wrong secret")
		unauthorised(ctx, w, "That registration secret is not valid.")
		return
	}

	var req registerClientRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(ctx, w, http.StatusBadRequest, err.Error())
			return
		}
	}

	token, tokenHash, err := auth.NewToken()
	if err != nil {
		s.fail(ctx, w, "issuing token", err)
		return
	}

	client, err := task.NewClient(req.Name, tokenHash)
	if errors.Is(err, task.ErrClientNameTooLong) {
		writeError(ctx, w, http.StatusBadRequest, "That name is too long.")
		return
	}
	if err != nil {
		s.fail(ctx, w, "registering client", err)
		return
	}

	if err := s.tasks.CreateClient(ctx, client); err != nil {
		s.fail(ctx, w, "registering client", err)
		return
	}

	conversation := task.NewConversation(client.ID, "")
	if err := s.tasks.CreateConversation(ctx, conversation); err != nil {
		s.fail(ctx, w, "starting the first conversation", err)
		return
	}
	if err := s.tasks.SetActiveConversation(ctx, client.ID, conversation.ID); err != nil {
		s.fail(ctx, w, "activating the first conversation", err)
		return
	}
	client.ActiveConversationID = conversation.ID

	s.logger.InfoContext(ctx, "client registered", slog.String("client_id", client.ID))

	// The only moment the token exists outside the caller's hands. It is not
	// stored, so it cannot be shown again.
	writeJSON(ctx, w, http.StatusCreated, registeredClientView{
		clientView: viewOfClient(client),
		Token:      token,
	})
}

// handleGetClient : Returns the calling client, including which conversation
// is active.
func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(ctx, w, http.StatusOK, viewOfClient(callingClient(ctx)))
}

// handleRevokeClient : Stops a client authenticating.
//
// A client may revoke itself, which is what a device does when it is being
// handed on or wiped. Revoking another client is refused, so that a stolen
// token cannot be used to lock out the rest.
func (s *Server) handleRevokeClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller := callingClient(ctx)
	id := chi.URLParam(r, "id")

	if id != caller.ID {
		writeError(ctx, w, http.StatusForbidden, "A client may only revoke itself.")
		return
	}

	if err := s.tasks.RevokeClient(ctx, id); err != nil {
		if errors.Is(err, task.ErrNotFound) {
			writeError(ctx, w, http.StatusNotFound, "No such client.")
			return
		}
		s.fail(ctx, w, "revoking client", err)
		return
	}

	s.logger.InfoContext(ctx, "client revoked", slog.String("client_id", id))
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateConversation : Starts a new conversation for the calling client.
//
// It becomes the active one unless the caller asks otherwise, since starting a
// conversation almost always means wanting to talk in it.
func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	client := callingClient(ctx)

	var req createConversationRequest
	req.Activate = true
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(ctx, w, http.StatusBadRequest, err.Error())
			return
		}
	}

	conversation := task.NewConversation(client.ID, req.Title)
	if err := s.tasks.CreateConversation(ctx, conversation); err != nil {
		s.fail(ctx, w, "creating conversation", err)
		return
	}

	if req.Activate {
		if err := s.tasks.SetActiveConversation(ctx, client.ID, conversation.ID); err != nil {
			s.fail(ctx, w, "activating conversation", err)
			return
		}
	}

	s.logger.InfoContext(ctx, "conversation created",
		slog.String("conversation_id", conversation.ID),
		slog.Bool("active", req.Activate))
	writeJSON(ctx, w, http.StatusCreated, viewOfConversation(*conversation, req.Activate))
}

// handleActivateConversation : Switches which conversation a prompt lands in.
func (s *Server) handleActivateConversation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	client := callingClient(ctx)
	id := chi.URLParam(r, "id")

	if !task.ValidConversationID(id) {
		writeError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}

	err := s.tasks.SetActiveConversation(ctx, client.ID, id)
	switch {
	case errors.Is(err, task.ErrNotFound), errors.Is(err, task.ErrNotOwned):
		// Answered alike: telling one client that another's conversation
		// exists reveals more than it should.
		writeError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	case err != nil:
		s.fail(ctx, w, "activating conversation", err)
		return
	}

	conversation, err := s.tasks.GetConversation(ctx, id)
	if err != nil {
		s.fail(ctx, w, "reading conversation", err)
		return
	}

	s.logger.InfoContext(ctx, "active conversation switched", slog.String("conversation_id", id))
	writeJSON(ctx, w, http.StatusOK, viewOfConversation(*conversation, true))
}
