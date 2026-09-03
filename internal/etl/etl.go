package etl

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"cloudmilesscouter/internal/etl/parsers"
	"cloudmilesscouter/internal/storage"
)

// Parser turns one raw scrape into normalized award rows. Adding an airline
// is just a new file in internal/etl/parsers plus an entry in
// parsersByAirline below.
type Parser interface {
	Parse(raw storage.RawScrape) ([]storage.NormalizedAward, error)
}

var parsersByAirline = map[string]Parser{
	"united":   parsers.United{},
	"american": parsers.American{},
	"delta":    parsers.Delta{},
	"alaska":   parsers.Alaska{},
}

// RegisterParser adds or overrides the parser for an airline. It exists for the
// integration test that pushes synthetic raw scrapes through Run; production
// code registers parsers in the map literal above.
func RegisterParser(airline string, p Parser) {
	parsersByAirline[airline] = p
}

// Run reads every raw scrape from MongoDB, keeps only the newest doc per
// searched route+date, normalizes each via the parser registered for its
// airline, and writes the results into Postgres.
//
// cache may be nil. When set, every route+date whose awards were rewritten this
// run has its cached /api/search results dropped so the API stops serving the
// pre-scrape prices.
func Run(ctx context.Context, client *mongo.Client, db *sql.DB, cache *storage.Cache) error {
	docs, err := storage.FindRawScrapes(ctx, client)
	if err != nil {
		return err
	}

	// Retries (and any re-scrape) leave more than one doc for the same searched
	// route+date. WriteAwards only clears each key once per batch, so parsing
	// every doc would stack both scrapes' rows in Postgres. Keep the newest.
	type rawKey struct {
		airline, origin, destination string
		searchDate                   time.Time
	}
	newest := make(map[rawKey]storage.RawScrape, len(docs))
	for _, doc := range docs {
		k := rawKey{doc.Airline, doc.Origin, doc.Destination, doc.SearchDate}
		if prev, ok := newest[k]; !ok || doc.ScrapedAt.After(prev.ScrapedAt) {
			newest[k] = doc
		}
	}

	var awards []storage.NormalizedAward
	// Keys we parsed this run, so WriteAwards clears each one's stale rows even
	// when the parse produced zero awards (empty extraction, still a success).
	// A doc we skip (no parser, or a parse error) is left untouched: we don't
	// know the airline's real state, and wiping on a transient parse error would
	// be worse than serving slightly stale rows.
	clearKeys := make([]storage.RawScrapeKey, 0, len(newest))
	skipped := 0
	for k, doc := range newest {
		parser, ok := parsersByAirline[doc.Airline]
		if !ok {
			slog.Warn("no parser registered for airline, skipping", "airline", doc.Airline)
			skipped++
			continue
		}

		parsed, err := parser.Parse(doc)
		if err != nil {
			slog.Warn("failed to parse raw scrape, skipping", "airline", doc.Airline, "err", err)
			skipped++
			continue
		}
		clearKeys = append(clearKeys, storage.RawScrapeKey{
			AirlineCode: k.airline,
			Origin:      k.origin,
			Destination: k.destination,
			SearchDate:  k.searchDate,
		})
		awards = append(awards, parsed...)
	}

	if err := storage.WriteAwards(ctx, db, awards, clearKeys); err != nil {
		return err
	}

	// Best-effort cache invalidation: the awards are already committed, so a
	// Redis failure here just means those keys serve stale until their TTL.
	for _, k := range clearKeys {
		if err := cache.InvalidateRoute(ctx, k); err != nil {
			slog.Warn("cache invalidation failed", "err", err,
				"airline", k.AirlineCode, "origin", k.Origin, "destination", k.Destination,
				"search_date", k.SearchDate.Format("2006-01-02"))
		}
	}

	slog.Info("etl run complete", "docs", len(docs), "docs_deduped", len(newest), "keys_parsed", len(clearKeys), "awards_written", len(awards), "docs_skipped", skipped)
	return nil
}
