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
