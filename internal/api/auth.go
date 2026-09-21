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

// authHeader : The header carrying a device's token.
const authHeader = "Authorization"

// callerKey : The context key under which the authenticated caller is
// carried.
type callerKey struct{}

// caller : Who is making a request: a user, and the device they are using.
//
// The user is the boundary — everything is owned by and visible to them. The
// device is only which credential was presented, and matters for where a
// prompt lands and which token to revoke.
type caller struct {
	user   *task.User
	device *task.Device
}

// handleLogin : Authenticates a user and registers the device they are
// logging in from, returning its token.
//
// Logging in and registering a device are one act: a token exists only for a
// device, and a device may be created only by someone who proved who they
// are. There is nothing to register separately.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := s.tasks.UserByUsername(ctx, req.Username)
	if errors.Is(err, task.ErrNotFound) {
		// Spend what a real check costs. Answering an unknown username
		// faster than a wrong password tells whoever is guessing which
		// usernames exist.
		auth.DummyPasswordCheck()
		s.logger.WarnContext(ctx, "login refused: no such user")
		unauthorised(ctx, w, "That username or password is not correct.")
		return
	}
	if err != nil {
		s.fail(ctx, w, "reading user", err)
		return
	}

	if !auth.PasswordMatches(user.PasswordHash, req.Password) {
		s.logger.WarnContext(ctx, "login refused: wrong password",
			slog.String("user_id", user.ID))
		unauthorised(ctx, w, "That username or password is not correct.")
		return
	}

	token, tokenHash, err := auth.NewToken()
	if err != nil {
		s.fail(ctx, w, "issuing token", err)
		return
	}

	device, err := task.NewDevice(user.ID, req.DeviceName, tokenHash)
	if errors.Is(err, task.ErrDeviceNameTooLong) {
		writeError(ctx, w, http.StatusBadRequest, "That device name is too long.")
		return
	}
	if err != nil {
		s.fail(ctx, w, "registering device", err)
		return
	}
	if err := s.tasks.CreateDevice(ctx, device); err != nil {
		s.fail(ctx, w, "registering device", err)
		return
	}

	// Somewhere to talk at once. A conversation belongs to the user, so a
	// second device joins whatever already exists rather than starting over.
	conversationID, err := s.startingConversation(ctx, user.ID)
	if err != nil {
		s.fail(ctx, w, "preparing a conversation", err)
		return
	}
	if err := s.tasks.SetActiveConversation(ctx, user.ID, device.ID, conversationID); err != nil {
		s.fail(ctx, w, "activating conversation", err)
		return
	}
	device.ActiveConversationID = conversationID

	s.logger.InfoContext(ctx, "device logged in",
		slog.String("user_id", user.ID),
		slog.String("device_id", device.ID))

	// The only moment the token exists outside the caller's hands.
	writeJSON(ctx, w, http.StatusCreated, loginResponse{
		Token:  token,
		User:   viewOfUser(user),
		Device: viewOfDevice(*device, true),
	})
}

// startingConversation : Returns a conversation for a newly logged-in device
// to begin in, reusing the user's most recent rather than adding another.
func (s *Server) startingConversation(ctx context.Context, userID string) (string, error) {
	existing, err := s.tasks.ListConversations(ctx, userID, 1)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return existing[0].ID, nil
	}

	conversation := task.NewConversation(userID, "")
	if err := s.tasks.CreateConversation(ctx, conversation); err != nil {
		return "", err
	}
	return conversation.ID, nil
}

// requireAuth : Authenticates the caller and refuses the request without a
// usable token.
//
// Every refusal reads the same and answers 401. Distinguishing a token never
// issued from one since revoked would tell whoever is guessing which of their
// guesses landed.
func (s *Server) requireAuth(next http.Handler) http.Handler {
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

		device, err := s.tasks.DeviceByTokenHash(ctx, auth.HashToken(token))
		switch {
		case errors.Is(err, task.ErrNotFound), errors.Is(err, task.ErrRevoked):
			// Logged apart so a revocation is diagnosable, answered alike so
			// it is not discoverable.
			s.logger.WarnContext(ctx, "rejected a token", slog.Any("reason", err))
			unauthorised(ctx, w, "That token is not valid.")
			return
		case err != nil:
			s.fail(ctx, w, "authenticating device", err)
			return
		}

		user, err := s.tasks.GetUser(ctx, device.UserID)
		if err != nil {
			// The device outlived its user, so the token is worthless.
			s.logger.WarnContext(ctx, "token belongs to a device with no user",
				slog.String("device_id", device.ID))
			unauthorised(ctx, w, "That token is not valid.")
			return
		}

		ctx = context.WithValue(ctx, callerKey{}, &caller{user: user, device: device})
		ctx = logging.WithAttrs(ctx,
			slog.String("user_id", user.ID),
			slog.String("device_id", device.ID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// callerOf : Returns who is making the request. Valid only inside a handler
// behind requireAuth.
func callerOf(ctx context.Context) *caller {
	c, _ := ctx.Value(callerKey{}).(*caller)
	return c
}

// unauthorised : Refuses a request, naming the scheme a caller should use.
func unauthorised(ctx context.Context, w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="friday"`)
	writeError(ctx, w, http.StatusUnauthorized, message)
}

// handleGetMe : Returns the user, the device in use, and where a prompt from
// it will land.
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := callerOf(ctx)
	writeJSON(ctx, w, http.StatusOK, meResponse{
		User:   viewOfUser(c.user),
		Device: viewOfDevice(*c.device, true),
	})
}

// handleListDevices : Returns the caller's devices, revoked ones included so
// that a revocation is visible rather than silently absent.
func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := callerOf(ctx)

	devices, err := s.tasks.ListDevices(ctx, c.user.ID)
	if err != nil {
		s.fail(ctx, w, "listing devices", err)
		return
	}

	views := make([]deviceView, len(devices))
	for i, d := range devices {
		views[i] = viewOfDevice(d, d.ID == c.device.ID)
	}
	writeJSON(ctx, w, http.StatusOK, listDevicesResponse{Devices: views})
}

// handleRevokeDevice : Stops one of the caller's devices authenticating.
//
// Any of a user's devices may revoke any other, which is the point: a phone
// left in a taxi is revoked from the laptop at home.
func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := callerOf(ctx)
	id := chi.URLParam(r, "id")

	if !task.ValidDeviceID(id) {
		writeError(ctx, w, http.StatusNotFound, "No such device.")
		return
	}

	err := s.tasks.RevokeDevice(ctx, c.user.ID, id)
	switch {
	case errors.Is(err, task.ErrNotFound), errors.Is(err, task.ErrNotOwned):
		writeError(ctx, w, http.StatusNotFound, "No such device.")
		return
	case err != nil:
		s.fail(ctx, w, "revoking device", err)
		return
	}

	s.logger.InfoContext(ctx, "device revoked", slog.String("revoked_device_id", id))
	w.WriteHeader(http.StatusNoContent)
}
