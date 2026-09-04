package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/queue"
)

func TestBackoff(t *testing.T) {
	base := 2 * time.Second
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
		{4, maxBackoff},  // 32s clamped to 30s
		{10, maxBackoff}, // well past the cap
		{63, maxBackoff}, // shift overflow -> negative -> cap
		{64, maxBackoff}, // shift >= 64 is undefined-ish -> cap
	}
	for _, c := range cases {
		if got := backoff(base, c.attempt); got != c.want {
			t.Errorf("backoff(%s, %d) = %s, want %s", base, c.attempt, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err  string
		want string
	}{
		{"context deadline exceeded", "timeout"},
		{"Timeout 30000ms exceeded waiting for selector", "timeout"},
		{"navigation timed out", "timeout"},
		{"403 Forbidden", "blocked"},
		{"Access Denied (reference #...)", "blocked"},
		{"hit a captcha challenge", "blocked"},
		{"net::ERR_NAME_NOT_RESOLVED", "browser"},
		{"playwright: driver exited", "browser"},
		{"chromium crashed", "browser"},
		{"some unmapped failure", "other"},
	}
	for _, c := range cases {
		if got := classify(errString(c.err)); got != c.want {
			t.Errorf("classify(%q) = %q, want %q", c.err, got, c.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestForceFailure(t *testing.T) {
	cases := []struct {
		name    string
		list    []string
		airline string
		want    bool
	}{
		{"empty config", nil, "delta", false},
		{"named", []string{"delta"}, "delta", true},
		{"one of several", []string{"united", "delta"}, "delta", true},
		{"not named", []string{"united"}, "delta", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.Config{ScraperForceFailure: c.list}
			if got := forceFailure(cfg, c.airline); got != c.want {
				t.Errorf("forceFailure(%v, %q) = %v, want %v", c.list, c.airline, got, c.want)
			}
		})
	}
}

// fakeRequeuer records the retry path's calls so tests can assert which branch
// ran without a live Kafka.
type fakeRequeuer struct {
	enqueued    []queue.ScrapeJob
	deadLetters []queue.DeadLetterJob
}

func (f *fakeRequeuer) Enqueue(_ context.Context, job queue.ScrapeJob) error {
	f.enqueued = append(f.enqueued, job)
	return nil
}

func (f *fakeRequeuer) DeadLetter(_ context.Context, dl queue.DeadLetterJob) error {
	f.deadLetters = append(f.deadLetters, dl)
	return nil
}

// TestRetryDeadLettersAtMaxAttempts checks the boundary: a job whose next
// attempt would reach cfg.MaxAttempts is dead-lettered (not re-enqueued) without
// sleeping on a backoff.
func TestRetryDeadLettersAtMaxAttempts(t *testing.T) {
	cfg := config.Config{MaxAttempts: 3, RetryBackoffBase: time.Second}
	job := queue.ScrapeJob{Airline: "united", Attempt: 2} // next would be 3 == MaxAttempts
	fake := &fakeRequeuer{}

	done := make(chan struct{})
	go func() {
		retry(context.Background(), cfg, fake, slog.Default(), job, "timeout", "navigation timed out")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry did not return promptly on the give-up boundary (it must not sleep before dead-lettering)")
	}

	if len(fake.enqueued) != 0 {
		t.Fatalf("re-enqueued %d jobs at the give-up boundary, want 0", len(fake.enqueued))
	}
	if len(fake.deadLetters) != 1 {
		t.Fatalf("dead-lettered %d jobs, want 1", len(fake.deadLetters))
	}
	dl := fake.deadLetters[0]
	if dl.Job != job || dl.Reason != "timeout" || dl.Attempts != 3 || dl.Error != "navigation timed out" {
		t.Fatalf("dead-letter = %+v, want job %+v reason=timeout attempts=3 error=%q", dl, job, "navigation timed out")
	}
}

// TestRetryReEnqueuesBelowMaxAttempts checks the other branch: a job with tries
// left is re-enqueued with an incremented attempt and not dead-lettered.
func TestRetryReEnqueuesBelowMaxAttempts(t *testing.T) {
	cfg := config.Config{MaxAttempts: 3, RetryBackoffBase: time.Millisecond}
	job := queue.ScrapeJob{Airline: "delta", Attempt: 0}
	fake := &fakeRequeuer{}

	retry(context.Background(), cfg, fake, slog.Default(), job, "timeout", "boom")

	if len(fake.deadLetters) != 0 {
		t.Fatalf("dead-lettered %d jobs with tries left, want 0", len(fake.deadLetters))
	}
	if len(fake.enqueued) != 1 || fake.enqueued[0].Attempt != 1 {
		t.Fatalf("enqueued = %+v, want one job with Attempt=1", fake.enqueued)
	}
}
