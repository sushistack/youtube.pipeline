package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestPhaseBRunner_Run_UsesErrgroupWithoutSiblingCancellation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	imageFailed := make(chan struct{})
	ttsCompleted := make(chan struct{})
	var scenarioLoaded atomic.Bool
	var ttsCtxErr atomic.Value

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			if req.Scenario != nil && req.Scenario.RunID == req.RunID {
				scenarioLoaded.Store(true)
			}
			close(imageFailed)
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage, CostUSD: 0.12, DurationMs: 1200},
			}, errors.New("image failed")
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			<-imageFailed
			if err := ctx.Err(); err != nil {
				ttsCtxErr.Store(err)
			}
			close(ttsCompleted)
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, CostUSD: 0.03, DurationMs: 900},
			}, nil
		},
		nil,
		clock.RealClock{},
		logger,
		nil,
		nil,
	)

	res, err := runner.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected image failure")
	}
	<-ttsCompleted
	if !scenarioLoaded.Load() {
		t.Fatal("scenario not loaded into request")
	}
	if v := ttsCtxErr.Load(); v != nil {
		t.Fatalf("sibling error canceled tts context: %v", v)
	}
	if res.Image.Err == nil {
		t.Fatal("expected typed image track failure")
	}
	if res.TTS.Err != nil {
		t.Fatalf("unexpected tts failure: %v", res.TTS.Err)
	}
}

func TestPhaseBRunner_Run_WaitsForBothTracksBeforeReturning(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	releaseTTS := make(chan struct{})
	ttsStarted := make(chan struct{})
	imageFinished := make(chan struct{})
	done := make(chan struct{})

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			defer close(imageFinished)
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage, DurationMs: 1},
			}, errors.New("boom")
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			close(ttsStarted)
			<-releaseTTS
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, DurationMs: 1},
			}, nil
		},
		nil,
		clock.RealClock{},
		logger,
		nil,
		nil,
	)

	go func() {
		defer close(done)
		_, _ = runner.Run(context.Background(), req)
	}()

	// Wait until both tracks have observably started/finished before asserting
	// the runner has not returned; this removes scheduler-dependent flakiness.
	<-imageFinished
	<-ttsStarted

	select {
	case <-done:
		t.Fatal("runner returned before sibling track finished")
	default:
	}

	close(releaseTTS)
	<-done
}

func TestPhaseBRunner_Run_ImageFailsTTSSucceeds_PreservesTTSArtifacts(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	ttsPath := filepath.Join(t.TempDir(), "tts", "scene_01.wav")
	var assembleCalls int32

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage, CostUSD: 0.21, DurationMs: 2100},
			}, errors.New("image failed")
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			mustWrite(t, ttsPath, "wav")
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, CostUSD: 0.04, DurationMs: 400},
				Artifacts:   []string{ttsPath},
			}, nil
		},
		nil,
		clock.RealClock{},
		logger,
		func(ctx context.Context, res pipeline.PhaseBResult) error {
			atomic.AddInt32(&assembleCalls, 1)
			return nil
		},
		nil,
	)

	res, err := runner.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected mixed failure")
	}
	var trackErr *pipeline.PhaseBTrackError
	if !errors.As(err, &trackErr) || trackErr.Stage != domain.StageImage {
		t.Fatalf("expected image track error, got %v", err)
	}
	if _, statErr := os.Stat(ttsPath); statErr != nil {
		t.Fatalf("preserved tts artifact missing: %v", statErr)
	}
	if res.TTS.Err != nil {
		t.Fatalf("unexpected tts error: %v", res.TTS.Err)
	}
	testutil.AssertEqual(t, atomic.LoadInt32(&assembleCalls), int32(0))
}

func TestPhaseBRunner_Run_TTSFailsImageSucceeds_PreservesImages(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	imagePath := filepath.Join(t.TempDir(), "images", "scene_01", "shot_01.png")

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			mustWrite(t, imagePath, "img")
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage, CostUSD: 0.17, DurationMs: 1700},
				Artifacts:   []string{imagePath},
			}, nil
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, CostUSD: 0.02, DurationMs: 200},
			}, errors.New("tts failed")
		},
		nil,
		clock.RealClock{},
		logger,
		nil,
		nil,
	)

	res, err := runner.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected mixed failure")
	}
	var trackErr *pipeline.PhaseBTrackError
	if !errors.As(err, &trackErr) || trackErr.Stage != domain.StageTTS {
		t.Fatalf("expected tts track error, got %v", err)
	}
	if _, statErr := os.Stat(imagePath); statErr != nil {
		t.Fatalf("preserved image artifact missing: %v", statErr)
	}
	if res.Image.Err != nil {
		t.Fatalf("unexpected image error: %v", res.Image.Err)
	}
}

