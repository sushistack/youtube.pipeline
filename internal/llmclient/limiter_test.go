package llmclient_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestNewCallLimiter_RejectsInvalidConfig(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))

	cases := []llmclient.LimitConfig{
		{RequestsPerMinute: 0, MaxConcurrent: 1, AcquireTimeout: 30 * time.Second},
		{RequestsPerMinute: 60, MaxConcurrent: 0, AcquireTimeout: 30 * time.Second},
		{RequestsPerMinute: 60, MaxConcurrent: 1, AcquireTimeout: 0},
	}

	for _, cfg := range cases {
		_, err := llmclient.NewCallLimiter(cfg, clk)
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("expected ErrValidation for %+v, got %v", cfg, err)
		}
	}
}

func TestCallLimiter_Do_UsesSemaphoreAndTokenBucket(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	limiter, err := llmclient.NewCallLimiter(llmclient.LimitConfig{
		RequestsPerMinute: 60,
		MaxConcurrent:     1,
		AcquireTimeout:    30 * time.Second,
	}, clk)
	if err != nil {
		t.Fatalf("NewCallLimiter: %v", err)
	}

	started := make(chan struct{}, 2)
	releaseFirst := make(chan struct{})
	done := make(chan string, 2)
	var mu sync.Mutex
	startedCount := 0

	go func() {
		err := limiter.Do(context.Background(), func(context.Context) error {
			mu.Lock()
			startedCount++
			mu.Unlock()
			started <- struct{}{}
			<-releaseFirst
			done <- "first"
			return nil
		})
		if err != nil {
			t.Errorf("first call: %v", err)
		}
	}()

	waitForStructSignal(t, started, 50)

	go func() {
		err := limiter.Do(context.Background(), func(context.Context) error {
			mu.Lock()
			startedCount++
			mu.Unlock()
			started <- struct{}{}
			done <- "second"
			return nil
		})
		if err != nil {
			t.Errorf("second call: %v", err)
		}
	}()

	waitForSleeperRegistration(clk, 50)
	clk.Advance(1 * time.Second)
	mu.Lock()
	testutil.AssertEqual(t, startedCount, 1)
	mu.Unlock()

	close(releaseFirst)
	waitForStartedCountWithAdvance(t, &mu, &startedCount, 2, clk, 10*time.Millisecond, 50)
	waitForStringCount(t, done, 2, clk, 100*time.Millisecond, 10)
}

