package clock

import (
	"context"
	"sync"
	"time"
)

// Clock abstracts time operations for testability.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// RealClock delegates to the real time package.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

func (RealClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FakeClock provides deterministic time control for tests.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	deadline time.Time
	ch       chan struct{}
}

// NewFakeClock creates a FakeClock starting at the given time.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// PendingSleepers returns the number of active Sleep waiters. Tests use this
// to drive fake time without outrunning goroutines before they register.
func (c *FakeClock) PendingSleepers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// Advance moves the clock forward by d and wakes any Sleep calls
// whose deadline has been reached.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var remaining []waiter
	for _, w := range c.waiters {
		if !c.now.Before(w.deadline) {
			close(w.ch)
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
	c.mu.Unlock()
}

// Sleep blocks until Advance moves past the deadline or ctx is cancelled.
func (c *FakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	deadline := c.now.Add(d)
	if !c.now.Before(deadline) {
		c.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	c.waiters = append(c.waiters, waiter{deadline: deadline, ch: ch})
	c.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		for i, w := range c.waiters {
			if w.ch == ch {
				c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		return ctx.Err()
	}
}
