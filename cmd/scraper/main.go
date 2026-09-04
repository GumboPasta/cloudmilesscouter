package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/logging"
	"cloudmilesscouter/internal/mailotp"
	"cloudmilesscouter/internal/scraper"
	"cloudmilesscouter/internal/scraper/airlines"
	"cloudmilesscouter/internal/storage"
)

func main() {
	cfg := config.Load()
	logging.Setup("scraper", cfg.LogLevel, cfg.LogFormat)

	if len(os.Args) > 1 && os.Args[1] == "bootstrap" {
		if err := airlines.Bootstrap(cfg.UnitedProfileDir); err != nil {
			slog.Error("bootstrap failed", "err", err)
			os.Exit(1)
		}
		return
	}

	// Same one-time device-trust setup as "bootstrap", but reads United's OTP
	// email itself via Gmail IMAP instead of waiting for a human to type it —
	// needs UNITED_USERNAME/UNITED_PASSWORD and GMAIL_ADDRESS/GMAIL_APP_PASSWORD.
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-auto" {
		gmail := mailotp.Config{Address: cfg.GmailAddress, AppPassword: cfg.GmailAppPassword}
		if err := airlines.AutoBootstrap(cfg.UnitedProfileDir, cfg.UnitedUsername, cfg.UnitedPassword, gmail); err != nil {
			slog.Error("bootstrap-auto failed", "err", err)
			os.Exit(1)
		}
		return
	}

	// Graceful shutdown: SIGINT/SIGTERM cancels connections and the post-scrape
	// store. A scrape already running in the browser cannot be interrupted (the
	// scraper functions take no context) — the signal takes effect at the next
	// checkpoint.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(rootCtx, 5*time.Second)
	defer cancel()

	client, err := storage.Connect(ctx, cfg.MongoURI)
	if err != nil {
		slog.Error("mongo connection failed", "err", err)
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())

	slog.Info("mongo reachable", "uri", cfg.MongoURI)

	pg, err := storage.ConnectPostgres(ctx, cfg.PostgresURI)
	if err != nil {
		slog.Error("postgres connection failed", "err", err)
		os.Exit(1)
	}
	defer pg.Close()

	slog.Info("postgres reachable", "uri", cfg.PostgresURI)

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

	if rootCtx.Err() != nil {
		slog.Warn("interrupted after scrape, not storing", "airline", *airline)
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
	// context.Background() with a 10s bound, not rootCtx: don't let a Ctrl-C
	// land after a 30-45s scrape and throw the result away, but don't hang on a
	// wedged Mongo either.
	storeCtx, storeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer storeCancel()
	if err := storage.StoreRawScrape(storeCtx, client, doc); err != nil {
		slog.Error("store failed", "err", err)
		os.Exit(1)
	}

	if hasResults, err := airlines.HasResultsFor(*airline, body); err != nil {
		slog.Warn("could not determine result count", "err", err)
	} else if !hasResults {
		slog.Warn("no flights found", "airline", doc.Airline, "origin", doc.Origin, "destination", doc.Destination)
	}

	slog.Info("scrape stored", "airline", doc.Airline, "origin", doc.Origin, "destination", doc.Destination, "bytes", len(body))
}