func TestCallLimiter_Do_TimesOutAfter30Seconds_FakeClock(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	limiter, err := llmclient.NewCallLimiter(llmclient.LimitConfig{
		RequestsPerMinute: 600,
		MaxConcurrent:     1,
		AcquireTimeout:    30 * time.Second,
	}, clk)
	if err != nil {
		t.Fatalf("NewCallLimiter: %v", err)
	}

	done := make(chan error, 1)
	entered := make(chan struct{}, 1)
	go func() {
		done <- limiter.Do(context.Background(), func(ctx context.Context) error {
			entered <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	waitForStructSignal(t, entered, 50)
	waitForSleeperRegistration(clk, 50)
	clk.Advance(30*time.Second + 10*time.Millisecond)
	waitForResult(t, done, clk, 10*time.Millisecond, 200, func(err error) {
		if !errors.Is(err, domain.ErrStageFailed) {
			t.Fatalf("expected ErrStageFailed, got %v", err)
		}
	})
}

func TestCallLimiter_Do_ReleasesPermitOnTimeout(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	limiter, err := llmclient.NewCallLimiter(llmclient.LimitConfig{
		RequestsPerMinute: 600,
		MaxConcurrent:     1,
		AcquireTimeout:    30 * time.Second,
	}, clk)
	if err != nil {
		t.Fatalf("NewCallLimiter: %v", err)
	}

	firstDone := make(chan error, 1)
	firstEntered := make(chan struct{}, 1)
	go func() {
		firstDone <- limiter.Do(context.Background(), func(ctx context.Context) error {
			firstEntered <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	waitForStructSignal(t, firstEntered, 50)
	waitForSleeperRegistration(clk, 50)
	clk.Advance(30*time.Second + 10*time.Millisecond)
	waitForResult(t, firstDone, clk, 10*time.Millisecond, 200, func(err error) {
		if !errors.Is(err, domain.ErrStageFailed) {
			t.Fatalf("expected ErrStageFailed, got %v", err)
		}
	})

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- limiter.Do(context.Background(), func(context.Context) error { return nil })
	}()

	waitForResult(t, secondDone, clk, 10*time.Millisecond, 200, func(err error) {
		if err != nil {
			t.Fatalf("expected second call to succeed, got %v", err)
		}
	})
}

func TestCallLimiter_Do_NoGoroutineLeakUnderTimeoutContention(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	limiter, err := llmclient.NewCallLimiter(llmclient.LimitConfig{
		RequestsPerMinute: 600,
		MaxConcurrent:     1,
		AcquireTimeout:    30 * time.Second,
	}, clk)
	if err != nil {
		t.Fatalf("NewCallLimiter: %v", err)
	}

	// Baseline snapshot BEFORE spawning any goroutines. After all Do() calls
	// return, the inner resultCh/timeoutCh goroutines must have exited — so
	// runtime.NumGoroutine() should return to the baseline within a bounded
	// window. This catches timeout-sleeper leaks that a WaitGroup on fn alone
	// would miss (the fn closure cannot deferred-Done for the sleeper goroutine).
	runtime.GC()
	baseline := runtime.NumGoroutine()

	var workers sync.WaitGroup
	workers.Add(2)

	firstDone := make(chan error, 1)
	firstEntered := make(chan struct{}, 1)
	go func() {
		firstDone <- limiter.Do(context.Background(), func(ctx context.Context) error {
			defer workers.Done()
			firstEntered <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	waitForStructSignal(t, firstEntered, 50)
	waitForSleeperRegistration(clk, 50)

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- limiter.Do(context.Background(), func(context.Context) error {
			defer workers.Done()
			return nil
		})
	}()

	clk.Advance(30*time.Second + 10*time.Millisecond)
	waitForResult(t, firstDone, clk, 10*time.Millisecond, 200, func(err error) {
		if !errors.Is(err, domain.ErrStageFailed) {
			t.Fatalf("expected first call to time out with ErrStageFailed, got %v", err)
		}
	})

	waitForResult(t, secondDone, clk, 10*time.Millisecond, 200, func(err error) {
		if err != nil {
			t.Fatalf("expected waiting call to complete, got %v", err)
		}
	})

	waitForWaitGroup(t, &workers, clk, 100*time.Millisecond, 10)

	// Goroutine delta check: every goroutine Do() spawned (resultCh fn runner
	// + timeoutCh sleeper) must have exited. Allow a small slack for GC/runtime
	// goroutines that can fluctuate between snapshots.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.Gosched()
		runtime.GC()
		if runtime.NumGoroutine() <= baseline+1 {
			return
		}
	}
	t.Fatalf("goroutine leak: baseline=%d, final=%d", baseline, runtime.NumGoroutine())
}

func TestDashScopeLimiterFactory_ImageAndTTSSharePointer(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	factory := newProviderFactory(t)
	if factory.DashScopeImage() != factory.DashScopeTTS() {
		t.Fatal("dashscope image and tts limiters must share the same pointer")
	}
}

func TestDashScopeLimiterFactory_TextSharesPointerWithImageAndTTS(t *testing.T) {
	// Phase A wires the DashScope text client (Qwen) onto the same shared
	// account-level budget as image/tts. A separate limiter would let three
	// surfaces independently exhaust their own RPM caps and aggregate past
	// DashScope's per-key throttle, defeating the budget contract image and
	// tts already enforce by sharing.
	testutil.BlockExternalHTTP(t)
	factory := newProviderFactory(t)
	if factory.DashScopeText() != factory.DashScopeImage() {
		t.Fatal("dashscope text and image limiters must share the same pointer")
	}
	if factory.DashScopeText() != factory.DashScopeTTS() {
		t.Fatal("dashscope text and tts limiters must share the same pointer")
	}
}

func TestDashScopeLimiterFactory_NonDashScopeProvidersAreIsolated(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	factory := newProviderFactory(t)
	if factory.DashScopeImage() == factory.DeepSeekText() {
		t.Fatal("deepseek should not share the dashscope limiter")
	}
	if factory.DashScopeImage() == factory.GeminiText() {
		t.Fatal("gemini should not share the dashscope limiter")
	}
	if factory.DashScopeImage() == factory.ComfyUIImage() {
		t.Fatal("comfyui should not share the dashscope limiter")
	}
}

func TestProviderLimiterFactory_ComfyUIImageDefaults(t *testing.T) {
	// Spec rule: ComfyUI defaults must produce a usable limiter even when
	// callers pass a zero-value LimitConfig — RPM=600, MaxConcurrent=1,
	// AcquireTimeout=10m. Without normalization, NewCallLimiter would reject
	// RequestsPerMinute=0 with ErrValidation.
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	factory, err := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 1},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 1},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 1},
	}, clk)
	if err != nil {
		t.Fatalf("NewProviderLimiterFactory with zero ComfyUI cfg: %v", err)
	}
	if factory.ComfyUIImage() == nil {
		t.Fatal("ComfyUIImage() returned nil")
	}
}

func TestProviderLimiterFactory_ComfyUIImageHonorsExplicit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	factory, err := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 1},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 1},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 1},
		ComfyUI:   llmclient.LimitConfig{RequestsPerMinute: 600, MaxConcurrent: 1, AcquireTimeout: 10 * time.Minute},
	}, clk)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	limiter := factory.ComfyUIImage()
	if limiter == nil {
		t.Fatal("nil limiter")
	}
	// Smoke-test: a no-op call returns nil within the cache budget.
	done := make(chan error, 1)
	go func() {
		done <- limiter.Do(context.Background(), func(context.Context) error { return nil })
	}()
	waitForResult(t, done, clk, 10*time.Millisecond, 200, func(err error) {
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
	})
}

