package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/queue"
)

// defaultAirlines is the set of airlines one search fans out to. A job is
// dispatched for each; the worker skips any without a scraper registered in
// internal/scraper/airlines.Scrapers. Override with -airlines a,b,c.
var defaultAirlines = []string{"united", "american", "delta", "alaska"}

func main() {
	cfg := config.Load()

	fs := flag.NewFlagSet("producer", flag.ExitOnError)
	origin := fs.String("origin", "", `origin IATA airport/metro code, e.g. "DFW"`)
	destination := fs.String("destination", "", `destination IATA airport/metro code, e.g. "JFK"`)
	date := fs.String("date", "", "departure date, YYYY-MM-DD")
	airlinesCSV := fs.String("airlines", "", "comma-separated airline IDs to dispatch (default: all)")
	fs.Parse(os.Args[1:])

	if *origin == "" || *destination == "" || *date == "" {
		fmt.Println(`usage: producer -origin DFW -destination JFK -date YYYY-MM-DD [-airlines a,b,c]`)
		os.Exit(1)
	}

	airlines := defaultAirlines
	if *airlinesCSV != "" {
		airlines = strings.Split(*airlinesCSV, ",")
	}

	if _, err := time.Parse("2006-01-02", *date); err != nil {
		slog.Error("invalid date", "err", err)
		os.Exit(1)
	}

	p := queue.NewProducer(cfg.KafkaBrokers)
	defer p.Close()

	failed := 0
	for _, airline := range airlines {
		job := queue.ScrapeJob{
			Airline:     airline,
			Origin:      *origin,
			Destination: *destination,
			Date:        *date,
		}
		if err := p.Enqueue(context.Background(), job); err != nil {
			slog.Error("dispatch failed", "airline", airline, "err", err)
			failed++
		}
	}

	if failed > 0 {
		slog.Error("some jobs failed to dispatch", "failed", failed, "total", len(airlines))
		os.Exit(1)
	}

	slog.Info("all jobs dispatched", "count", len(airlines), "origin", *origin, "destination", *destination, "date", *date)
}
