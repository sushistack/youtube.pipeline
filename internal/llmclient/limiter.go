package llmclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

const (
	defaultAcquireTimeout = 30 * time.Second
	semaphorePollInterval = 10 * time.Millisecond
	// loggableWaitThreshold gates rate-limiter / concurrency-wait log lines so
	// the steady state (sub-second waits) does not drown the operator. Any
	// wait above this is interesting in a dogfood diagnostic.
	loggableWaitThreshold = 500 * time.Millisecond
)

// LimitConfig controls one provider-scoped limiter instance.
type LimitConfig struct {
	RequestsPerMinute int
	MaxConcurrent     int64
	AcquireTimeout    time.Duration
}

// CallLimiter gates provider calls through a shared token bucket and semaphore.
// Story 5.2 depends on these instances being passed by pointer, not copied.
type CallLimiter struct {
	cfg      LimitConfig
	clk      clock.Clock
	rpm      *rate.Limiter
	inflight *semaphore.Weighted
	// logger is the optional structured logger. When non-nil, non-trivial
	// waits (>= loggableWaitThreshold) emit observability events tagged with
	// the limiter name so a stuck Phase A can be distinguished from a hung
	// HTTP call. Use SetLogger to inject post-construction; the zero value
	// (nil) is safe and matches all existing call sites.
	logger *slog.Logger
	// name disambiguates limiter instances in shared logs (e.g. "dashscope"
	// vs. "deepseek") since one process owns several. Set via SetName.
	name string
}

// SetLogger attaches a structured logger to the limiter. Safe to call once
// at construction time before any Do() invocation. Subsequent calls overwrite.
// nil clears the logger (logging becomes a no-op).
func (l *CallLimiter) SetLogger(logger *slog.Logger) { l.logger = logger }

// SetName tags log lines emitted by this limiter so an operator can tell
// "dashscope is rate-limited" apart from "deepseek is rate-limited" in a
// shared stdout. Empty string clears the tag.
func (l *CallLimiter) SetName(name string) { l.name = name }

func NewCallLimiter(cfg LimitConfig, clk clock.Clock) (*CallLimiter, error) {
	if cfg.RequestsPerMinute <= 0 || cfg.MaxConcurrent <= 0 || cfg.AcquireTimeout <= 0 {
		return nil, fmt.Errorf("call limiter config invalid: %w", domain.ErrValidation)
	}
	if clk == nil {
		return nil, fmt.Errorf("call limiter clock is required: %w", domain.ErrValidation)
	}
	interval := time.Minute / time.Duration(cfg.RequestsPerMinute)
	return &CallLimiter{
		cfg:      cfg,
		clk:      clk,
		rpm:      rate.NewLimiter(rate.Every(interval), 1),
		inflight: semaphore.NewWeighted(cfg.MaxConcurrent),
	}, nil
}

func (l *CallLimiter) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := l.waitTurn(ctx); err != nil {
		return err
	}
	if err := l.acquire(ctx); err != nil {
		return err
	}
	defer l.inflight.Release(1)

	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- fmt.Errorf("provider call panicked: %v: %w", r, domain.ErrStageFailed)
			}
		}()
		resultCh <- fn(callCtx)
	}()

	timeoutCh := make(chan error, 1)
	go func() {
		timeoutCh <- l.clk.Sleep(callCtx, l.cfg.AcquireTimeout)
	}()

	select {
	case err := <-resultCh:
		return err
	case err := <-timeoutCh:
		if err != nil {
			return err
		}
		cancel()
		return fmt.Errorf("provider call exceeded %s: %w", l.cfg.AcquireTimeout, domain.ErrStageFailed)
	}
}

func (l *CallLimiter) waitTurn(ctx context.Context) error {
	reservation := l.rpm.ReserveN(l.clk.Now(), 1)
	if !reservation.OK() {
		return fmt.Errorf("rate limiter reservation rejected: %w", domain.ErrStageFailed)
	}
	delay := reservation.DelayFrom(l.clk.Now())
	if delay <= 0 {
		return nil
	}
	if l.logger != nil && delay >= loggableWaitThreshold {
		l.logger.Info("rate limiter wait",
			"provider", l.name,
			"delay_ms", delay.Milliseconds(),
			"rpm", l.cfg.RequestsPerMinute,
		)
	}
	if err := l.clk.Sleep(ctx, delay); err != nil {
		reservation.CancelAt(l.clk.Now())
		return err
	}
	return nil
}

