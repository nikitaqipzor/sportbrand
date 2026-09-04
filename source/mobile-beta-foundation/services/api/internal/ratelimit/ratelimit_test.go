package ratelimit_test

import (
	"sync"
	"testing"
	"time"

	"athletica.ai/api/internal/ratelimit"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newLimiter(cfg ratelimit.Config) (*ratelimit.Limiter, *clock) {
	c := &clock{t: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	return ratelimit.New(cfg, c.Now), c
}

func TestRequestBudgetIsEnforcedPerKey(t *testing.T) {
	limiter, c := newLimiter(ratelimit.Config{Limit: 3, Window: time.Minute})

	for i := range 3 {
		if d := limiter.Allow("ip:a"); !d.Allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	d := limiter.Allow("ip:a")
	if d.Allowed {
		t.Fatal("the fourth request in the window must be rejected")
	}
	if d.Reason != "rate" || d.RetryAfter <= 0 {
		t.Fatalf("decision = %+v, want reason=rate with a positive Retry-After", d)
	}

	// A different key keeps its own budget.
	if d := limiter.Allow("ip:b"); !d.Allowed {
		t.Fatal("an unrelated key must not be throttled")
	}

	// The window rolls over.
	c.Advance(time.Minute)
	if d := limiter.Allow("ip:a"); !d.Allowed {
		t.Fatal("the budget must refill once the window elapses")
	}
}

func TestFailureBackoffGrowsAndBlocks(t *testing.T) {
	limiter, c := newLimiter(ratelimit.Config{
		Limit: 100, Window: time.Minute,
		FailureLimit: 3, BackoffBase: 2 * time.Second, BackoffMax: time.Minute,
	})

	// Two failures are tolerated.
	limiter.Fail("account:a")
	limiter.Fail("account:a")
	if d := limiter.Allow("account:a"); !d.Allowed {
		t.Fatal("the account must not be locked before the failure limit")
	}

	// The third failure starts the lockout.
	limiter.Fail("account:a")
	d := limiter.Allow("account:a")
	if d.Allowed || d.Reason != "backoff" {
		t.Fatalf("decision = %+v, want a backoff lockout", d)
	}
	first := d.RetryAfter

	c.Advance(first)
	if d := limiter.Allow("account:a"); !d.Allowed {
		t.Fatal("the lockout must end after Retry-After")
	}

	// The next failure locks out for longer (exponential backoff).
	limiter.Fail("account:a")
	second := limiter.Allow("account:a").RetryAfter
	if second <= first {
		t.Fatalf("backoff did not grow: %s then %s", first, second)
	}
}

func TestBackoffIsCapped(t *testing.T) {
	limiter, _ := newLimiter(ratelimit.Config{
		Limit: 1000, Window: time.Minute,
		FailureLimit: 1, BackoffBase: time.Second, BackoffMax: 10 * time.Second,
	})
	for range 20 {
		limiter.Fail("account:a")
	}
	if d := limiter.Allow("account:a"); d.RetryAfter > 10*time.Second {
		t.Fatalf("Retry-After = %s, want at most the configured cap of 10s", d.RetryAfter)
	}
}

func TestSuccessClearsTheLockout(t *testing.T) {
	limiter, _ := newLimiter(ratelimit.Config{
		Limit: 100, Window: time.Minute,
		FailureLimit: 2, BackoffBase: time.Minute, BackoffMax: time.Hour,
	})

	limiter.Fail("account:a")
	limiter.Fail("account:a")
	if limiter.Allow("account:a").Allowed {
		t.Fatal("expected a lockout")
	}

	limiter.Succeed("account:a")
	if !limiter.Allow("account:a").Allowed {
		t.Fatal("a successful authentication must clear the failure history")
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	limiter, _ := newLimiter(ratelimit.Config{Limit: 1000, Window: time.Minute})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			limiter.Allow("shared")
			limiter.Fail("shared")
			limiter.Succeed("shared")
			limiter.Allow(string(rune('a' + i%26)))
		}(i)
	}
	wg.Wait()
}
