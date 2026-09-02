package queue

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/segmentio/kafka-go"
)

// splitBrokers turns a comma-separated broker list ("host1:9092,host2:9092")
// into a slice, trimming whitespace and dropping empty entries. A single
// address with no comma comes back as a one-element slice.
func splitBrokers(brokers string) []string {
	var addrs []string
	for _, b := range strings.Split(brokers, ",") {
		if b = strings.TrimSpace(b); b != "" {
			addrs = append(addrs, b)
		}
	}
	return addrs
}

// Consumer reads ScrapeJobs from the scrape.jobs topic as part of a consumer
// group, so several workers (or worker processes) share the topic's partitions.
type Consumer struct {
	r *kafka.Reader
}

// NewConsumer builds a Consumer joined to groupID against the given
// comma-separated broker list. The fetch loop commits each message as soon as
// it is pulled, before the scrape runs, so delivery is at-most-once: a worker
// that crashes mid-scrape does not get the job back. The recovery path for a
// failed scrape is the re-enqueue in the worker's retry, which writes a fresh
// message.
func NewConsumer(brokers, groupID string) *Consumer {
	return &Consumer{
		r: kafka.NewReader(kafka.ReaderConfig{
			Brokers: splitBrokers(brokers),
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
