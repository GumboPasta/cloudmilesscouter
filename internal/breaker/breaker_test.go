package breaker

import (
	"testing"
	"time"
)

func TestBreaker(t *testing.T) {
	now := time.Now()
	b := New()
	b.now = func() time.Time { return now }

	if !b.Allow("delta") {
		t.Fatal("fresh key should be allowed")
	}

	// Failures below the threshold do not open the breaker.
	for i := 0; i < Threshold-1; i++ {
		if tripped := b.RecordFailure("delta"); tripped {
			t.Fatalf("tripped early after %d failures", i+1)
		}
		if !b.Allow("delta") {
			t.Fatalf("closed breaker should allow after %d failures", i+1)
		}
	}

	// The threshold-th consecutive failure trips it.
	if tripped := b.RecordFailure("delta"); !tripped {
		t.Fatal("expected trip at threshold")
	}
	if b.Allow("delta") {
		t.Fatal("open breaker should block")
	}
	// Other keys are unaffected.
	if !b.Allow("united") {
		t.Fatal("unrelated key should still be allowed")
	}

	// It closes again once the cooldown elapses.
	now = now.Add(Cooldown + time.Second)
	if !b.Allow("delta") {
		t.Fatal("breaker should close after cooldown")
	}

	// A success resets the failure count.
	b.RecordFailure("delta")
	b.RecordSuccess("delta")
	for i := 0; i < Threshold-1; i++ {
		if b.RecordFailure("delta") {
			t.Fatalf("count not reset by success: tripped after %d post-reset failures", i+1)
		}
	}
}
