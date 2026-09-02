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

// TestRetryGivesUpAtMaxAttempts checks the boundary: a job whose next attempt
// would reach cfg.MaxAttempts is dropped without touching the producer (a nil
// producer here would panic if it were used).
func TestRetryGivesUpAtMaxAttempts(t *testing.T) {
	cfg := config.Config{MaxAttempts: 3, RetryBackoffBase: time.Second}
	job := queue.ScrapeJob{Airline: "united", Attempt: 2} // next would be 3 == MaxAttempts

	done := make(chan struct{})
	go func() {
		retry(context.Background(), cfg, nil, slog.Default(), job, "timeout")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry did not return promptly on the give-up boundary (it must not sleep or enqueue)")
	}
}
