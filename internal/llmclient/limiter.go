package llmclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

const (
	defaultAcquireTimeout = 30 * time.Second
	semaphorePollInterval = 10 * time.Millisecond
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
}

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
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if l.inflight.TryAcquire(1) {
			return nil
		}
		remaining := deadline.Sub(l.clk.Now())
		if remaining <= 0 {
			return fmt.Errorf("provider concurrency acquire timed out after %s: %w", l.cfg.AcquireTimeout, domain.ErrStageFailed)
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
	deepSeek, err := NewCallLimiter(normalizeLimitConfig(cfg.DeepSeek), clk)
	if err != nil {
		return nil, fmt.Errorf("deepseek limiter: %w", err)
	}
	gemini, err := NewCallLimiter(normalizeLimitConfig(cfg.Gemini), clk)
	if err != nil {
		return nil, fmt.Errorf("gemini limiter: %w", err)
	}
	return &ProviderLimiterFactory{
		dashScope: dashScope,
		deepSeek:  deepSeek,
		gemini:    gemini,
	}, nil
}

func (f *ProviderLimiterFactory) DashScopeImage() *CallLimiter { return f.dashScope }
func (f *ProviderLimiterFactory) DashScopeTTS() *CallLimiter   { return f.dashScope }
func (f *ProviderLimiterFactory) DeepSeekText() *CallLimiter   { return f.deepSeek }
func (f *ProviderLimiterFactory) GeminiText() *CallLimiter     { return f.gemini }

func normalizeLimitConfig(cfg LimitConfig) LimitConfig {
	if cfg.AcquireTimeout == 0 {
		cfg.AcquireTimeout = defaultAcquireTimeout
	}
	return cfg
}
