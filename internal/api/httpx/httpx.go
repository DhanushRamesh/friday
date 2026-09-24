// Package httpx : Writes and reads the HTTP bodies every module shares.
//
// It holds no state of FRIDAY's own and knows nothing about chats, sessions
// or users. Its job is only to put the right bytes on the wire and to refuse
// a request body that should not be read, so that each resource module is
// left with its own logic.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// MaxRequestBody : The largest request body accepted. A prompt is text typed
// or spoken by one person, so anything beyond this is a mistake or an attack.
const MaxRequestBody = 1 << 20

// ErrorResponse : The body returned with every failing status code.
type ErrorResponse struct {
	// Error : What went wrong, phrased for whoever reads it. Internal detail
	// stays in the log.
	Error string `json:"error"`
}

// WriteJSON : Writes body as a JSON response with the given status code.
func WriteJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be reported.
		logging.FromContext(ctx).ErrorContext(ctx, "failed to write response body",
			slog.Any("error", err))
	}
}

// WriteError : Writes a failing response carrying a message for whoever reads
// it. The message may be spoken aloud, so it is written in plain language and
// never carries internal detail.
func WriteError(ctx context.Context, w http.ResponseWriter, status int, message string) {
	WriteJSON(ctx, w, status, ErrorResponse{Error: message})
}

// Responder : The logging half of answering a request, embedded by every
// module's handler.
//
// It exists so that reporting an internal failure reads the same everywhere
// and cannot accidentally leak the cause to the caller.
type Responder struct {
	// Logger : Receives the causes that callers are not told. Required.
	Logger *slog.Logger
}

// Fail : Records an internal failure and answers 500 without revealing it.
func (rs Responder) Fail(ctx context.Context, w http.ResponseWriter, doing string, err error) {
	rs.Logger.ErrorContext(ctx, doing+" failed", slog.Any("error", err))
	WriteError(ctx, w, http.StatusInternalServerError, "Something went wrong on my end.")
}

// DecodeJSON : Reads a JSON body, rejecting one that is malformed, oversized
// or carries fields the request does not define.
func DecodeJSON(w http.ResponseWriter, r *http.Request, into any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBody)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(into); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return errors.New("A request body is required.")
		case errors.As(err, &maxErr):
			return errors.New("That request is too large.")
		default:
			return errors.New("The request body is not valid JSON.")
		}
	}
	// A second value means the body held more than one object.
	if dec.More() {
		return errors.New("The request body must hold a single object.")
	}
	return nil
}
