// Package breaker is a minimal in-memory circuit breaker keyed by a string
// (an airline ID, here). It exists so the worker pool stops launching browsers
// against an airline site that is clearly down until a cooldown passes.
//
// It is a three-state machine per key — closed, open, half-open:
//
//   - closed: work is allowed. Threshold consecutive failures trip it to open.
//   - open: work is refused. After Cooldown the next Allow returns true once (a
//     probe) and moves the key to half-open.
//   - half-open: exactly one probe is in flight; further Allow calls are
//     refused. A success closes the breaker; a failure re-opens it for a fresh
//     Cooldown, so a still-down site is not hammered the instant its cooldown
//     lapses.
//
// It is process-local only: each worker process has its own view. That is fine
// here — the pool is one process — and keeps the type dependency-free.
package breaker

import (
	"sync"
	"time"
)

// Default trip parameters. DefaultThreshold is kept above the worker's
// MaxAttempts (default 3) so one job's full retry run can never trip the
// breaker on its own — only failures across distinct jobs do. cmd/worker
// overrides both from config; New falls back to these when passed zero.
const (
	DefaultThreshold = 5
	DefaultCooldown  = 60 * time.Second
)

// State is a breaker's current state for one key.
type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

type entry struct {
	state               State
	consecutiveFailures int
	openUntil           time.Time // when an open breaker becomes probe-eligible
}

// Breaker tracks state per key. The zero value is not usable; call New.
type Breaker struct {
	now       func() time.Time // overridable in tests
	threshold int
	cooldown  time.Duration

	mu   sync.Mutex
	keys map[string]*entry
}

// New returns a ready Breaker. A zero threshold or cooldown falls back to the
// package defaults.
func New(threshold int, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	return &Breaker{
		now:       time.Now,
		threshold: threshold,
		cooldown:  cooldown,
		keys:      make(map[string]*entry),
	}
}

// Cooldown reports the configured open-state cooldown, for callers that log it.
func (b *Breaker) Cooldown() time.Duration { return b.cooldown }

func (b *Breaker) get(key string) *entry {
	e := b.keys[key]
	if e == nil {
		e = &entry{}
		b.keys[key] = e
	}
	return e
}

// Allow reports whether work for key may proceed. A closed breaker always
// allows. An open breaker whose cooldown has elapsed allows exactly one probe
// and transitions to half-open; every other open or half-open call is refused.
func (b *Breaker) Allow(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.get(key)
	switch e.state {
	case Open:
		if b.now().Before(e.openUntil) {
			return false
		}
		e.state = HalfOpen // grant this caller the probe
		return true
	case HalfOpen:
		return false
	default: // Closed
		return true
	}
}

// State returns the current state for key.
func (b *Breaker) State(key string) State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.get(key).state
}

// RecordSuccess clears any failure state for key and closes the breaker.
func (b *Breaker) RecordSuccess(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.get(key)
	e.state = Closed
	e.consecutiveFailures = 0
	e.openUntil = time.Time{}
}

// RecordFailure counts a failure for key. A failed probe in half-open re-opens
// the breaker for another cooldown; in closed, the threshold-th consecutive
// failure opens it. It returns true if this call moved the breaker to open.
func (b *Breaker) RecordFailure(key string) (opened bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.get(key)

	if e.state == HalfOpen {
		e.state = Open
		e.openUntil = b.now().Add(b.cooldown)
		return true
	}

	e.consecutiveFailures++
	if e.consecutiveFailures >= b.threshold {
		e.state = Open
		e.consecutiveFailures = 0
		e.openUntil = b.now().Add(b.cooldown)
		return true
	}
	return false
}
