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
// Failure handling: a failed scrape or store is re-queued as a fresh message
// with an incremented attempt count after an exponential backoff, up to
// cfg.MaxAttempts tries, then written to the scrape.jobs.dlq dead-letter topic
// with the failure reason and last error (Phase 6 Step 4). A per-airline
// circuit breaker (closed → open → half-open) fails jobs fast for a cooldown
// once an airline site has failed repeatedly, then lets a single probe job
// through before fully closing again, so the pool stops launching browsers at a
// site that is down. Poison (undecodable) messages are logged and dropped, not
// dead-lettered.
package main

import (
	"context"
	"errors"
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
	"cloudmilesscouter/internal/logging"
	"cloudmilesscouter/internal/metrics"
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
	logging.Setup("worker", cfg.LogLevel, cfg.LogFormat)

	connectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := storage.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		slog.Error("mongo connection failed", "err", err)
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())
	slog.Info("mongo reachable", "uri", cfg.MongoURI)

	consumer := queue.NewConsumer(cfg.KafkaBrokers, cfg.KafkaGroupID)
	defer consumer.Close()

	// The pool re-queues failed jobs through its own producer.
	producer := queue.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()

	brk := breaker.New(cfg.CircuitThreshold, cfg.CircuitCooldown)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Prometheus scrape target (Phase 6). The worker is not otherwise an HTTP
	// server, so it runs a bare /metrics listener. A bind failure is logged and
	// the pool carries on unmonitored rather than exiting.
	go func() {
		if err := metrics.ListenAndServe(cfg.MetricsAddr); err != nil {
			slog.Error("metrics listener stopped", "err", err, "addr", cfg.MetricsAddr)
		}
	}()
	go publishLag(ctx, consumer)

	slog.Info("worker pool started", "workers", cfg.WorkerCount, "brokers", cfg.KafkaBrokers,
		"group", cfg.KafkaGroupID, "max_attempts", cfg.MaxAttempts, "retry_backoff_base", cfg.RetryBackoffBase.String(),
		"circuit_threshold", cfg.CircuitThreshold, "circuit_cooldown", cfg.CircuitCooldown.String(),
		"metrics_addr", cfg.MetricsAddr, "force_failure", strings.Join(cfg.ScraperForceFailure, ","))

	// One fetch loop feeds an unbuffered channel that the workers drain. The loop
	// blocks on the handoff until a worker is free, so it stays exactly in step
	// with the pool and never commits more jobs than the pool can hold. Kafka
	// keeps the unfetched messages.
	jobs := make(chan kafka.Message)

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
			// A persistently broken reader (broker down, topic gone) would
			// otherwise spin this loop and flood the log. Pause before retrying,
			// but stay responsive to shutdown.
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
			}
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
	// Shutting down: the message is already committed (at-most-once), so there is
	// nothing to release — just don't start a fresh scrape. The producer
	// re-dispatches the search on its next cadence.
	if ctx.Err() != nil {
		return
	}

	job, err := queue.Decode(msg)
	if err != nil {
		slog.Error("undecodable job, skipping", "worker", workerID, "err", err, "raw", string(msg.Value))
		return
	}

	logJob := slog.With("worker", workerID, "airline", job.Airline, "origin", job.Origin,
		"destination", job.Destination, "date", job.Date, "attempt", job.Attempt)

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
		recordCircuitState(brk, job.Airline)
		logJob.Warn("circuit open for airline, dropping job", "reason", "circuit_open")
		return
	}
	if brk.State(job.Airline) == breaker.HalfOpen {
		logJob.Info("circuit half-open, probing airline with this job")
	}
	recordCircuitState(brk, job.Airline)

	logJob.Info("job started")
	metrics.ScrapeAttemptsTotal.WithLabelValues(job.Airline).Inc()
	start := time.Now()
	var body []byte
	if forceFailure(cfg, job.Airline) {
		// Phase 6 Step 5 validation knob: fail as if the airline site were down,
		// without launching a browser. "blocked" so classify() buckets it as an
		// outage on the scraper-health panels.
		err = errors.New("forced scrape failure: airline site blocked (SCRAPER_FORCE_FAILURE)")
	} else {
		body, err = scrapeFn(cfg, scraper.SearchParams{
			Origin:      job.Origin,
			Destination: job.Destination,
			Date:        date,
		})
	}
	metrics.ScrapeDuration.WithLabelValues(job.Airline).Observe(time.Since(start).Seconds())
	if err != nil {
		reason := classify(err)
		metrics.ScrapeFailuresTotal.WithLabelValues(job.Airline, reason).Inc()
		if brk.RecordFailure(job.Airline) {
			logJob.Warn("circuit opened for airline", "cooldown", cfg.CircuitCooldown.String())
		}
		recordCircuitState(brk, job.Airline)
		logJob.Error("scrape failed", "err", err, "reason", reason)
		retry(ctx, cfg, producer, logJob, job, reason, err.Error())
		return
	}
	brk.RecordSuccess(job.Airline)
	recordCircuitState(brk, job.Airline)

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
		metrics.ScrapeFailuresTotal.WithLabelValues(job.Airline, "store").Inc()
		logJob.Error("store failed", "err", err, "reason", "store")
		retry(ctx, cfg, producer, logJob, job, "store", err.Error())
		return
	}

	// A scrape that stores an empty result set is still a success (a route/date
	// with no award space legitimately returns nothing), but selector drift on
	// the DOM extractors looks identical, so warn — it's the only in-pipeline
	// signal that an extractor may have broken.
	if hasResults, err := airlines.HasResultsFor(job.Airline, body); err != nil {
		logJob.Warn("could not determine result count", "err", err)
	} else if !hasResults {
		metrics.ScrapeEmptyResultsTotal.WithLabelValues(job.Airline).Inc()
		logJob.Warn("scrape stored an empty result set", "reason", "empty_result")
	}

	logJob.Info("job done", "bytes", len(body))
}

