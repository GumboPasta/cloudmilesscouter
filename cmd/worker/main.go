// Command worker runs a pool of concurrent workers that pull ScrapeJobs off the
// scrape.jobs Kafka topic, scrape each airline, and store the raw result in
// MongoDB. Run it alongside the stack, then dispatch jobs with cmd/producer.
//
// Only airlines registered in internal/scraper/airlines.Scrapers are scraped;
// jobs for others are logged and skipped until their scraper lands (Step 4).
// Failed scrapes are logged and the job is committed anyway — re-queue with
// backoff is Step 5's job.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/segmentio/kafka-go"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/queue"
	"cloudmilesscouter/internal/scraper"
	"cloudmilesscouter/internal/scraper/airlines"
	"cloudmilesscouter/internal/storage"
)

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("worker pool started", "workers", cfg.WorkerCount, "brokers", cfg.KafkaBrokers, "group", cfg.KafkaGroupID)

	// One fetch loop feeds a buffered channel that the workers drain. The buffer
	// lets the loop stay a step ahead without pulling more than the pool can hold.
	jobs := make(chan kafka.Message, cfg.WorkerCount)

	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for msg := range jobs {
				process(ctx, cfg, client, consumer, id, msg)
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

func process(ctx context.Context, cfg config.Config, client *mongo.Client, consumer *queue.Consumer, workerID int, msg kafka.Message) {
	job, err := queue.Decode(msg)
	if err != nil {
		slog.Error("undecodable job, skipping", "worker", workerID, "err", err, "raw", string(msg.Value))
		commit(ctx, consumer, msg)
		return
	}

	logJob := slog.With("worker", workerID, "airline", job.Airline,
		"origin", job.Origin, "destination", job.Destination, "date", job.Date, "cabin", job.Cabin)

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

	logJob.Info("job started")
	body, err := scrapeFn(cfg, scraper.SearchParams{
		Origin:      job.Origin,
		Destination: job.Destination,
		Date:        date,
	})
	if err != nil {
		logJob.Error("scrape failed", "err", err)
		commit(ctx, consumer, msg) // Step 5 owns re-queue + backoff
		return
	}

	doc := storage.RawScrape{
		Airline:     job.Airline,
		Origin:      job.Origin,
		Destination: job.Destination,
		SearchDate:  date,
		ScrapedAt:   time.Now(),
		RawPayload:  string(body),
	}
	if err := storage.StoreRawScrape(context.Background(), client, doc); err != nil {
		// Leave the job uncommitted so it is redelivered rather than lost.
		logJob.Error("store failed, job will be redelivered", "err", err)
		return
	}

	commit(ctx, consumer, msg)
	logJob.Info("job done", "bytes", len(body))
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
