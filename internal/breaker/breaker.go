// Package breaker is a minimal in-memory circuit breaker keyed by a string
// (an airline ID, here). It exists so the worker pool stops launching browsers
// against an airline site that is clearly down until a cooldown passes.
//
// It is deliberately simple: no half-open trial state, no metrics, process-local
// only. A per-airline breaker with real state machines is a Phase 6 concern.
package breaker

import (
	"sync"
	"time"
)

// Trips the breaker after this many consecutive failures for a key; stays open
// for Cooldown after tripping.
const (
	Threshold = 3
	Cooldown  = 60 * time.Second
)

type state struct {
	consecutiveFailures int
	openUntil           time.Time
}

// Breaker tracks failure state per key. The zero value is not usable; call New.
type Breaker struct {
	now func() time.Time // overridable in tests

	mu   sync.Mutex
	keys map[string]*state
}

// New returns a ready Breaker.
func New() *Breaker {
	return &Breaker{now: time.Now, keys: make(map[string]*state)}
}

func (b *Breaker) get(key string) *state {
	s := b.keys[key]
	if s == nil {
		s = &state{}
		b.keys[key] = s
	}
	return s
}

// Allow reports whether work for key may proceed. It returns false while the
// breaker for key is open.
func (b *Breaker) Allow(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.now().Before(b.get(key).openUntil)
}

// RecordSuccess clears any failure state for key.
func (b *Breaker) RecordSuccess(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.get(key)
	s.consecutiveFailures = 0
	s.openUntil = time.Time{}
}

// RecordFailure counts a failure for key and, once Threshold consecutive
// failures are reached, opens the breaker for Cooldown. It returns true if this
// call tripped the breaker.
func (b *Breaker) RecordFailure(key string) (tripped bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.get(key)
	s.consecutiveFailures++
	if s.consecutiveFailures >= Threshold {
		s.consecutiveFailures = 0
		s.openUntil = b.now().Add(Cooldown)
		return true
	}
	return false
}
