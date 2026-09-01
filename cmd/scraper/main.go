package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/scraper"
	"cloudmilesscouter/internal/scraper/airlines"
	"cloudmilesscouter/internal/storage"
)

func main() {
	cfg := config.Load()

	if len(os.Args) > 1 && os.Args[1] == "bootstrap" {
		if err := airlines.Bootstrap(cfg.UnitedProfileDir); err != nil {
			slog.Error("bootstrap failed", "err", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := storage.Connect(ctx, cfg.MongoURI)
	if err != nil {
		log.Fatalf("mongo connection failed: %v", err)
	}
	defer client.Disconnect(context.Background())

	log.Println("MongoDB is reachable at", cfg.MongoURI)

	pg, err := storage.ConnectPostgres(ctx, cfg.PostgresURI)
	if err != nil {
		log.Fatalf("postgres connection failed: %v", err)
	}
	defer pg.Close()

	log.Println("PostgreSQL is reachable at", cfg.PostgresURI)

	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	airline := fs.String("airline", "united", "airline ID to scrape (must be registered in airlines.Scrapers)")
	origin := fs.String("origin", "", `origin IATA airport/metro code, e.g. "DFW"`)
	destination := fs.String("destination", "", `destination IATA airport/metro code, e.g. "JFK"`)
	date := fs.String("date", "", "departure date, YYYY-MM-DD")
	fs.Parse(os.Args[1:])

	if *origin == "" || *destination == "" || *date == "" {
		fmt.Println(`usage: scraper [-airline united] -origin DFW -destination JFK -date YYYY-MM-DD`)
		os.Exit(1)
	}

	scrapeFn, ok := airlines.Scrapers[*airline]
	if !ok {
		slog.Error("no scraper registered for airline", "airline", *airline)
		os.Exit(1)
	}

	departDate, err := time.Parse("2006-01-02", *date)
	if err != nil {
		slog.Error("invalid date", "err", err)
		os.Exit(1)
	}

	body, err := scrapeFn(cfg, scraper.SearchParams{
		Origin:      *origin,
		Destination: *destination,
		Date:        departDate,
	})
	if err != nil {
		slog.Error("scrape failed", "err", err)
		os.Exit(1)
	}

	doc := storage.RawScrape{
		Airline:     *airline,
		Origin:      *origin,
		Destination: *destination,
		SearchDate:  departDate,
		ScrapedAt:   time.Now(),
		RawPayload:  string(body),
	}
	if err := storage.StoreRawScrape(context.Background(), client, doc); err != nil {
		slog.Error("store failed", "err", err)
		os.Exit(1)
	}

	if hasResults, err := hasResults(*airline, body); err != nil {
		slog.Warn("could not determine result count", "err", err)
	} else if !hasResults {
		slog.Warn("no flights found", "airline", doc.Airline, "origin", doc.Origin, "destination", doc.Destination)
	}

	slog.Info("scrape stored", "airline", doc.Airline, "origin", doc.Origin, "destination", doc.Destination, "bytes", len(body))
}

// hasResults dispatches to the airline-specific "did this scrape return any
// flights" check. Unknown airlines report true (nothing to warn about).
func hasResults(airline string, body []byte) (bool, error) {
	switch airline {
	case "united":
		return airlines.HasResults(body)
	case "american":
		return airlines.HasResultsAmerican(body)
	default:
		return true, nil
	}
}