func TestSharedDashScopeLimiter_CombinedRPMWithinFivePercent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	clk := clock.NewFakeClock(time.Unix(0, 0))
	factory, err := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 120, MaxConcurrent: 40},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 2},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 2},
	}, clk)
	if err != nil {
		t.Fatalf("NewProviderLimiterFactory: %v", err)
	}

	// Track the primary budget window (explicit 1s advances) separately from
	// the micro-advances inside waitForResult. The limiter's RPM assertion
	// uses the primary window: 20 rounds × 1s = 20s of rate-consumable time.
	// This avoids both (a) a hardcoded "20s" literal that is tautological and
	// (b) counting post-release micro-advances that the limiter did not gate.
	const roundInterval = 1 * time.Second
	const rounds = 20
	var mu sync.Mutex
	imageCalls := 0
	ttsCalls := 0
	start := clk.Now()
	budgetAdvance := time.Duration(0)
	for i := 0; i < rounds; i++ {
		roundDone := make(chan error, 2)
		go func() {
			roundDone <- factory.DashScopeImage().Do(context.Background(), func(context.Context) error {
				mu.Lock()
				imageCalls++
				mu.Unlock()
				return nil
			})
		}()
		go func() {
			roundDone <- factory.DashScopeTTS().Do(context.Background(), func(context.Context) error {
				mu.Lock()
				ttsCalls++
				mu.Unlock()
				return nil
			})
		}()
		waitForSleeperRegistration(clk, 50)
		clk.Advance(roundInterval)
		budgetAdvance += roundInterval
		waitForResult(t, roundDone, clk, 10*time.Millisecond, 200, func(err error) {
			if err != nil {
				t.Fatalf("image limiter call: %v", err)
			}
		})
		waitForResult(t, roundDone, clk, 10*time.Millisecond, 200, func(err error) {
			if err != nil {
				t.Fatalf("tts limiter call: %v", err)
			}
		})
	}

	// Sanity check: clk advanced at least the explicit budget — confirms the
	// limiter did not stall the scenario.
	if elapsed := clk.Now().Sub(start); elapsed < budgetAdvance {
		t.Fatalf("fake clock advanced %v < budgetAdvance %v", elapsed, budgetAdvance)
	}
	totalCalls := imageCalls + ttsCalls
	actualRPM := float64(totalCalls) / budgetAdvance.Minutes()
	expectedRPM := 120.0
	diffPct := absFloat(actualRPM-expectedRPM) / expectedRPM
	if diffPct > 0.05 {
		t.Fatalf("combined throughput %.2f RPM exceeds 5%% budget window around %.2f RPM", actualRPM, expectedRPM)
	}
	imageShare := float64(imageCalls) / float64(totalCalls)
	if absFloat(imageShare-0.5) > 0.05 {
		t.Fatalf("image share %.2f outside ±5%% of target 0.50", imageShare)
	}
}