// jobRequeuer is the subset of *queue.Producer the retry path uses: re-enqueue a
// job for another attempt, or dead-letter it once attempts are exhausted. The
// interface lets the worker tests inject a fake.
type jobRequeuer interface {
	Enqueue(ctx context.Context, job queue.ScrapeJob) error
	DeadLetter(ctx context.Context, dl queue.DeadLetterJob) error
}

// recordCircuitState publishes the breaker's current state for airline to the
// scrape_circuit_state gauge (0 closed, 1 open, 2 half-open).
func recordCircuitState(brk *breaker.Breaker, airline string) {
	metrics.ScrapeCircuitState.WithLabelValues(airline).Set(float64(brk.State(airline)))
}

// forceFailure reports whether airline is named in cfg.ScraperForceFailure — the
// Phase 6 Step 5 validation knob that fails a scrape without launching a browser
// so the breaker / retry / DLQ path can be exercised on the live stack.
func forceFailure(cfg config.Config, airline string) bool {
	for _, a := range cfg.ScraperForceFailure {
		if a == airline {
			return true
		}
	}
	return false
}

// retry re-queues job as a fresh message with an incremented attempt after an
// exponential backoff, or dead-letters it once cfg.MaxAttempts tries are used
// up. The original message is already committed (at-most-once), so if ctx is
// cancelled during the backoff or the enqueue fails, the job is simply dropped.
func retry(ctx context.Context, cfg config.Config, producer jobRequeuer, logJob *slog.Logger, job queue.ScrapeJob, reason, errMsg string) {
	next := job.Attempt + 1
	if next >= cfg.MaxAttempts {
		logJob.Error("job failed permanently, giving up", "reason", reason, "attempts", next)
		deadLetter(producer, logJob, job, reason, errMsg, next)
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

// deadLetter writes a permanently failed job to the scrape.jobs.dlq topic with
// the failure context. context.Background() with the same 10s ceiling as the
// re-enqueue: a DLQ write should survive shutdown but not hang on a broker
// outage. If it fails the job is lost — there is nowhere left to put it.
func deadLetter(producer jobRequeuer, logJob *slog.Logger, job queue.ScrapeJob, reason, errMsg string, attempts int) {
	dlqCtx, cancel := context.WithTimeout(context.Background(), ioTimeout)
	defer cancel()
	err := producer.DeadLetter(dlqCtx, queue.DeadLetterJob{
		Job:      job,
		Reason:   reason,
		Attempts: attempts,
		Error:    errMsg,
		FailedAt: time.Now(),
	})
	if err != nil {
		logJob.Error("dead-letter write failed, job lost", "err", err, "reason", reason)
		return
	}
	metrics.DLQMessagesTotal.WithLabelValues(job.Airline, reason).Inc()
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

// publishLag refreshes the kafka_consumer_lag gauge every 15s (one Prometheus
// scrape interval) from the reader's stats. kafka-go only recomputes lag on a
// fetch, so the gauge tracks the last message the pool pulled — good enough to
// watch the queue back up while the workers are busy. It returns on shutdown.
func publishLag(ctx context.Context, consumer *queue.Consumer) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metrics.KafkaConsumerLag.WithLabelValues(queue.Topic).Set(float64(consumer.Lag()))
		}
	}
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