func TestPhaseBRunner_Run_BothTrackObservabilityRecordedOnMixedFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	store := &fakeObsStore{}
	logger, _ := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, nil, clock.RealClock{}, logger)
	req := phaseBRequest(t)

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			reason := "rate_limit"
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{
					Stage:       domain.StageImage,
					CostUSD:     0.31,
					DurationMs:  3100,
					RetryCount:  1,
					RetryReason: &reason,
				},
			}, errors.New("image failed")
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, CostUSD: 0.05, DurationMs: 500},
			}, nil
		},
		rec,
		clock.RealClock{},
		logger,
		nil,
		nil,
	)

	_, err := runner.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected mixed failure")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.calls) < 2 {
		t.Fatalf("expected image and tts observations, got %d", len(store.calls))
	}
	var imageObs *domain.StageObservation
	var ttsObs *domain.StageObservation
	for i := range store.calls {
		switch store.calls[i].Stage {
		case domain.StageImage:
			imageObs = &store.calls[i]
		case domain.StageTTS:
			ttsObs = &store.calls[i]
		}
	}
	if imageObs == nil || ttsObs == nil {
		t.Fatalf("missing image or tts observation: %+v", store.calls)
	}
	if imageObs.RetryReason == nil || *imageObs.RetryReason != "rate_limit" {
		t.Fatalf("image retry reason = %v, want rate_limit", imageObs.RetryReason)
	}
	testutil.AssertEqual(t, imageObs.CostUSD, 0.31)
	testutil.AssertEqual(t, ttsObs.CostUSD, 0.05)
}

func TestPhaseBRunner_Run_DoesNotAssembleUntilBothTracksSucceed(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	releaseTTS := make(chan struct{})
	assembleCalled := make(chan struct{}, 1)

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage, DurationMs: 100},
			}, nil
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			<-releaseTTS
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, DurationMs: 100},
			}, nil
		},
		nil,
		clock.RealClock{},
		logger,
		func(ctx context.Context, res pipeline.PhaseBResult) error {
			assembleCalled <- struct{}{}
			return nil
		},
		nil,
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runner.Run(context.Background(), req)
	}()

	select {
	case <-assembleCalled:
		t.Fatal("assembly started before both tracks completed")
	default:
	}

	close(releaseTTS)
	<-done
	<-assembleCalled
}

func TestPhaseBRunner_Run_NoAssemblyOnMixedFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	var assembleCalls int32

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage, DurationMs: 10},
			}, nil
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, DurationMs: 10},
			}, errors.New("tts failed")
		},
		nil,
		clock.RealClock{},
		logger,
		func(ctx context.Context, res pipeline.PhaseBResult) error {
			atomic.AddInt32(&assembleCalls, 1)
			return nil
		},
		nil,
	)

	if _, err := runner.Run(context.Background(), req); err == nil {
		t.Fatal("expected mixed failure")
	}
	testutil.AssertEqual(t, atomic.LoadInt32(&assembleCalls), int32(0))
}

func TestPhaseBRunner_Run_CapturesWallClockElapsed(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	store := &fakeObsStore{}
	fakeClock := clock.NewFakeClock(time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC))
	logger, logs := testutil.CaptureLog(t)
	rec := pipeline.NewRecorder(store, nil, fakeClock, logger)
	req := phaseBRequest(t)

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			if err := fakeClock.Sleep(ctx, 2*time.Second); err != nil {
				return pipeline.ImageTrackResult{}, err
			}
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage, DurationMs: 2000},
			}, nil
		},
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			if err := fakeClock.Sleep(ctx, 5*time.Second); err != nil {
				return pipeline.TTSTrackResult{}, err
			}
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, DurationMs: 5000},
			}, nil
		},
		rec,
		fakeClock,
		logger,
		nil,
		nil,
	)

	resCh := make(chan pipeline.PhaseBResult, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, _ := runner.Run(context.Background(), req)
		resCh <- res
	}()

	waitForPendingSleepers(t, fakeClock, 2)
	fakeClock.Advance(2 * time.Second)
	waitForPendingSleepers(t, fakeClock, 1)
	fakeClock.Advance(3 * time.Second)
	<-done
	res := <-resCh

	// AC-6: wall-clock is a Phase B summary signal surfaced via the runner's
	// return value and structured logging, NOT folded into the per-track
	// duration_ms SUM column. The Recorder should see only two observations
	// (one per track).
	testutil.AssertEqual(t, res.WallClockMs, int64(5000))

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.calls) != 2 {
		t.Fatalf("expected 2 per-track observations, got %d", len(store.calls))
	}

	if !strings.Contains(logs.String(), `"wall_clock_ms":5000`) {
		t.Fatalf("expected structured wall_clock_ms log entry; logs: %s", logs.String())
	}
}

