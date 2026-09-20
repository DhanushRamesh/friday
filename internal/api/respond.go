package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/DhanushRamesh/friday/internal/logging"
)

// writeJSON : Writes body as a JSON response with the given status code.
func writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be reported.
		logging.FromContext(ctx).ErrorContext(ctx, "failed to write response body",
			slog.Any("error", err))
	}
}

// writeError : Writes a failing response carrying a message for whoever reads
// it. The message may be spoken aloud, so it is written in plain language and
// never carries internal detail.
func writeError(ctx context.Context, w http.ResponseWriter, status int, message string) {
	writeJSON(ctx, w, status, errorResponse{Error: message})
}

// fail : Records an internal failure and answers 500 without revealing it.
func (s *Server) fail(ctx context.Context, w http.ResponseWriter, doing string, err error) {
	s.logger.ErrorContext(ctx, doing+" failed", slog.Any("error", err))
	writeError(ctx, w, http.StatusInternalServerError, "Something went wrong on my end.")
}
