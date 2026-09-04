package breaker

import (
	"testing"
	"time"
)

func TestBreakerTripsAndRecovers(t *testing.T) {
	now := time.Now()
	b := New(DefaultThreshold, DefaultCooldown)
	b.now = func() time.Time { return now }

	if !b.Allow("delta") {
		t.Fatal("fresh key should be allowed")
	}

	// Failures below the threshold do not open the breaker.
	for i := 0; i < DefaultThreshold-1; i++ {
		if opened := b.RecordFailure("delta"); opened {
			t.Fatalf("opened early after %d failures", i+1)
		}
		if !b.Allow("delta") {
			t.Fatalf("closed breaker should allow after %d failures", i+1)
		}
	}

	// The threshold-th consecutive failure opens it.
	if opened := b.RecordFailure("delta"); !opened {
		t.Fatal("expected open at threshold")
	}
	if b.State("delta") != Open {
		t.Fatalf("state = %v, want open", b.State("delta"))
	}
	if b.Allow("delta") {
		t.Fatal("open breaker should block")
	}
	// Other keys are unaffected.
	if !b.Allow("united") {
		t.Fatal("unrelated key should still be allowed")
	}

	// A success resets the failure count.
	now = now.Add(DefaultCooldown + time.Second)
	b.RecordSuccess("delta")
	if b.State("delta") != Closed {
		t.Fatalf("state after success = %v, want closed", b.State("delta"))
	}
	for i := 0; i < DefaultThreshold-1; i++ {
		if b.RecordFailure("delta") {
			t.Fatalf("count not reset by success: opened after %d post-reset failures", i+1)
		}
	}
}

func TestBreakerHalfOpen(t *testing.T) {
	now := time.Now()
	b := New(3, 60*time.Second)
	b.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		b.RecordFailure("alaska")
	}
	if b.State("alaska") != Open {
		t.Fatalf("state = %v, want open", b.State("alaska"))
	}

	// Still open before the cooldown elapses.
	now = now.Add(59 * time.Second)
	if b.Allow("alaska") {
		t.Fatal("breaker should still be open before cooldown")
	}

	// Cooldown elapsed: exactly one probe is allowed, and the key is half-open.
	now = now.Add(2 * time.Second)
	if !b.Allow("alaska") {
		t.Fatal("first call after cooldown should be allowed as a probe")
	}
	if b.State("alaska") != HalfOpen {
		t.Fatalf("state = %v, want half-open", b.State("alaska"))
	}
	if b.Allow("alaska") {
		t.Fatal("second call while a probe is in flight should be refused")
	}

	// A failed probe re-opens for a fresh cooldown, even though the original
	// cooldown had already elapsed.
	if opened := b.RecordFailure("alaska"); !opened {
		t.Fatal("failed probe should report the breaker re-opened")
	}
	if b.Allow("alaska") {
		t.Fatal("breaker should be open again after a failed probe")
	}
	now = now.Add(61 * time.Second)
	if !b.Allow("alaska") {
		t.Fatal("probe should be allowed again after the new cooldown")
	}

	// A successful probe closes the breaker.
	b.RecordSuccess("alaska")
	if b.State("alaska") != Closed {
		t.Fatalf("state = %v, want closed after successful probe", b.State("alaska"))
	}
	if !b.Allow("alaska") {
		t.Fatal("closed breaker should allow")
	}
}

func TestNewDefaultsOnZero(t *testing.T) {
	b := New(0, 0)
	if b.threshold != DefaultThreshold || b.cooldown != DefaultCooldown {
		t.Fatalf("New(0,0) = {%d, %s}, want defaults {%d, %s}",
			b.threshold, b.cooldown, DefaultThreshold, DefaultCooldown)
	}
	if b.Cooldown() != DefaultCooldown {
		t.Fatalf("Cooldown() = %s, want %s", b.Cooldown(), DefaultCooldown)
	}
}