// fakePhaseBRunLoader is a minimal PhaseBRunLoader implementation used to
// assert that the runner resolves runs.frozen_descriptor from the store and
// sets PhaseBRequest.FrozenDescriptorOverride before the image/tts tracks run.
type fakePhaseBRunLoader struct {
	run *domain.Run
	err error
}

func (f *fakePhaseBRunLoader) Get(ctx context.Context, id string) (*domain.Run, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.run, nil
}

// TestPhaseBRunner_Run_PopulatesFrozenDescriptorOverrideFromRunStore enforces
// AC-6 propagation at the Phase B entry point: if the caller did not already
// set FrozenDescriptorOverride and a RunLoader is configured, the runner must
// load runs.frozen_descriptor and inject it into the request before invoking
// the image/tts tracks. This prevents the "wired-but-forgotten" regression
// flagged in the Story 7.3 code review.
func TestPhaseBRunner_Run_PopulatesFrozenDescriptorOverrideFromRunStore(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	descriptor := "operator-edited descriptor"
	loader := &fakePhaseBRunLoader{run: &domain.Run{
		ID:               req.RunID,
		SCPID:            "049",
		FrozenDescriptor: &descriptor,
	}}

	var observedOverride *string
	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, r pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			if r.FrozenDescriptorOverride != nil {
				v := *r.FrozenDescriptorOverride
				observedOverride = &v
			}
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage},
			}, nil
		},
		func(ctx context.Context, r pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS},
			}, nil
		},
		nil,
		clock.RealClock{},
		logger,
		nil,
		loader,
	)

	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if observedOverride == nil {
		t.Fatal("expected FrozenDescriptorOverride to be set by runner, got nil")
	}
	if *observedOverride != descriptor {
		t.Fatalf("FrozenDescriptorOverride = %q, want %q", *observedOverride, descriptor)
	}
}

// TestPhaseBRunner_Run_PreservesCallerSuppliedOverride verifies that when the
// caller pre-populates FrozenDescriptorOverride, the runner does NOT overwrite
// it from the run store — preserves the legacy contract where the caller can
// bypass the DB lookup (e.g., for testing or forced re-runs).
func TestPhaseBRunner_Run_PreservesCallerSuppliedOverride(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	caller := "caller-supplied"
	req.FrozenDescriptorOverride = &caller

	stored := "stored-in-db"
	loader := &fakePhaseBRunLoader{run: &domain.Run{
		ID:               req.RunID,
		SCPID:            "049",
		FrozenDescriptor: &stored,
	}}

	logger, _ := testutil.CaptureLog(t)
	var observedOverride *string
	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, r pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			if r.FrozenDescriptorOverride != nil {
				v := *r.FrozenDescriptorOverride
				observedOverride = &v
			}
			return pipeline.ImageTrackResult{Observation: domain.StageObservation{Stage: domain.StageImage}}, nil
		},
		func(ctx context.Context, r pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			return pipeline.TTSTrackResult{Observation: domain.StageObservation{Stage: domain.StageTTS}}, nil
		},
		nil,
		clock.RealClock{},
		logger,
		nil,
		loader,
	)

	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if observedOverride == nil || *observedOverride != caller {
		t.Fatalf("caller override was overwritten: got %v, want %q", observedOverride, caller)
	}
}

func phaseBRequest(t *testing.T) pipeline.PhaseBRequest {
	t.Helper()
	runDir := t.TempDir()
	scenarioPath := filepath.Join(runDir, "scenario.json")
	raw, err := json.MarshalIndent(agents.PipelineState{
		RunID: "scp-049-run-1",
		SCPID: "049",
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	if err := os.WriteFile(scenarioPath, raw, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return pipeline.PhaseBRequest{
		RunID:        "scp-049-run-1",
		Stage:        domain.StageImage,
		ScenarioPath: scenarioPath,
		Segments: []*domain.Episode{{
			RunID:      "scp-049-run-1",
			SceneIndex: 0,
			Shots: []domain.Shot{{
				ImagePath:        "images/scene_01/shot_01.png",
				DurationSeconds:  5,
				Transition:       domain.TransitionKenBurns,
				VisualDescriptor: "close-up",
			}},
		}},
	}
}

func waitForPendingSleepers(t *testing.T, clk *clock.FakeClock, want int) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		if clk.PendingSleepers() >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("fake clock sleepers = %d, want at least %d", clk.PendingSleepers(), want)
}
