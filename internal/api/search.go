package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloudmilesscouter/internal/storage"
)

// dateLayout is the only date format /api/search accepts, matching the
// awards.search_date column and the DoD example (date=2025-06-01).
const dateLayout = "2006-01-02"

// validCabins are the cabin names seeded in the cabins table. An empty cabin
// param is allowed and means "any cabin".
var validCabins = map[string]bool{
	"economy":         true,
	"premium_economy": true,
	"business":        true,
	"first":           true,
}

// handleSearch serves GET /api/search?origin=&destination=&date=&cabin=. It
// returns a JSON array of award options for the route and date, cheapest first.
// origin, destination and date are required; cabin is optional.
func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	origin, err := airportCode(q.Get("origin"), "origin")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	destination, err := airportCode(q.Get("destination"), "destination")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawDate := strings.TrimSpace(q.Get("date"))
	if rawDate == "" {
		writeError(w, http.StatusBadRequest, "date is required")
		return
	}
	date, err := time.Parse(dateLayout, rawDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "date must be in YYYY-MM-DD format")
		return
	}

	cabin := strings.ToLower(strings.TrimSpace(q.Get("cabin")))
	if cabin != "" && !validCabins[cabin] {
		writeError(w, http.StatusBadRequest, "cabin must be one of economy, premium_economy, business, first")
		return
	}

	search := storage.AwardSearch{
		Origin:      origin,
		Destination: destination,
		SearchDate:  date,
		Cabin:       cabin,
	}

	// Cache first. A Redis error is not fatal — log it and fall through to
	// Postgres so a down cache degrades to slower, not broken.
	if results, hit, err := s.cache.GetSearch(r.Context(), search); err != nil {
		slog.Warn("search cache read failed", "err", err,
			"origin", origin, "destination", destination, "date", rawDate, "cabin", cabin)
	} else if hit {
		writeJSON(w, http.StatusOK, results)
		return
	}

	results, err := storage.SearchAwards(r.Context(), s.db, search)
	if err != nil {
		slog.Error("search query failed", "err", err,
			"origin", origin, "destination", destination, "date", rawDate, "cabin", cabin)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	if err := s.cache.SetSearch(r.Context(), search, results); err != nil {
		slog.Warn("search cache write failed", "err", err,
			"origin", origin, "destination", destination, "date", rawDate, "cabin", cabin)
	}

	writeJSON(w, http.StatusOK, results)
}

// airportCode validates and normalizes an origin/destination param: 3 ASCII
// letters (IATA airport or metro code, e.g. JFK or NYC), upper-cased.
func airportCode(raw, field string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if len(code) != 3 || !isLetters(code) {
		return "", fmt.Errorf("%s must be a 3-letter airport code", field)
	}
	return code, nil
}

func isLetters(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// writeError sends a JSON error body: {"error": "..."}.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
