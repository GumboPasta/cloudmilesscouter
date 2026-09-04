package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/logging"
	"cloudmilesscouter/internal/queue"
)

// defaultAirlines is the set of airlines one search fans out to. A job is
// dispatched for each; the worker skips any without a scraper registered in
// internal/scraper/airlines.Scrapers. Override with -airlines a,b,c.
var defaultAirlines = []string{"united", "american", "delta", "alaska"}

func main() {
	cfg := config.Load()
	logging.Setup("producer", cfg.LogLevel, cfg.LogFormat)

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
		airlines = nil
		for _, a := range strings.Split(*airlinesCSV, ",") {
			if a = strings.TrimSpace(a); a != "" {
				airlines = append(airlines, a)
			}
		}
	}
	if len(airlines) == 0 {
		slog.Error("no airlines to dispatch", "airlines", *airlinesCSV)
		os.Exit(1)
	}

	if _, err := time.Parse("2006-01-02", *date); err != nil {
		slog.Error("invalid date", "err", err)
		os.Exit(1)
	}

	// Graceful shutdown: SIGINT/SIGTERM cancels the in-flight Enqueue and stops
	// the loop, so Ctrl-C between jobs leaves the writer to flush via defer
	// rather than tearing it down mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := queue.NewProducer(cfg.KafkaBrokers)
	defer p.Close()

	failed, dispatched := 0, 0
	for _, airline := range airlines {
		if ctx.Err() != nil {
			slog.Warn("interrupted, stopping dispatch", "dispatched", dispatched, "remaining", len(airlines)-dispatched-failed)
			break
		}
		job := queue.ScrapeJob{
			Airline:     airline,
			Origin:      *origin,
			Destination: *destination,
			Date:        *date,
		}
		if err := p.Enqueue(ctx, job); err != nil {
			slog.Error("dispatch failed", "airline", airline, "err", err)
			failed++
			continue
		}
		dispatched++
	}

	if failed > 0 || ctx.Err() != nil {
		slog.Error("not all jobs dispatched", "dispatched", dispatched, "failed", failed, "total", len(airlines))
		os.Exit(1)
	}

	slog.Info("all jobs dispatched", "count", len(airlines), "origin", *origin, "destination", *destination, "date", *date)
}
