// Package ratelimit provides the in-process throttling used to protect the
// authentication endpoints (audit finding H4).
//
// It combines two mechanisms per key:
//
//   - a fixed-window request counter, which caps how often a key may call an
//     endpoint at all;
//   - an exponential failure backoff, which locks a key out for a growing
//     period after repeated credential failures.
//
// Handlers key the limiter both by client IP and by account, so neither a
// single noisy IP nor a distributed spray against one account gets a free ride.
package ratelimit

import (
	"sync"
	"time"
)

// Config tunes a Limiter.
type Config struct {
	// Limit is the number of requests allowed per Window.
	Limit int
	// Window is the length of the fixed counting window.
	Window time.Duration
	// FailureLimit is how many consecutive failures are tolerated before the
	// backoff lockout starts.
	FailureLimit int
	// BackoffBase is the first lockout duration.
	BackoffBase time.Duration
	// BackoffMax caps the lockout duration.
	BackoffMax time.Duration
}

func (c Config) withDefaults() Config {
	if c.Limit <= 0 {
		c.Limit = 10
	}
	if c.Window <= 0 {
		c.Window = time.Minute
	}
	if c.FailureLimit <= 0 {
		c.FailureLimit = 5
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 2 * time.Second
	}
	if c.BackoffMax < c.BackoffBase {
		c.BackoffMax = 15 * time.Minute
	}
	return c
}

// Decision is the outcome of an Allow call.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
	// Reason is "rate" when the request budget is exhausted and "backoff" when
	// the key is locked out after repeated failures.
	Reason string
}

type entry struct {
	windowStart  time.Time
	count        int
	failures     int
	blockedUntil time.Time
	lastSeen     time.Time
}

// Limiter is a concurrency-safe in-process limiter.
type Limiter struct {
	mu      sync.Mutex
	cfg     Config
	now     func() time.Time
	entries map[string]*entry
	ops     int
	ttl     time.Duration
}

// New builds a Limiter. Pass now=nil for the wall clock.
func New(cfg Config, now func() time.Time) *Limiter {
	cfg = cfg.withDefaults()
	if now == nil {
		now = time.Now
	}
	ttl := cfg.Window
	if cfg.BackoffMax > ttl {
		ttl = cfg.BackoffMax
	}
	return &Limiter{cfg: cfg, now: now, entries: map[string]*entry{}, ttl: 2 * ttl}
}

// Allow consumes one request slot for key.
func (l *Limiter) Allow(key string) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.maybeSweep(now)
	e := l.entryFor(key, now)

	if now.Before(e.blockedUntil) {
		return Decision{Allowed: false, RetryAfter: e.blockedUntil.Sub(now), Reason: "backoff"}
	}
	if now.Sub(e.windowStart) >= l.cfg.Window {
		e.windowStart = now
		e.count = 0
	}
	if e.count >= l.cfg.Limit {
		return Decision{Allowed: false, RetryAfter: e.windowStart.Add(l.cfg.Window).Sub(now), Reason: "rate"}
	}
	e.count++
	return Decision{Allowed: true}
}

// Fail records an authentication failure for key and extends its lockout once
// the failure limit is reached.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	e := l.entryFor(key, now)
	e.failures++
	if e.failures >= l.cfg.FailureLimit {
		e.blockedUntil = now.Add(l.backoffFor(e.failures - l.cfg.FailureLimit + 1))
	}
}

// Succeed clears the failure history of key after a successful authentication.
func (l *Limiter) Succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if e, ok := l.entries[key]; ok {
		e.failures = 0
		e.blockedUntil = time.Time{}
		e.lastSeen = l.now()
	}
}

// backoffFor returns BackoffBase * 2^(step-1), capped at BackoffMax.
func (l *Limiter) backoffFor(step int) time.Duration {
	d := l.cfg.BackoffBase
	for i := 1; i < step; i++ {
		d *= 2
		if d >= l.cfg.BackoffMax {
			return l.cfg.BackoffMax
		}
	}
	if d > l.cfg.BackoffMax {
		return l.cfg.BackoffMax
	}
	return d
}

func (l *Limiter) entryFor(key string, now time.Time) *entry {
	e, ok := l.entries[key]
	if !ok {
		e = &entry{windowStart: now}
		l.entries[key] = e
	}
	e.lastSeen = now
	return e
}

// maybeSweep drops idle entries so the map cannot grow without bound.
func (l *Limiter) maybeSweep(now time.Time) {
	l.ops++
	if l.ops < 512 && len(l.entries) < 4096 {
		return
	}
	l.ops = 0
	for key, e := range l.entries {
		if now.Sub(e.lastSeen) > l.ttl && now.After(e.blockedUntil) {
			delete(l.entries, key)
		}
	}
}
