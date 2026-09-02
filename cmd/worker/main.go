// Command worker runs a pool of concurrent workers that pull ScrapeJobs off the
// scrape.jobs Kafka topic, scrape each airline, and store the raw result in
// MongoDB. Run it alongside the stack, then dispatch jobs with cmd/producer.
//
// Only airlines registered in internal/scraper/airlines.Scrapers are scraped;
// jobs for others are logged and skipped until their scraper lands.
//
// Delivery is at-most-once: the fetch loop commits each message before handing
// it to a worker, so a crash mid-scrape does not redeliver the job.
//
// Failure handling (Step 5): a failed scrape or store is re-queued as a fresh
// message with an incremented attempt count after an exponential backoff, up to
// cfg.MaxAttempts tries, then dropped with a structured "giving up" log (a
// dead-letter topic is Phase 6). A per-airline circuit breaker fails jobs fast
// for a cooldown once an airline site has failed repeatedly, so the pool stops
// launching browsers at a site that is down.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/segmentio/kafka-go"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"cloudmilesscouter/internal/breaker"
	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/queue"
	"cloudmilesscouter/internal/scraper"
	"cloudmilesscouter/internal/scraper/airlines"
	"cloudmilesscouter/internal/storage"
)

// maxBackoff caps the exponential retry backoff regardless of attempt count.
const maxBackoff = 30 * time.Second

// ioTimeout is a safety ceiling on the Mongo store and the retry re-enqueue so a
// wedged Mongo or Kafka cannot pin a worker goroutine forever. It is deliberately
// not a config field — a hang bound, not a tunable.
const ioTimeout = 10 * time.Second

func main() {
	cfg := config.Load()

	connectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := storage.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		log.Fatalf("mongo connection failed: %v", err)
	}
	defer client.Disconnect(context.Background())
	log.Println("MongoDB is reachable at", cfg.MongoURI)

	consumer := queue.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroupID)
	defer consumer.Close()

	// The pool re-queues failed jobs through its own producer.
	producer := queue.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()

	brk := breaker.New()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("worker pool started", "workers", cfg.WorkerCount, "brokers", cfg.KafkaBrokers,
		"group", cfg.KafkaGroupID, "max_attempts", cfg.MaxAttempts, "retry_backoff_base", cfg.RetryBackoffBase.String())

	// One fetch loop feeds a buffered channel that the workers drain. The buffer
	// lets the loop stay a step ahead without pulling more than the pool can hold.
	jobs := make(chan kafka.Message, cfg.WorkerCount)

	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for msg := range jobs {
				process(ctx, cfg, client, producer, brk, id, msg)
			}
		}(i)
	}

	for ctx.Err() == nil {
		msg, err := consumer.Fetch(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break // shutting down
			}
			slog.Error("fetch failed", "err", err)
			continue
		}
		// Commit before the scrape runs: at-most-once. A failed scrape is
		// recovered by the re-enqueue in retry, not by Kafka redelivery.
		if err := consumer.Commit(ctx, msg); err != nil {
			slog.Error("commit failed", "err", err, "partition", msg.Partition, "offset", msg.Offset)
		}
		select {
		case jobs <- msg:
		case <-ctx.Done():
		}
	}

	close(jobs)
	wg.Wait()
	slog.Info("worker pool stopped")
}

