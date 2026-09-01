package etl

import (
	"context"
	"database/sql"
	"log/slog"

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
}

// Run reads every raw scrape from MongoDB, normalizes it via the parser
// registered for its airline, and writes the results into Postgres.
func Run(ctx context.Context, client *mongo.Client, db *sql.DB) error {
	docs, err := storage.FindRawScrapes(ctx, client)
	if err != nil {
		return err
	}

	var awards []storage.NormalizedAward
	skipped := 0
	for _, doc := range docs {
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
		awards = append(awards, parsed...)
	}

	if err := storage.WriteAwards(ctx, db, awards); err != nil {
		return err
	}

	slog.Info("etl run complete", "docs", len(docs), "awards_written", len(awards), "docs_skipped", skipped)
	return nil
}
