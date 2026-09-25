// Package assist : Lets Home Assistant use the server as its conversation agent.
//
// Home Assistant reaches a language model through one of its integrations,
// and of those built into it only Ollama's asks for the address of the server
// to call. The server therefore answers in Ollama's shape — a model listing at
// /api/tags and a conversation at /api/chat — because that is the vocabulary
// Home Assistant already speaks. Nothing here runs a model.
//
//	satellite -> Home Assistant -> POST /api/chat   -> a the server chat
//	                            <- newline-delimited JSON, as the answer forms
//
// The answer is streamed for the same reason the server's own clients are given a
// stream: Home Assistant holds the connection open while a chat runs, so a
// chat that takes two minutes survives as long as it keeps saying something,
// and each thing it says can be spoken as it arrives rather than after.
package assist

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
)

const (
	// ModelName : The one model the server offers. Home Assistant asks for a
	// model by name and will not finish setting up an integration that
	// offers none, so the server presents itself as one.
	//
	// Not the assistant's name, which is configuration and changes. This is
	// the identifier Home Assistant stores against its conversation agent,
	// so changing it stops voice working until that agent is reconfigured.
	ModelName = "assistant"

	// roleUser : The author of the question in a Home Assistant request.
	roleUser = "user"
	// roleAssistant : The author of every chunk the server sends back.
	roleAssistant = "assistant"

	// keepAliveInterval : How often an empty chunk is sent while a chat is
	// thinking.
	//
	// The connection is held open for as long as the chat runs, and a chat
	// can think for minutes without producing a word. An empty chunk reads
	// as an empty delta at the far end and costs nothing, where silence
	// risks the connection being closed underneath the answer.
	keepAliveInterval = 15 * time.Second

	// maxRequestBody : The largest request accepted. Home Assistant sends
	// the conversation so far, which is larger than a bare prompt but is
	// still only text somebody spoke.
	maxRequestBody = 1 << 20
)

// Runner : The part of the chat runner that this module requires.
type Runner interface {
	// Submit : Starts running a stored chat in the background.
	Submit(t *chat.Chat) error
	// Cancel : Stops a queued or running chat, reporting whether one was
	// found.
	Cancel(id string) bool
}

// Subscriber : Somewhere to listen for a chat's messages as they happen.
type Subscriber interface {
	// Subscribe : Returns a channel of a chat's events and a function that
	// ends the subscription.
	Subscribe(chatID string) (<-chan events.Event, func())
}

// Message : One turn of a conversation, in the shape Home Assistant sends and
// expects back.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest : The body of a conversation request.
//
// Home Assistant sends more than this — the tools it can offer, a context
// size, how long to keep a model loaded — and all of it describes running a
// model locally, which is not what happens here. Unknown fields are therefore
// accepted and ignored rather than refused, so that a future version of Home
// Assistant sending one more of them does not stop it answering.
type ChatRequest struct {
	// Model : Which model to answer as. Only ModelName exists.
	Model string `json:"model"`
	// Messages : The conversation so far, oldest first.
	Messages []Message `json:"messages"`
	// ConversationID : Which conversation to talk in. Empty joins the one this client
	// is active in, which is what Home Assistant does since it knows
	// nothing about conversations.
	//
	// Not part of Ollama's shape. It is added rather than kept on a second
	// endpoint because a caller that does know which conversation it means
	// should not have to switch the client's active conversation first and race
	// anything else using the same token.
	ConversationID string `json:"conversation_id,omitempty"`
}

// ChatChunk : One piece of an answer.
//
// A chunk carrying text has Done false; the last chunk carries no text and
// has Done true. This is what tells the caller the answer is complete.
type ChatChunk struct {
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	Message   Message   `json:"message"`
	// ErrorCode, ErrorDetail : Set on the final chunk when the chat failed.
	//
	// Beyond Ollama's shape, and omitted when empty, so a client that does
	// not know about them sees exactly what it saw before. What is said
	// aloud stays in Message.Content; these are for a screen.
	ErrorCode   string `json:"error_code,omitempty"`
	ErrorDetail string `json:"error_detail,omitempty"`
	Done        bool   `json:"done"`
	DoneReason  string `json:"done_reason,omitempty"`
}

// ModelsResponse : The body of a model listing.
type ModelsResponse struct {
	Models []Model `json:"models"`
}

// Model : One model in a listing.
type Model struct {
	Name       string       `json:"name"`
	Model      string       `json:"model"`
	ModifiedAt time.Time    `json:"modified_at"`
	Size       int64        `json:"size"`
	Digest     string       `json:"digest"`
	Details    ModelDetails `json:"details"`
}

