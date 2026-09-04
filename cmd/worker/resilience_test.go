//go:build e2e

// Phase 6 Step 5 validation: drive the worker's process() against the live Kafka
// stack with a scrape that always fails and check the definition of done — the
// per-airline circuit breaker opens after the failure threshold, blocks jobs
// during its cooldown, lets exactly one half-open probe through afterwards, and
// permanently failed jobs land on the real scrape.jobs.dlq topic with their
// failure context.
//
// Same prereqs as e2e_test.go: the docker/ compose stack up and cmd/worker NOT
// running (this test joins the real consumer group).
//
//	go test -tags e2e -run TestResilience ./cmd/worker
package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"cloudmilesscouter/internal/breaker"
	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/queue"
	"cloudmilesscouter/internal/scraper"
	"cloudmilesscouter/internal/scraper/airlines"
	"cloudmilesscouter/internal/storage"
)

const resilAirline = "resil"

func TestResilienceBreakerAndDLQ(t *testing.T) {
	cfg := config.Load()
	cfg.MaxAttempts = 1                     // first failure is terminal — straight to the DLQ, no backoff sleep
	cfg.RetryBackoffBase = time.Millisecond // (unused at MaxAttempts=1, kept sane anyway)

	connectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := storage.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		t.Skipf("mongo not reachable at %s: %v", cfg.MongoURI, err)
	}
	t.Cleanup(func() { client.Disconnect(context.Background()) })

	// An always-failing scraper standing in for an airline whose site is down.
	// The call counter is the test's own "scrape attempts" signal — it only ticks
	// when the breaker actually lets a job reach the scrape.
	var scrapeCalls atomic.Int64
	airlines.Scrapers[resilAirline] = func(config.Config, scraper.SearchParams) ([]byte, error) {
		scrapeCalls.Add(1)
		return nil, errors.New("forced failure: airline site blocked")
	}
	t.Cleanup(func() { delete(airlines.Scrapers, resilAirline) })

	consumer := queue.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroupID)
	t.Cleanup(func() { consumer.Close() })
	if n := drainTopic(t, consumer); n > 0 {
		t.Logf("drained %d pre-existing message(s) from %s", n, queue.Topic)
	}

	producer := queue.NewProducer(cfg.KafkaBrokers)
	t.Cleanup(func() { producer.Close() })

	brokers := strings.Split(cfg.KafkaBrokers, ",")
	dlqBefore := readResilDLQ(t, brokers) // the DLQ topic keeps messages across runs

	brk := breaker.New(3, 2*time.Second)

	runJob := func() {
		job := queue.ScrapeJob{Airline: resilAirline, Origin: "RSL", Destination: "DLQ", Date: "2099-01-02"}
		if err := producer.Enqueue(context.Background(), job); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		fetchCtx, fc := context.WithTimeout(context.Background(), 15*time.Second)
		defer fc()
		msg, err := consumer.Fetch(fetchCtx)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if err := consumer.Commit(fetchCtx, msg); err != nil {
			t.Fatalf("commit: %v", err)
		}
		process(fetchCtx, cfg, client, producer, brk, 0, msg)
	}

	// --- below threshold: the breaker stays closed ---
	runJob()
	runJob()
	if got := brk.State(resilAirline); got != breaker.Closed {
		t.Fatalf("after 2 failures state = %v, want closed", got)
	}

	// --- the threshold-th consecutive failure opens it ---
	runJob()
	if got := brk.State(resilAirline); got != breaker.Open {
		t.Fatalf("after 3 failures state = %v, want open", got)
	}

	// --- an open breaker drops a job without attempting a scrape ---
	before := scrapeCalls.Load()
	runJob()
	if got := scrapeCalls.Load() - before; got != 0 {
		t.Fatalf("open breaker still ran %d scrape attempt(s), want 0", got)
	}

	// --- after the cooldown, exactly one job is let through as a half-open probe ---
	time.Sleep(2100 * time.Millisecond)
	before = scrapeCalls.Load()
	runJob() // the probe also fails, so the breaker re-opens
	if got := scrapeCalls.Load() - before; got != 1 {
		t.Fatalf("half-open probe ran %d scrape attempt(s), want 1", got)
	}
	if got := brk.State(resilAirline); got != breaker.Open {
		t.Fatalf("after a failed probe state = %v, want open", got)
	}

	// --- every job that attempted a scrape was dead-lettered (#1, #2, #3, probe) ---
	dlqAfter := readResilDLQ(t, brokers)
	if got := len(dlqAfter) - len(dlqBefore); got != 4 {
		t.Fatalf("new DLQ messages for %q = %d, want 4", resilAirline, got)
	}
	last := dlqAfter[len(dlqAfter)-1]
	if last.Job.Airline != resilAirline || last.Reason != "blocked" || last.Attempts != 1 {
		t.Fatalf("DLQ job = %+v, want airline=%s reason=blocked attempts=1", last, resilAirline)
	}
	if last.Error == "" || last.FailedAt.IsZero() {
		t.Fatalf("DLQ job missing error/failed_at: %+v", last)
	}
}

// readResilDLQ reads scrape.jobs.dlq from the beginning and returns the
// DeadLetterJobs for resilAirline. It uses a partition reader (no consumer group)
// so it never commits and can be called repeatedly for a before/after delta.
func readResilDLQ(t *testing.T, brokers []string) []queue.DeadLetterJob {
	t.Helper()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   brokers,
		Topic:     queue.DLQTopic,
		Partition: 0,
	})
	defer r.Close()
	if err := r.SetOffset(kafka.FirstOffset); err != nil {
		t.Fatalf("dlq set offset: %v", err)
	}

	var out []queue.DeadLetterJob
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		msg, err := r.ReadMessage(ctx)
		cancel()
		if err != nil {
			return out // deadline exceeded: nothing left
		}
		var dl queue.DeadLetterJob
		if err := json.Unmarshal(msg.Value, &dl); err != nil {
			t.Fatalf("dlq unmarshal: %v", err)
		}
		if dl.Job.Airline == resilAirline {
			out = append(out, dl)
		}
	}
}