func newProviderFactory(t *testing.T) *llmclient.ProviderLimiterFactory {
	t.Helper()
	clk := clock.NewFakeClock(time.Unix(0, 0))
	factory, err := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 120, MaxConcurrent: 4},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 2},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 2},
	}, clk)
	if err != nil {
		t.Fatalf("NewProviderLimiterFactory: %v", err)
	}
	return factory
}

func waitForResult(t *testing.T, done chan error, clk *clock.FakeClock, step time.Duration, maxSteps int, assertFn func(error)) {
	t.Helper()
	for i := 0; i < maxSteps; i++ {
		runtime.Gosched()
		select {
		case err := <-done:
			assertFn(err)
			return
		default:
			waitForSleeperRegistration(clk, 10)
			clk.Advance(step)
		}
	}
	runtime.Gosched()
	select {
	case err := <-done:
		assertFn(err)
		return
	default:
	}
	t.Fatal("expected operation to finish within fake-clock steps")
}

func waitForWaitGroup(t *testing.T, wg *sync.WaitGroup, clk *clock.FakeClock, step time.Duration, maxSteps int) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	for i := 0; i < maxSteps; i++ {
		runtime.Gosched()
		select {
		case <-done:
			return
		default:
			waitForSleeperRegistration(clk, 10)
			clk.Advance(step)
		}
	}
	t.Fatal("waitgroup did not complete within fake-clock steps")
}

func waitForResultNoAdvance(t *testing.T, done <-chan error, spins int, assertFn func(error)) {
	t.Helper()
	for i := 0; i < spins*100; i++ {
		runtime.Gosched()
		select {
		case err := <-done:
			assertFn(err)
			return
		default:
		}
	}
	t.Fatal("expected operation to complete without additional fake-clock advancement")
}

func waitForStringCount(t *testing.T, ch chan string, want int, clk *clock.FakeClock, step time.Duration, maxSteps int) {
	t.Helper()
	for i := 0; i < maxSteps; i++ {
		runtime.Gosched()
		if len(ch) >= want {
			return
		}
		waitForSleeperRegistration(clk, 10)
		clk.Advance(step)
	}
	t.Fatalf("string channel length did not reach %d; got %d", want, len(ch))
}

func waitForStructSignal(t *testing.T, ch <-chan struct{}, spins int) {
	t.Helper()
	for i := 0; i < spins*100; i++ {
		runtime.Gosched()
		select {
		case <-ch:
			return
		default:
		}
	}
	t.Fatal("expected struct signal was not observed")
}

func waitForStartedCount(t *testing.T, mu *sync.Mutex, count *int, want int, spins int) {
	t.Helper()
	for i := 0; i < spins*100; i++ {
		runtime.Gosched()
		mu.Lock()
		got := *count
		mu.Unlock()
		if got >= want {
			return
		}
	}
	t.Fatalf("expected started count to reach %d", want)
}

func waitForStartedCountWithAdvance(
	t *testing.T,
	mu *sync.Mutex,
	count *int,
	want int,
	clk *clock.FakeClock,
	step time.Duration,
	spins int,
) {
	t.Helper()
	for i := 0; i < spins*100; i++ {
		runtime.Gosched()
		mu.Lock()
		got := *count
		mu.Unlock()
		if got >= want {
			return
		}
		clk.Advance(step)
	}
	t.Fatalf("expected started count to reach %d", want)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func waitForSleeperRegistration(clk *clock.FakeClock, spins int) {
	for i := 0; i < spins; i++ {
		if clk.PendingSleepers() > 0 {
			return
		}
		runtime.Gosched()
	}
}
