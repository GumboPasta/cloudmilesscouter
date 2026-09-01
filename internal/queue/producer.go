package queue

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/segmentio/kafka-go"
)

// Topic is the single Kafka topic scrape jobs are dispatched to. It is created
// on boot by the kafka-init service in docker/docker-compose.yml.
const Topic = "scrape.jobs"

// ScrapeJob is one airline's worth of work: scrape this route, on this date,
// in this cabin. One search fans out into one ScrapeJob per airline.
type ScrapeJob struct {
	Airline     string `json:"airline"`     // airline ID, e.g. "united"
	Origin      string `json:"origin"`      // IATA airport or metro code, e.g. "DFW", "NYC"
	Destination string `json:"destination"` // IATA airport or metro code, e.g. "JFK"
	Date        string `json:"date"`        // departure date, YYYY-MM-DD
	Cabin       string `json:"cabin"`       // e.g. "economy", "business"
}

// Producer writes ScrapeJobs into the scrape.jobs topic.
type Producer struct {
	w *kafka.Writer
}

// NewProducer builds a Producer that connects to the given comma-separated
// broker list. Messages are keyed by airline so the same airline always lands
// on the same partition.
func NewProducer(brokers string) *Producer {
	return &Producer{
		w: &kafka.Writer{
			Addr:         kafka.TCP(brokers),
			Topic:        Topic,
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireAll,
		},
	}
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
	slog.Info("job dispatched", "airline", job.Airline, "origin", job.Origin, "destination", job.Destination, "date", job.Date, "cabin", job.Cabin)
	return nil
}

// Close flushes and closes the underlying writer.
func (p *Producer) Close() error {
	return p.w.Close()
}
