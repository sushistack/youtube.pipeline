package clock

import (
	"context"
	"testing"
	"time"
)

func TestRealClock_ImplementsClock(t *testing.T) {
	var _ Clock = RealClock{}
}

func TestFakeClock_ImplementsClock(t *testing.T) {
	var _ Clock = &FakeClock{}
}

func TestFakeClock_Now(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fc := NewFakeClock(start)
	if !fc.Now().Equal(start) {
		t.Errorf("Now() = %v, want %v", fc.Now(), start)
	}
}

func TestFakeClock_Advance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fc := NewFakeClock(start)
	fc.Advance(5 * time.Second)
	want := start.Add(5 * time.Second)
	if !fc.Now().Equal(want) {
		t.Errorf("after Advance(5s): Now() = %v, want %v", fc.Now(), want)
	}
}

func TestFakeClock_Sleep_ResolvedByAdvance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fc := NewFakeClock(start)

	done := make(chan error, 1)
	go func() {
		done <- fc.Sleep(context.Background(), 10*time.Second)
	}()

	// Give goroutine time to register the waiter
	time.Sleep(10 * time.Millisecond)

	fc.Advance(10 * time.Second)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Sleep returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Sleep did not resolve after Advance")
	}
}

func TestFakeClock_Sleep_ContextCancellation(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fc := NewFakeClock(start)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- fc.Sleep(ctx, time.Hour)
	}()

	// Give goroutine time to register
	time.Sleep(10 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Sleep error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Sleep did not return after context cancellation")
	}
}

func TestFakeClock_Sleep_ZeroDuration(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fc := NewFakeClock(start)
	if err := fc.Sleep(context.Background(), 0); err != nil {
		t.Errorf("Sleep(0) returned error: %v", err)
	}
}