func process(ctx context.Context, cfg config.Config, client *mongo.Client, producer *queue.Producer, brk *breaker.Breaker, workerID int, msg kafka.Message) {
	job, err := queue.Decode(msg)
	if err != nil {
		slog.Error("undecodable job, skipping", "worker", workerID, "err", err, "raw", string(msg.Value))
		return
	}

	logJob := slog.With("worker", workerID, "airline", job.Airline, "origin", job.Origin,
		"destination", job.Destination, "date", job.Date, "cabin", job.Cabin, "attempt", job.Attempt)

	// Bad airline or date can never succeed on a retry — skip and move on.
	scrapeFn, ok := airlines.Scrapers[job.Airline]
	if !ok {
		logJob.Warn("no scraper registered for airline, skipping")
		return
	}
	date, err := time.Parse("2006-01-02", job.Date)
	if err != nil {
		logJob.Error("invalid job date, skipping", "err", err)
		return
	}

	// Circuit open: drop the job instead of retrying. Every retry inside the
	// 60s cooldown would be a no-op that still burns an attempt, so a job that
	// lands here gets dropped within ~6s having never been scraped. The producer
	// re-dispatches searches on its own cadence, so a dropped job comes back on
	// the next dispatch, after the cooldown has had a chance to pass.
	if !brk.Allow(job.Airline) {
		logJob.Warn("circuit open for airline, dropping job", "reason", "circuit_open")
		return
	}

	logJob.Info("job started")
	body, err := scrapeFn(cfg, scraper.SearchParams{
		Origin:      job.Origin,
		Destination: job.Destination,
		Date:        date,
	})
	if err != nil {
		reason := classify(err)
		if brk.RecordFailure(job.Airline) {
			logJob.Warn("circuit opened for airline", "cooldown", breaker.Cooldown.String())
		}
		logJob.Error("scrape failed", "err", err, "reason", reason)
		retry(ctx, cfg, producer, logJob, job, reason)
		return
	}
	brk.RecordSuccess(job.Airline)

	doc := storage.RawScrape{
		Airline:     job.Airline,
		Origin:      job.Origin,
		Destination: job.Destination,
		SearchDate:  date,
		ScrapedAt:   time.Now(),
		RawPayload:  string(body),
	}
	// context.Background(), not the worker ctx: on SIGTERM the worker ctx would
	// abort a store the scraper just spent 30-45s of browser time producing. The
	// 10s bound guards against a genuine Mongo hang without letting shutdown race
	// the write away.
	storeCtx, cancel := context.WithTimeout(context.Background(), ioTimeout)
	err = storage.StoreRawScrape(storeCtx, client, doc)
	cancel()
	if err != nil {
		logJob.Error("store failed", "err", err, "reason", "store")
		retry(ctx, cfg, producer, logJob, job, "store")
		return
	}

	// A scrape that stores an empty result set is still a success (a route/date
	// with no award space legitimately returns nothing), but selector drift on
	// the DOM extractors looks identical, so warn — it's the only in-pipeline
	// signal that an extractor may have broken.
	if hasResults, err := airlines.HasResultsFor(job.Airline, body); err != nil {
		logJob.Warn("could not determine result count", "err", err)
	} else if !hasResults {
		logJob.Warn("scrape stored an empty result set", "reason", "empty_result")
	}

	logJob.Info("job done", "bytes", len(body))
}

// retry re-queues job as a fresh message with an incremented attempt after an
// exponential backoff, or drops it once cfg.MaxAttempts tries are used up. The
// original message is already committed (at-most-once), so if ctx is cancelled
// during the backoff or the enqueue fails, the job is simply dropped.
func retry(ctx context.Context, cfg config.Config, producer *queue.Producer, logJob *slog.Logger, job queue.ScrapeJob, reason string) {
	next := job.Attempt + 1
	if next >= cfg.MaxAttempts {
		logJob.Error("job failed permanently, giving up", "reason", reason, "attempts", next)
		return
	}

	delay := backoff(cfg.RetryBackoffBase, job.Attempt)
	logJob.Info("re-queueing job after backoff", "reason", reason, "next_attempt", next, "backoff", delay.String())
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		logJob.Warn("shutdown during backoff, retry abandoned", "reason", reason)
		return
	}

	job.Attempt = next
	// context.Background() with a 10s ceiling, same rationale as the store: let
	// the re-enqueue finish through shutdown, but don't let a Kafka outage park
	// the worker here indefinitely. A timeout drops the job (at-most-once).
	enqueueCtx, cancel := context.WithTimeout(context.Background(), ioTimeout)
	defer cancel()
	if err := producer.Enqueue(enqueueCtx, job); err != nil {
		logJob.Error("re-queue failed, job dropped", "err", err)
	}
}

// backoff returns base doubled once per prior attempt, capped at maxBackoff.
// attempt is the count of tries already made (0 for the first retry).
func backoff(base time.Duration, attempt int) time.Duration {
	d := base << attempt
	if d <= 0 || d > maxBackoff { // <=0 catches shift overflow
		return maxBackoff
	}
	return d
}

// classify buckets a scrape error into a coarse reason for structured logging.
// It is best-effort string matching, not a typed-error contract.
func classify(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "deadline exceeded"), strings.Contains(s, "timeout"), strings.Contains(s, "timed out"):
		return "timeout"
	case strings.Contains(s, "403"), strings.Contains(s, "access denied"),
		strings.Contains(s, "blocked"), strings.Contains(s, "captcha"), strings.Contains(s, "bot"):
		return "blocked"
	case strings.Contains(s, "net::err"), strings.Contains(s, "playwright"),
		strings.Contains(s, "chromium"), strings.Contains(s, "browser"):
		return "browser"
	default:
		return "other"
	}
}