// ModelDetails : What a caller is told about a model's construction. The server
// has no weights to describe, and answers only so that a listing parses.
type ModelDetails struct {
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	Format            string   `json:"format"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// Handler : Serves the endpoints Home Assistant calls.
type Handler struct {
	httpx.Responder
	repo   chat.Repository
	runner Runner
	events Subscriber
}

// New : Builds the handler from the store, the runner that executes chats and
// the bus that carries what they say.
func New(logger *slog.Logger, repo chat.Repository, runner Runner, bus Subscriber) *Handler {
	return &Handler{
		Responder: httpx.Responder{Logger: logger},
		repo:      repo,
		runner:    runner,
		events:    bus,
	}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
//
// The two paths are fixed by the client calling them: it appends them to the
// address it was configured with, so they cannot be moved under /v1 with the
// rest of the API.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/tags", h.Models)
	r.Post("/api/chat", h.Chat)
}

// Models : Lists the models available, of which there is one.
//
// Home Assistant calls this to check its configuration, and refuses to finish
// setting up if it does not answer quickly, so nothing slow belongs here.
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, http.StatusOK, ModelsResponse{
		Models: []Model{{
			Name:       ModelName,
			Model:      ModelName,
			ModifiedAt: time.Now().UTC(),
			Details: ModelDetails{
				Family:            ModelName,
				Families:          []string{ModelName},
				Format:            "api",
				ParameterSize:     "n/a",
				QuantizationLevel: "n/a",
			},
		}},
	})
}

// Chat : Answers a question from Home Assistant, streaming the answer as it
// forms.
//
// The conversation Home Assistant sends is not stored. The server keeps its own
// log of a conversation and builds a model's history from that, so only the
// question is taken from the request; taking the rest would give the chat two
// disagreeing accounts of what was said.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Logger.ErrorContext(ctx, "response writer cannot flush; streaming is impossible")
		httpx.WriteError(ctx, w, http.StatusInternalServerError, "Streaming is not available.")
		return
	}

	var req ChatRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That request could not be read.")
		return
	}

	prompt := lastQuestion(req.Messages)
	if prompt == "" {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "A question is required.")
		return
	}

	caller := authn.Of(ctx)
	// A named conversation is used when the caller knows one and it is theirs;
	// anything else falls back to whichever this client is active in.
	asked := req.ConversationID
	if asked != "" && !h.ownedByCaller(ctx, caller, asked) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	}
	if asked == "" {
		asked = caller.Client.ActiveConversationID
	}
	conversationID, err := chat.ActiveConversation(ctx, h.repo, caller.User.ID, caller.Client.ID, asked)
	if err != nil {
		h.Fail(ctx, w, "resolving conversation", err)
		return
	}

	// From the caller, not from this endpoint. This one speaks Ollama's wire
	// format, and a format says nothing about how the words were produced:
	// anything able to speak it can call it, and one day something typed
	// will.
	// Speaking again means the previous answer is no longer wanted, and two
	// cannot be listened to at once.
	h.supersede(ctx, conversationID)

	t, err := chat.New(conversationID, caller.Client.Channel, prompt)
	switch {
	case errors.Is(err, chat.ErrEmptyPrompt):
		httpx.WriteError(ctx, w, http.StatusBadRequest, "A question is required.")
		return
	case errors.Is(err, chat.ErrPromptTooLong):
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That question is too long.")
		return
	case err != nil:
		h.Fail(ctx, w, "creating chat", err)
		return
	}

	// Copied from the client rather than read back when the chat runs, so a
	// chat recovered after a restart goes to the model it was accepted for.
	t.Model = caller.Client.Model

	// Which client asked. A tool that acts on the client itself, such as
	// switching which conversation it talks in, has no other way to know
	// which one to act on.
	t.ClientID = caller.Client.ID

	if err := h.repo.Create(ctx, t); err != nil {
		h.Fail(ctx, w, "storing chat", err)
		return
	}

	// Subscribed before the chat is submitted, so that an answer arriving
	// immediately is heard rather than falling into the gap between the two.
	live, unsubscribe := h.events.Subscribe(t.ID)
	defer unsubscribe()

	if err := h.runner.Submit(t); err != nil {
		h.Logger.ErrorContext(ctx, "cannot submit chat", slog.Any("error", err))
		if failErr := t.Fail("That could not be started."); failErr == nil {
			_ = h.repo.Update(ctx, t)
		}
		httpx.WriteError(ctx, w, http.StatusServiceUnavailable, "Not accepting work at the moment.")
		return
	}

	h.Logger.InfoContext(ctx, "assist chat accepted", slog.String("chat_id", t.ID))

	// Every failure after this point is reported inside the stream, because
	// the status line has already been sent and cannot be taken back.
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	// Tell nginx and similar not to buffer, which would hold the whole answer
	// back until the chat ended and defeat the point of streaming it.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	h.follow(ctx, w, flusher, live, t.ID)
}

// follow : Writes the chat's messages as chunks until it ends or the caller
// leaves.
func (h *Handler) follow(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	live <-chan events.Event,
	chatID string,
) {
	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()

	for {
		select {
		case <-ctx.Done():
			// Home Assistant hung up, which here means the turn is over: it
			// will not reconnect to collect what it missed, the way a phone
			// resuming a stream does. Letting the chat run on would spend a
			// provider call on an answer nobody can hear, so it is stopped.
			//
			// This is deliberately unlike the SSE stream, where a dropped
			// connection is a phone on bad mobile data and the answer must
			// still be there when it comes back.
			if h.runner.Cancel(chatID) {
				h.Logger.InfoContext(ctx, "assist caller left, chat cancelled",
					slog.String("chat_id", chatID))
			}
			return

		case <-keepAlive.C:
			writeChunk(w, flusher, chunk("", false, ""))

		case ev, ok := <-live:
			if !ok {
				// The bus closed without a terminal event, which leaves the
				// caller waiting on a stream that will never end unless it is
				// closed properly here.
				writeChunk(w, flusher, chunk("", true, "stop"))
				return
			}

			if ev.Kind.Terminal() {
				if text := settled(strings.TrimSpace(ev.Text)); text != "" {
					writeChunk(w, flusher, chunk(text, false, ""))
				}
				last := chunk("", true, reasonFor(ev.Kind))
				// Only a screen can use these, and only a failure has them.
				// Home Assistant ignores fields it does not know, so the
				// spoken answer is unchanged by their being here.
				if ev.Kind == events.KindError {
					if failed, err := h.repo.Get(ctx, chatID); err == nil {
						last.ErrorCode = failed.ErrorCode
						last.ErrorDetail = failed.ErrorDetail
					}
				}
				writeChunk(w, flusher, last)
				return
			}

			// Progress, spoken as it happens. A chat reporting where it has
			// got to is why the connection is held open at all.
			if text := strings.TrimSpace(ev.Text); text != "" {
				writeChunk(w, flusher, chunk(text+"\n", false, ""))
			}
		}
	}
}

// chunk : Builds one piece of an answer.
func chunk(text string, done bool, reason string) ChatChunk {
	return ChatChunk{
		Model:      ModelName,
		CreatedAt:  time.Now().UTC(),
		Message:    Message{Role: roleAssistant, Content: text},
		Done:       done,
		DoneReason: reason,
	}
}

// writeChunk : Writes one chunk as a line of JSON and sends it immediately.
func writeChunk(w http.ResponseWriter, flusher http.Flusher, c ChatChunk) {
	if err := json.NewEncoder(w).Encode(c); err != nil {
		return
	}
	flusher.Flush()
}

// settled : Returns text with a trailing question mark turned into a full
// stop.
//
// Home Assistant reads the final character of an answer as a control signal:
// a question mark means "keep the microphone open for a reply", and there is
// no setting to turn that off. So an answer that happens to end in a question
// leaves the satellite listening, and the wake word stops being needed —
// which is the opposite of how the server is meant to be spoken to. Spoken aloud
// the substitution changes only the intonation of the last few words.
func settled(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	switch runes[len(runes)-1] {
	// The ordinary question mark, its fullwidth form, and the Greek question
	// mark, which is the set Home Assistant looks for.
	case '?', '？', '\u037e':
		runes[len(runes)-1] = '.'
		return string(runes)
	}
	return text
}

// reasonFor : Names why a chat stopped, in the vocabulary the caller expects.
func reasonFor(kind events.Kind) string {
	if kind == events.KindFinal {
		return "stop"
	}
	return string(kind)
}

// lastQuestion : Returns the most recent thing the user said.
//
// Home Assistant sends the whole conversation, its own system prompt included,
// and the question is the last user turn in it.
func lastQuestion(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != roleUser {
			continue
		}
		if text := strings.TrimSpace(messages[i].Content); text != "" {
			return text
		}
	}
	return ""
}

// supersede : Stops whatever is still running in a conversation.
//
// Speaking again means the previous answer is no longer wanted, and two
// cannot be listened to at once. A failure here is logged rather than
// refused: the new prompt matters more than tidying the old one, and a chat
// left running still reaches a terminal status on its own.
func (h *Handler) supersede(ctx context.Context, conversationID string) {
	unfinished, err := h.repo.Unfinished(ctx, conversationID)
	if err != nil {
		h.Logger.ErrorContext(ctx, "cannot find chats to supersede", slog.Any("error", err))
		return
	}

	for _, id := range unfinished {
		if h.runner.Cancel(id) {
			h.Logger.InfoContext(ctx, "superseded by a new prompt", slog.String("chat_id", id))
			continue
		}
		// Nothing is working on it, which happens to a chat left behind by a
		// process that stopped. Stop it here instead.
		t, err := h.repo.Get(ctx, id)
		if err != nil || t.Status.IsTerminal() {
			continue
		}
		if err := t.Cancel(); err == nil {
			_ = h.repo.Update(ctx, t)
		}
	}
}

// ownedByCaller : Whether a conversation exists and belongs to the caller.
//
// A conversation that is somebody else's is answered as missing, as everywhere
// else: saying it exists tells one user about another's.
func (h *Handler) ownedByCaller(ctx context.Context, c *authn.Caller, conversationID string) bool {
	if !chat.ValidConversationID(conversationID) {
		return false
	}
	conversation, err := h.repo.GetConversation(ctx, conversationID)
	return err == nil && conversation.UserID == c.User.ID
}
