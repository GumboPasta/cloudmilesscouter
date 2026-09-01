//go:build e2e

// Package-level end-to-end smoke: producer -> Kafka -> worker (this package's
// process) -> MongoDB -> ETL -> PostgreSQL, using a stub scraper and stub parser
// so nothing touches a live airline site or a browser.
//
// Requires the docker/ compose stack up and cmd/worker NOT running (the test
// joins the real consumer group and would race a live worker for its message).
// The test drains any messages already sitting in scrape.jobs as a setup step —
// run it against a dev broker, not one with jobs you care about.
//
//	go test -tags e2e ./cmd/worker
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"cloudmilesscouter/internal/breaker"
	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/etl"
	"cloudmilesscouter/internal/queue"
	"cloudmilesscouter/internal/scraper"
	"cloudmilesscouter/internal/scraper/airlines"
	"cloudmilesscouter/internal/storage"
)

const (
	e2eAirline = "e2e"
	e2eCode    = "E2E"
	e2eOrigin  = "ZZ1"
	e2eDest    = "ZZ2"
	e2eDate    = "2099-01-02"
)

// e2eParser turns the stub scraper's constant blob into one award row.
type e2eParser struct{}

func (e2eParser) Parse(raw storage.RawScrape) ([]storage.NormalizedAward, error) {
	var body struct {
		Points int `json:"points"`
	}
	if err := json.Unmarshal([]byte(raw.RawPayload), &body); err != nil {
		return nil, err
	}
	if body.Points == 0 {
		return nil, errors.New("e2e stub: no points in payload")
	}
	return []storage.NormalizedAward{{
		AirlineCode:       e2eCode,
		AirlineName:       "E2E Test Airline",
		Origin:            raw.Origin,
		Destination:       raw.Destination,
		SearchDate:        raw.SearchDate,
		ScrapedAt:         raw.ScrapedAt,
		Cabin:             "economy",
		AwardType:         "saver",
		Currency:          "USD",
		FlightNumber:      "E2 100",
		FlightOrigin:      raw.Origin,
		FlightDestination: raw.Destination,
		DepartTime:        raw.SearchDate.Add(9 * time.Hour),
		ArriveTime:        raw.SearchDate.Add(12 * time.Hour),
		DurationMinutes:   180,
		Stops:             0,
		PointsCost:        body.Points,
		TaxesFees:         5.60,
	}}, nil
}

func TestPipelineEndToEnd(t *testing.T) {
	cfg := config.Load()

	connectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := storage.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		t.Skipf("mongo not reachable at %s: %v", cfg.MongoURI, err)
	}
	t.Cleanup(func() { client.Disconnect(context.Background()) })

	pg, err := storage.ConnectPostgres(connectCtx, cfg.PostgresURI)
	if err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}
	t.Cleanup(func() { pg.Close() })

	// Fake airline: "scrape" is a constant blob, parser is e2eParser. No browser.
	airlines.Scrapers[e2eAirline] = func(config.Config, scraper.SearchParams) ([]byte, error) {
		return []byte(`{"points":12345}`), nil
	}
	etl.RegisterParser(e2eAirline, e2eParser{})

	// Registered after the connection cleanups, so it runs before them (LIFO).
	cleanupE2E(t, client, pg) // clear leftovers from a prior failed run
	t.Cleanup(func() { cleanupE2E(t, client, pg) })

	consumer := queue.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroupID)
	t.Cleanup(func() { consumer.Close() })

	// Setup: drain anything already in the topic so our fetch below gets our job.
	drained := drainTopic(t, consumer)
	if drained > 0 {
		t.Logf("drained %d pre-existing message(s) from %s", drained, queue.Topic)
	}

	// --- produce one job ---
	producer := queue.NewProducer(cfg.KafkaBrokers)
	t.Cleanup(func() { producer.Close() })

	job := queue.ScrapeJob{Airline: e2eAirline, Origin: e2eOrigin, Destination: e2eDest, Date: e2eDate}
	if err := producer.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// --- consume + process exactly as cmd/worker's fetch loop does ---
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer fetchCancel()

	msg, err := consumer.Fetch(fetchCtx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := consumer.Commit(fetchCtx, msg); err != nil {
		t.Fatalf("commit: %v", err)
	}
	decoded, err := queue.Decode(msg)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Airline != e2eAirline {
		t.Fatalf("fetched an unexpected job for %q after draining", decoded.Airline)
	}
	process(fetchCtx, cfg, client, producer, breaker.New(), 0, msg)

	// --- assert raw doc landed in mongo ---
	coll := client.Database("data").Collection("flight_scrapes")
	n, err := coll.CountDocuments(context.Background(), bson.M{"airline": e2eAirline, "origin": e2eOrigin, "destination": e2eDest})
	if err != nil {
		t.Fatalf("count mongo docs: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 raw doc in mongo, got %d", n)
	}

	// --- run ETL over everything (also a regression check on the real docs) ---
	if err := etl.Run(context.Background(), client, pg); err != nil {
		t.Fatalf("etl.Run: %v", err)
	}

	// --- assert the normalized row is in postgres ---
	var awards int
	if err := pg.QueryRow(`
		SELECT count(*) FROM awards a
		JOIN airlines ai ON ai.id = a.airline_id
		JOIN routes r ON r.id = a.route_id
		WHERE ai.code = $1 AND r.origin = $2 AND r.destination = $3`,
		e2eCode, e2eOrigin, e2eDest).Scan(&awards); err != nil {
		t.Fatalf("query awards: %v", err)
	}
	if awards != 1 {
		t.Fatalf("want 1 award row in postgres, got %d", awards)
	}
}