// acquire polls the semaphore via clk.Sleep at a fixed interval. Under
// clock.FakeClock, callers must advance the clock to unblock the poll loop.
// Real-clock paths back off for 10ms between TryAcquire attempts.
func (l *CallLimiter) acquire(ctx context.Context) error {
	deadline := l.clk.Now().Add(l.cfg.AcquireTimeout)
	start := l.clk.Now()
	logged := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if l.inflight.TryAcquire(1) {
			if logged && l.logger != nil {
				l.logger.Info("concurrency slot acquired",
					"provider", l.name,
					"waited_ms", l.clk.Now().Sub(start).Milliseconds(),
					"max_concurrent", l.cfg.MaxConcurrent,
				)
			}
			return nil
		}
		remaining := deadline.Sub(l.clk.Now())
		if remaining <= 0 {
			return fmt.Errorf("provider concurrency acquire timed out after %s: %w", l.cfg.AcquireTimeout, domain.ErrStageFailed)
		}
		if !logged && l.logger != nil && l.clk.Now().Sub(start) >= loggableWaitThreshold {
			l.logger.Info("concurrency wait",
				"provider", l.name,
				"max_concurrent", l.cfg.MaxConcurrent,
				"acquire_timeout_ms", l.cfg.AcquireTimeout.Milliseconds(),
			)
			logged = true
		}
		step := semaphorePollInterval
		if remaining < step {
			step = remaining
		}
		if err := l.clk.Sleep(ctx, step); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("provider concurrency wait interrupted: %w", err)
		}
	}
}

// ProviderLimiterConfig wires one shared DashScope limiter plus isolated text
// provider limiters. AcquireTimeout defaults to 30s when omitted.
type ProviderLimiterConfig struct {
	DashScope LimitConfig
	DeepSeek  LimitConfig
	Gemini    LimitConfig
}

type ProviderLimiterFactory struct {
	dashScope *CallLimiter
	deepSeek  *CallLimiter
	gemini    *CallLimiter
}

func NewProviderLimiterFactory(cfg ProviderLimiterConfig, clk clock.Clock) (*ProviderLimiterFactory, error) {
	dashScope, err := NewCallLimiter(normalizeLimitConfig(cfg.DashScope), clk)
	if err != nil {
		return nil, fmt.Errorf("dashscope limiter: %w", err)
	}
	dashScope.SetName("dashscope")
	deepSeek, err := NewCallLimiter(normalizeLimitConfig(cfg.DeepSeek), clk)
	if err != nil {
		return nil, fmt.Errorf("deepseek limiter: %w", err)
	}
	deepSeek.SetName("deepseek")
	gemini, err := NewCallLimiter(normalizeLimitConfig(cfg.Gemini), clk)
	if err != nil {
		return nil, fmt.Errorf("gemini limiter: %w", err)
	}
	gemini.SetName("gemini")
	return &ProviderLimiterFactory{
		dashScope: dashScope,
		deepSeek:  deepSeek,
		gemini:    gemini,
	}, nil
}

// SetLogger injects a logger into all underlying limiters. Called once at
// server startup so dogfood diagnostics see rate-limiter / concurrency wait
// events in the same JSON stream as request logs.
func (f *ProviderLimiterFactory) SetLogger(logger *slog.Logger) {
	if f == nil {
		return
	}
	f.dashScope.SetLogger(logger)
	f.deepSeek.SetLogger(logger)
	f.gemini.SetLogger(logger)
}

func (f *ProviderLimiterFactory) DashScopeImage() *CallLimiter { return f.dashScope }
func (f *ProviderLimiterFactory) DashScopeTTS() *CallLimiter   { return f.dashScope }
func (f *ProviderLimiterFactory) DashScopeText() *CallLimiter  { return f.dashScope }
func (f *ProviderLimiterFactory) DeepSeekText() *CallLimiter   { return f.deepSeek }
func (f *ProviderLimiterFactory) GeminiText() *CallLimiter     { return f.gemini }

func normalizeLimitConfig(cfg LimitConfig) LimitConfig {
	if cfg.AcquireTimeout == 0 {
		cfg.AcquireTimeout = defaultAcquireTimeout
	}
	return cfg
}
