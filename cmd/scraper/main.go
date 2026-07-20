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

	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	origin := fs.String("origin", "", `United display-string origin, e.g. "DALLAS, TX, US (ALL AIRPORTS)"`)
	destination := fs.String("destination", "", "United display-string destination")
	date := fs.String("date", "", "departure date, YYYY-MM-DD")
	fs.Parse(os.Args[1:])

	if *origin == "" || *destination == "" || *date == "" {
		fmt.Println(`usage: scraper -origin "..." -destination "..." -date YYYY-MM-DD`)
		os.Exit(1)
	}

	departDate, err := time.Parse("2006-01-02", *date)
	if err != nil {
		slog.Error("invalid date", "err", err)
		os.Exit(1)
	}

	body, err := airlines.Scrape(cfg.UnitedProfileDir, cfg.Headless, scraper.SearchParams{
		Origin:      *origin,
		Destination: *destination,
		Date:        departDate,
	}, cfg.UnitedPassword)
	if err != nil {
		slog.Error("scrape failed", "err", err)
		os.Exit(1)
	}

	doc := storage.RawScrape{
		Airline:     "united",
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

	if hasResults, err := airlines.HasResults(body); err != nil {
		slog.Warn("could not determine result count", "err", err)
	} else if !hasResults {
		slog.Warn("no flights found", "airline", doc.Airline, "origin", doc.Origin, "destination", doc.Destination)
	}

	slog.Info("scrape stored", "airline", doc.Airline, "origin", doc.Origin, "destination", doc.Destination, "bytes", len(body))
}
