package api

import (
	"log/slog"
	"net/http"

	"cloudmilesscouter/internal/storage"
)

// handleAirlines serves GET /api/airlines: a JSON array of the airlines present
// in the awards data, ordered by name.
func (s *server) handleAirlines(w http.ResponseWriter, r *http.Request) {
	airlines, err := storage.ListAirlines(r.Context(), s.db)
	if err != nil {
		slog.Error("list airlines failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list airlines")
		return
	}
	writeJSON(w, http.StatusOK, airlines)
}
