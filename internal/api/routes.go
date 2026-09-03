package api

import (
	"log/slog"
	"net/http"

	"cloudmilesscouter/internal/storage"
)

// handleRoutes serves GET /api/routes: a JSON array of routes that have award
// data, most-populated first, for the frontend to offer as search shortcuts.
func (s *server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := storage.ListRoutes(r.Context(), s.db)
	if err != nil {
		slog.Error("list routes failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list routes")
		return
	}
	writeJSON(w, http.StatusOK, routes)
}
