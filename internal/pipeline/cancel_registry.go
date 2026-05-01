package pipeline

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrCancelTimeout is returned by CancelRegistry.CancelAndWait when one or
// more in-flight workers fail to release their slot before the deadline.
// Rewind treats this as non-fatal — the subsequent DB cleanup and reset
// overwrite whatever the stalled worker eventually writes — but logs it so
// the operator knows a worker had to be left running.
var ErrCancelTimeout = errors.New("cancel timeout: in-flight worker did not release in time")

// CancelRegistry tracks in-flight stage execution per run so that a Rewind
// can signal cancellation and wait for clean unwinding before deleting the
// worker's outputs. Threadsafe for concurrent Begin/CancelAndWait across
// multiple runs.
//
// Per-run entries are stored as a slice so re-entrant Begin (e.g., a
// Resume that itself spawns a Phase A goroutine) keeps every concurrent
// worker visible to a single CancelAndWait.
type CancelRegistry struct {
	mu      sync.Mutex
	entries map[string][]*cancelEntry
}

type cancelEntry struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewCancelRegistry constructs an empty registry.
func NewCancelRegistry() *CancelRegistry {
	return &CancelRegistry{entries: map[string][]*cancelEntry{}}
}

// Begin registers a new in-flight worker for runID and returns a derived
// context that is cancelled when CancelAndWait fires (or when parent is
// cancelled). The release callback MUST be called exactly once when the
// worker exits (success or error) so a concurrent CancelAndWait can
// observe completion.
//
// Idiomatic usage:
//
//	ctx, _, release := registry.Begin(ctx, runID)
//	defer release()
func (r *CancelRegistry) Begin(parent context.Context, runID string) (context.Context, context.CancelFunc, func()) {
	if r == nil {
		// Nil receiver tolerated so callers can use a single code path
		// regardless of whether a registry is wired.
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, func() { cancel() }
	}
	ctx, cancel := context.WithCancel(parent)
	entry := &cancelEntry{cancel: cancel, done: make(chan struct{})}

	r.mu.Lock()
	r.entries[runID] = append(r.entries[runID], entry)
	r.mu.Unlock()

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			cancel()
			close(entry.done)
			r.mu.Lock()
			defer r.mu.Unlock()
			list := r.entries[runID]
			for i, e := range list {
				if e == entry {
					r.entries[runID] = append(list[:i], list[i+1:]...)
					break
				}
			}
			if len(r.entries[runID]) == 0 {
				delete(r.entries, runID)
			}
		})
	}
	return ctx, cancel, release
}

// CancelAndWait signals every in-flight worker on runID to abort and blocks
// until each has called its release. timeout caps the wait; on timeout the
// function returns ErrCancelTimeout but workers may still be running. The
// caller can choose to proceed (the Rewind path does — its subsequent
// deletions overwrite anything the stalled worker eventually writes).
//
// Returns nil immediately when no workers are registered for runID.
func (r *CancelRegistry) CancelAndWait(runID string, timeout time.Duration) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	list := append([]*cancelEntry(nil), r.entries[runID]...)
	r.mu.Unlock()
	if len(list) == 0 {
		return nil
	}
	for _, e := range list {
		e.cancel()
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for _, e := range list {
		select {
		case <-e.done:
		case <-deadline.C:
			return ErrCancelTimeout
		}
	}
	return nil
}

// ActiveCount returns the number of in-flight workers currently registered
// for runID. Test/debug aid; not part of the production cancel path.
func (r *CancelRegistry) ActiveCount(runID string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries[runID])
}
