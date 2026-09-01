package queue

import (
	"context"
	"encoding/json"

	"github.com/segmentio/kafka-go"
)

// Consumer reads ScrapeJobs from the scrape.jobs topic as part of a consumer
// group, so several workers (or worker processes) share the topic's partitions.
type Consumer struct {
	r *kafka.Reader
}

// NewConsumer builds a Consumer joined to groupID against the given broker
// address. The fetch loop commits each message as soon as it is pulled, before
// the scrape runs, so delivery is at-most-once: a worker that crashes mid-scrape
// does not get the job back. The recovery path for a failed scrape is the
// re-enqueue in the worker's retry, which writes a fresh message.
func NewConsumer(brokers, groupID string) *Consumer {
	return &Consumer{
		r: kafka.NewReader(kafka.ReaderConfig{
			Brokers: []string{brokers},
			GroupID: groupID,
			Topic:   Topic,
		}),
	}
}

// Fetch blocks until the next message is available, ctx is cancelled, or the
// reader is closed.
func (c *Consumer) Fetch(ctx context.Context) (kafka.Message, error) {
	return c.r.FetchMessage(ctx)
}

// Commit marks msg (and everything before it on its partition) as processed.
// The fetch loop calls it from a single goroutine right after Fetch.
func (c *Consumer) Commit(ctx context.Context, msg kafka.Message) error {
	return c.r.CommitMessages(ctx, msg)
}

// Decode unmarshals a fetched message's value into a ScrapeJob.
func Decode(msg kafka.Message) (ScrapeJob, error) {
	var job ScrapeJob
	err := json.Unmarshal(msg.Value, &job)
	return job, err
}

// Close stops the reader.
func (c *Consumer) Close() error {
	return c.r.Close()
}