// drainTopic fetches and commits every message currently available, returning
// the count. It stops once a fetch blocks for 3s with nothing new.
func drainTopic(t *testing.T, consumer *queue.Consumer) int {
	t.Helper()
	count := 0
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		msg, err := consumer.Fetch(ctx)
		cancel()
		if err != nil {
			return count // deadline exceeded: nothing left
		}
		if err := consumer.Commit(context.Background(), msg); err != nil {
			t.Fatalf("drain commit: %v", err)
		}
		count++
	}
}

func cleanupE2E(t *testing.T, client *mongo.Client, pg *sql.DB) {
	t.Helper()
	if _, err := client.Database("data").Collection("flight_scrapes").
		DeleteMany(context.Background(), bson.M{"airline": e2eAirline}); err != nil {
		t.Logf("cleanup mongo: %v", err)
	}
	if _, err := pg.Exec(`
		DELETE FROM awards
		WHERE airline_id IN (SELECT id FROM airlines WHERE code = $1)
		   OR route_id IN (SELECT id FROM routes WHERE origin = $2 AND destination = $3)`,
		e2eCode, e2eOrigin, e2eDest); err != nil {
		t.Logf("cleanup awards: %v", err)
	}
	if _, err := pg.Exec(`DELETE FROM routes WHERE origin = $1 AND destination = $2`, e2eOrigin, e2eDest); err != nil {
		t.Logf("cleanup routes: %v", err)
	}
	if _, err := pg.Exec(`DELETE FROM airlines WHERE code = $1`, e2eCode); err != nil {
		t.Logf("cleanup airlines: %v", err)
	}
}

// TestETLLatestDocWins pushes two raw docs for the same searched route+date with
// different scraped_at and different points, then asserts etl.Run writes exactly
// one award row and that it comes from the newer doc.
func TestETLLatestDocWins(t *testing.T) {
	cfg := config.Load()

	connectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := storage.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		t.Skipf("mongo not reachable at %s: %v", cfg.MongoURI, err)
	}
	t.Cleanup(func() { client.Disconnect(context.Background()) })

	pg, err := storage.ConnectPostgres(connectCtx, cfg.PostgresURI)
	if err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}
	t.Cleanup(func() { pg.Close() })

	etl.RegisterParser(e2eAirline, e2eParser{})

	cleanupE2E(t, client, pg)
	t.Cleanup(func() { cleanupE2E(t, client, pg) })

	searchDate, err := time.Parse("2006-01-02", e2eDate)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}

	base := storage.RawScrape{
		Airline:     e2eAirline,
		Origin:      e2eOrigin,
		Destination: e2eDest,
		SearchDate:  searchDate,
	}
	older := base
	older.ScrapedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older.RawPayload = `{"points":11111}`
	newer := base
	newer.ScrapedAt = older.ScrapedAt.Add(time.Hour)
	newer.RawPayload = `{"points":22222}`

	// Insert older last so ordering can't accidentally carry the test.
	for _, doc := range []storage.RawScrape{newer, older} {
		if err := storage.StoreRawScrape(context.Background(), client, doc); err != nil {
			t.Fatalf("store raw scrape: %v", err)
		}
	}

	if err := etl.Run(context.Background(), client, pg); err != nil {
		t.Fatalf("etl.Run: %v", err)
	}

	rows, err := pg.Query(`
		SELECT a.points_cost FROM awards a
		JOIN airlines ai ON ai.id = a.airline_id
		JOIN routes r ON r.id = a.route_id
		WHERE ai.code = $1 AND r.origin = $2 AND r.destination = $3`,
		e2eCode, e2eOrigin, e2eDest)
	if err != nil {
		t.Fatalf("query awards: %v", err)
	}
	defer rows.Close()

	var points []int
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(points) != 1 {
		t.Fatalf("want exactly 1 award row, got %d: %v", len(points), points)
	}
	if points[0] != 22222 {
		t.Fatalf("want award row from the newer doc (22222 points), got %d", points[0])
	}
}
