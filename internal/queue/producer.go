package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
)

// Topic is the Kafka topic scrape jobs are dispatched to. DLQTopic is the
// dead-letter topic a job lands on once the worker has exhausted its retries
// (Phase 6 Step 4). Both are created on boot by the kafka-init service in
// docker/docker-compose.yml.
const (
	Topic    = "scrape.jobs"
	DLQTopic = "scrape.jobs.dlq"
)

// ScrapeJob is one airline's worth of work: scrape this route, on this date.
// One search fans out into one ScrapeJob per airline.
type ScrapeJob struct {
	Airline     string `json:"airline"`           // airline ID, e.g. "united"
	Origin      string `json:"origin"`            // IATA airport or metro code, e.g. "DFW", "NYC"
	Destination string `json:"destination"`       // IATA airport or metro code, e.g. "JFK"
	Date        string `json:"date"`              // departure date, YYYY-MM-DD
	Attempt     int    `json:"attempt,omitempty"` // 0 on first dispatch, +1 each time a worker re-queues it after a failure
}

// DeadLetterJob wraps a ScrapeJob the worker gave up on with the context needed
// to diagnose it later: the coarse failure reason, how many attempts were made,
// the last error string, and when it was dead-lettered.
type DeadLetterJob struct {
	Job      ScrapeJob `json:"job"`
	Reason   string    `json:"reason"`
	Attempts int       `json:"attempts"`
	Error    string    `json:"error"`
	FailedAt time.Time `json:"failed_at"`
}

// Producer writes ScrapeJobs into the scrape.jobs topic and, for jobs the worker
// gives up on, DeadLetterJobs into scrape.jobs.dlq.
type Producer struct {
	w   *kafka.Writer
	dlq *kafka.Writer
}

// NewProducer builds a Producer that connects to the given comma-separated
// broker list. Messages are keyed by airline so the same airline always lands
// on the same partition.
func NewProducer(brokers string) *Producer {
	addrs := kafka.TCP(splitBrokers(brokers)...)
	newWriter := func(topic string) *kafka.Writer {
		return &kafka.Writer{
			Addr:         addrs,
			Topic:        topic,
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireAll,
			// Each write is its own synchronous WriteMessages call, so the
			// writer's default 1s BatchTimeout added ~1s of latency per job — a
			// four-airline dispatch took ~4s. The messages are tiny and dispatch
			// is low-volume; flush almost immediately.
			BatchTimeout: 50 * time.Millisecond,
		}
	}
	return &Producer{w: newWriter(Topic), dlq: newWriter(DLQTopic)}
}

// Enqueue marshals job to JSON and writes it to the topic, keyed by airline.
func (p *Producer) Enqueue(ctx context.Context, job ScrapeJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err := p.w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(job.Airline),
		Value: payload,
	}); err != nil {
		return err
	}
	slog.Info("job dispatched", "airline", job.Airline, "origin", job.Origin, "destination", job.Destination, "date", job.Date, "attempt", job.Attempt)
	return nil
}

// DeadLetter marshals dl to JSON and writes it to the dead-letter topic, keyed
// by airline. The worker calls it once a job has used up cfg.MaxAttempts.
func (p *Producer) DeadLetter(ctx context.Context, dl DeadLetterJob) error {
	payload, err := json.Marshal(dl)
	if err != nil {
		return err
	}
	if err := p.dlq.WriteMessages(ctx, kafka.Message{
		Key:   []byte(dl.Job.Airline),
		Value: payload,
	}); err != nil {
		return err
	}
	slog.Warn("dead-lettered job", "airline", dl.Job.Airline, "origin", dl.Job.Origin,
		"destination", dl.Job.Destination, "date", dl.Job.Date, "reason", dl.Reason, "attempts", dl.Attempts)
	return nil
}

// Close flushes and closes the underlying writers.
func (p *Producer) Close() error {
	return errors.Join(p.w.Close(), p.dlq.Close())
}
