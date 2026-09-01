package queue

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/segmentio/kafka-go"
)

// Consumer reads ScrapeJobs from the scrape.jobs topic as part of a consumer
// group, so several workers (or worker processes) share the topic's partitions.
type Consumer struct {
	r *kafka.Reader

	mu        sync.Mutex
	committed map[int]int64 // partition -> highest committed offset+1
}

// NewConsumer builds a Consumer joined to groupID against the given
// comma-separated broker list. Offsets are committed explicitly via Commit
// after a job finishes, giving at-least-once delivery: a job whose worker
// crashes mid-scrape is redelivered on restart.
func NewConsumer(brokers, groupID string) *Consumer {
	return &Consumer{
		r: kafka.NewReader(kafka.ReaderConfig{
			Brokers: []string{brokers},
			GroupID: groupID,
			Topic:   Topic,
		}),
		committed: make(map[int]int64),
	}
}

// Fetch blocks until the next message is available, ctx is cancelled, or the
// reader is closed. The returned message is not committed — call Commit once
// its job is done.
func (c *Consumer) Fetch(ctx context.Context) (kafka.Message, error) {
	return c.r.FetchMessage(ctx)
}

// Commit marks msg (and everything before it on its partition) as processed.
// It is safe to call from multiple workers concurrently: kafka-go commits
// exactly the offset it is handed with no memory of earlier commits, so a
// worker finishing an older message after a newer one would otherwise drag the
// committed position backwards. Commit tracks the high-water mark per partition
// and skips any commit that would not advance it.
func (c *Consumer) Commit(ctx context.Context, msg kafka.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if msg.Offset+1 <= c.committed[msg.Partition] {
		return nil
	}
	if err := c.r.CommitMessages(ctx, msg); err != nil {
		return err
	}
	c.committed[msg.Partition] = msg.Offset + 1
	return nil
}

// Decode unmarshals a fetched message's value into a ScrapeJob.
func Decode(msg kafka.Message) (ScrapeJob, error) {
	var job ScrapeJob
	err := json.Unmarshal(msg.Value, &job)
	return job, err
}

// Close stops the reader and commits any pending offsets.
func (c *Consumer) Close() error {
	return c.r.Close()
}
