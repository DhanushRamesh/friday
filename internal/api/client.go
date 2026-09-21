package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/friday/internal/logging"
	"github.com/DhanushRamesh/friday/internal/task"
)

// ClientHeader : The header naming which client is calling.
//
// It is identity, not proof: anyone who knows an identifier can use it. When
// authentication arrives it becomes a token a client must prove, and nothing
// built on top of this needs to change, because knowing who is asking and
// establishing that they are who they say are separate concerns.
const ClientHeader = "X-Friday-Client"

// clientKey : The context key under which the calling client is carried.
type clientKey struct{}

// requireClient : Resolves the calling client and refuses the request without
// one.
func (s *Server) requireClient(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id := r.Header.Get(ClientHeader)
		if id == "" {
			writeError(ctx, w, http.StatusUnauthorized,
				"Send your client identifier in the "+ClientHeader+" header. Register one at POST /v1/clients.")
			return
		}
		if !task.ValidClientID(id) {
			writeError(ctx, w, http.StatusUnauthorized, "That is not a valid client identifier.")
			return
		}

		client, err := s.tasks.GetClient(ctx, id)
		if errors.Is(err, task.ErrNotFound) {
			writeError(ctx, w, http.StatusUnauthorized, "No such client. Register one at POST /v1/clients.")
			return
		}
		if err != nil {
			s.fail(ctx, w, "reading client", err)
			return
		}

		ctx = context.WithValue(ctx, clientKey{}, client)
		ctx = logging.WithAttrs(ctx, slog.String("client_id", client.ID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

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

	var req registerClientRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(ctx, w, http.StatusBadRequest, err.Error())
			return
		}
	}

	client, err := task.NewClient(req.Name)
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
	writeJSON(ctx, w, http.StatusCreated, viewOfClient(client))
}

// handleGetClient : Returns the calling client, including which conversation
// is active.
func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(ctx, w, http.StatusOK, viewOfClient(callingClient(ctx)))
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
