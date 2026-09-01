// Command worker runs a pool of concurrent workers that pull ScrapeJobs off the
// scrape.jobs Kafka topic, scrape each airline, and store the raw result in
// MongoDB. Run it alongside the stack, then dispatch jobs with cmd/producer.
//
// Only airlines registered in internal/scraper/airlines.Scrapers are scraped;
// jobs for others are logged and skipped until their scraper lands.
//
// Failure handling (Step 5): a failed scrape or store is re-queued with an
// incremented attempt count after an exponential backoff, up to cfg.MaxAttempts
// tries, then dropped with a structured "giving up" log (a dead-letter topic is
// Phase 6). A per-airline circuit breaker fails jobs fast for a cooldown once an
// airline site has failed repeatedly, so the pool stops launching browsers at a
// site that is down.
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
				process(ctx, cfg, client, consumer, producer, brk, id, msg)
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
		select {
		case jobs <- msg:
		case <-ctx.Done():
		}
	}

	close(jobs)
	wg.Wait()
	slog.Info("worker pool stopped")
}

func process(ctx context.Context, cfg config.Config, client *mongo.Client, consumer *queue.Consumer, producer *queue.Producer, brk *breaker.Breaker, workerID int, msg kafka.Message) {
	job, err := queue.Decode(msg)
	if err != nil {
		slog.Error("undecodable job, skipping", "worker", workerID, "err", err, "raw", string(msg.Value))
		commit(ctx, consumer, msg)
		return
	}

	logJob := slog.With("worker", workerID, "airline", job.Airline, "origin", job.Origin,
		"destination", job.Destination, "date", job.Date, "cabin", job.Cabin, "attempt", job.Attempt)

	// Bad airline or date can never succeed on a retry — commit and move on.
	scrapeFn, ok := airlines.Scrapers[job.Airline]
	if !ok {
		logJob.Warn("no scraper registered for airline, skipping")
		commit(ctx, consumer, msg)
		return
	}
	date, err := time.Parse("2006-01-02", job.Date)
	if err != nil {
		logJob.Error("invalid job date, skipping", "err", err)
		commit(ctx, consumer, msg)
		return
	}

	if !brk.Allow(job.Airline) {
		logJob.Warn("circuit open for airline, deferring job", "reason", "circuit_open")
		retry(ctx, cfg, producer, consumer, logJob, job, msg, "circuit_open")
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
		retry(ctx, cfg, producer, consumer, logJob, job, msg, reason)
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
	if err := storage.StoreRawScrape(context.Background(), client, doc); err != nil {
		logJob.Error("store failed", "err", err, "reason", "store")
		retry(ctx, cfg, producer, consumer, logJob, job, msg, "store")
		return
	}

	commit(ctx, consumer, msg)
	logJob.Info("job done", "bytes", len(body))
}

// retry re-queues job with an incremented attempt after an exponential backoff,
// or drops it once cfg.MaxAttempts tries are used up. The original message is
// committed only once the retry copy is safely enqueued (or the job is given up
// on); if ctx is cancelled during the backoff the job is left uncommitted so
// Kafka redelivers it on the next run.
func retry(ctx context.Context, cfg config.Config, producer *queue.Producer, consumer *queue.Consumer, logJob *slog.Logger, job queue.ScrapeJob, msg kafka.Message, reason string) {
	next := job.Attempt + 1
	if next >= cfg.MaxAttempts {
		logJob.Error("job failed permanently, giving up", "reason", reason, "attempts", next)
		commit(ctx, consumer, msg)
		return
	}

	delay := backoff(cfg.RetryBackoffBase, job.Attempt)
	logJob.Info("re-queueing job after backoff", "reason", reason, "next_attempt", next, "backoff", delay.String())
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		logJob.Warn("shutdown during backoff, job left for redelivery", "reason", reason)
		return
	}

	job.Attempt = next
	if err := producer.Enqueue(context.Background(), job); err != nil {
		logJob.Error("re-queue failed, job left for redelivery", "err", err)
		return
	}
	commit(ctx, consumer, msg)
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

// commit acknowledges msg back to Kafka. During shutdown ctx is already
// cancelled, so it falls back to a fresh timeout to still record finished work.
func commit(ctx context.Context, consumer *queue.Consumer, msg kafka.Message) {
	commitCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		commitCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	if err := consumer.Commit(commitCtx, msg); err != nil {
		slog.Error("commit failed", "err", err, "partition", msg.Partition, "offset", msg.Offset)
	}
}
