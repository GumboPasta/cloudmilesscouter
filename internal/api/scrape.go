package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloudmilesscouter/internal/queue"
)

// maxScrapeBody caps the request body for POST /api/scrape. The payload is a
// handful of short strings; anything larger is a client mistake or abuse.
const maxScrapeBody = 4 << 10

// defaultScrapeAirlines is the set a scrape request fans out to when the body
// omits "airlines". It matches cmd/producer's default; the worker skips any
// airline without a registered scraper.
var defaultScrapeAirlines = []string{"united", "american", "delta", "alaska"}

// scrapeDispatcher enqueues scrape jobs. *queue.Producer satisfies it; tests
// pass a fake so the handler can be exercised without Kafka.
type scrapeDispatcher interface {
	Enqueue(ctx context.Context, job queue.ScrapeJob) error
}

// scrapeRequest is the POST /api/scrape body. origin, destination and date are
// required; airlines is optional and defaults to defaultScrapeAirlines.
type scrapeRequest struct {
	Origin      string   `json:"origin"`
	Destination string   `json:"destination"`
	Date        string   `json:"date"`
	Airlines    []string `json:"airlines"`
}

// scrapeResponse reports which airline jobs were dispatched for the search.
type scrapeResponse struct {
	Dispatched  []string `json:"dispatched"`
	Origin      string   `json:"origin"`
	Destination string   `json:"destination"`
	Date        string   `json:"date"`
}

// handleScrape serves POST /api/scrape: it validates the search and dispatches
// one scrape job per airline onto the Kafka queue for the worker pool to pick
// up. It does not wait for the scrape to run — the response is 202 with the
// list of dispatched airlines.
func (s *server) handleScrape(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxScrapeBody))
	dec.DisallowUnknownFields()

	var req scrapeRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	origin, err := airportCode(req.Origin, "origin")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	destination, err := airportCode(req.Destination, "destination")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	date := strings.TrimSpace(req.Date)
	if date == "" {
		writeError(w, http.StatusBadRequest, "date is required")
		return
	}
	if _, err := time.Parse(dateLayout, date); err != nil {
		writeError(w, http.StatusBadRequest, "date must be in YYYY-MM-DD format")
		return
	}

	airlines, err := normalizeAirlines(req.Airlines)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dispatched := make([]string, 0, len(airlines))
	for _, airline := range airlines {
		job := queue.ScrapeJob{Airline: airline, Origin: origin, Destination: destination, Date: date}
		if err := s.dispatcher.Enqueue(r.Context(), job); err != nil {
			slog.Error("scrape dispatch failed", "err", err, "airline", airline,
				"origin", origin, "destination", destination, "date", date, "dispatched", dispatched)
			writeError(w, http.StatusBadGateway, "failed to dispatch scrape jobs")
			return
		}
		dispatched = append(dispatched, airline)
	}

	writeJSON(w, http.StatusAccepted, scrapeResponse{
		Dispatched:  dispatched,
		Origin:      origin,
		Destination: destination,
		Date:        date,
	})
}

// normalizeAirlines trims and lower-cases the requested airline IDs, dropping
// blanks and duplicates. An omitted/empty list yields defaultScrapeAirlines; a
// list that is all blanks is a client error.
func normalizeAirlines(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return defaultScrapeAirlines, nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, a := range raw {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, errors.New("airlines must not be empty")
	}
	return out, nil
}
