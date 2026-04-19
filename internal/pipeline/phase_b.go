package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

type ImageTrack func(ctx context.Context, req PhaseBRequest) (ImageTrackResult, error)
type TTSTrack func(ctx context.Context, req PhaseBRequest) (TTSTrackResult, error)
type PhaseBAssembler func(ctx context.Context, res PhaseBResult) error

type PhaseBRequest struct {
	RunID        string
	Stage        domain.Stage
	ScenarioPath string
	Segments     []*domain.Episode
	Scenario     *agents.PipelineState
	// FrozenDescriptorOverride, when non-nil and non-empty, replaces the
	// scenario artifact's FrozenDescriptor in every image prompt. Populated
	// by the pipeline runner from runs.frozen_descriptor after the operator
	// confirms a Vision Descriptor in the character pick phase. A nil
	// pointer preserves prior behavior (artifact value is used verbatim).
	FrozenDescriptorOverride *string
}

type ImageTrackResult struct {
	Observation domain.StageObservation
	Artifacts   []string
}

type TTSTrackResult struct {
	Observation domain.StageObservation
	Artifacts   []string
}

type PhaseBResult struct {
	Request      PhaseBRequest
	Image        ImageTrackOutcome
	TTS          TTSTrackOutcome
	WallClockMs  int64
	AssemblyDone bool
}

type ImageTrackOutcome struct {
	Result ImageTrackResult
	Err    error
}

type TTSTrackOutcome struct {
	Result TTSTrackResult
	Err    error
}

type PhaseBTrackError struct {
	Stage domain.Stage
	Err   error
}

func (e *PhaseBTrackError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return fmt.Sprintf("phase b %s track: %v", e.Stage, e.Err)
}

func (e *PhaseBTrackError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// PhaseBRunLoader is the minimal surface PhaseBRunner needs to resolve the
// operator-edited frozen descriptor at invocation time. It is satisfied by
// *db.RunStore but tests can supply a trivial fake. The loader is optional:
// when nil, PhaseBRunner consumes whatever FrozenDescriptorOverride the
// caller set on the request (or falls through to the artifact value).
type PhaseBRunLoader interface {
	Get(ctx context.Context, id string) (*domain.Run, error)
}

type PhaseBRunner struct {
	images    ImageTrack
	tts       TTSTrack
	recorder  *Recorder
	clock     clock.Clock
	logger    *slog.Logger
	assemble  PhaseBAssembler
	runLoader PhaseBRunLoader
}

func NewPhaseBRunner(
	images ImageTrack,
	tts TTSTrack,
	recorder *Recorder,
	clk clock.Clock,
	logger *slog.Logger,
	assemble PhaseBAssembler,
	runLoader PhaseBRunLoader,
) *PhaseBRunner {
	if clk == nil {
		clk = clock.RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PhaseBRunner{
		images:    images,
		tts:       tts,
		recorder:  recorder,
		clock:     clk,
		logger:    logger,
		assemble:  assemble,
		runLoader: runLoader,
	}
}

func (r *PhaseBRunner) Run(ctx context.Context, req PhaseBRequest) (PhaseBResult, error) {
	if r.images == nil || r.tts == nil {
		return PhaseBResult{}, fmt.Errorf("phase b runner: missing track dependency: %w", domain.ErrValidation)
	}
	preparedReq, err := r.prepareRequest(ctx, req)
	if err != nil {
		return PhaseBResult{}, err
	}

	res := PhaseBResult{Request: preparedReq}
	startedAt := r.clock.Now()
	var mu sync.Mutex

	var g errgroup.Group
	g.Go(func() error {
		result, trackErr := r.images(ctx, preparedReq)
		if recordErr := r.recordObservation(ctx, preparedReq.RunID, result.Observation); recordErr != nil {
			trackErr = errors.Join(trackErr, fmt.Errorf("record image observation: %w", recordErr))
		}
		mu.Lock()
		res.Image = ImageTrackOutcome{
			Result: result,
			Err:    wrapPhaseBTrackError(domain.StageImage, trackErr),
		}
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		result, trackErr := r.tts(ctx, preparedReq)
		if recordErr := r.recordObservation(ctx, preparedReq.RunID, result.Observation); recordErr != nil {
			trackErr = errors.Join(trackErr, fmt.Errorf("record tts observation: %w", recordErr))
		}
		mu.Lock()
		res.TTS = TTSTrackOutcome{
			Result: result,
			Err:    wrapPhaseBTrackError(domain.StageTTS, trackErr),
		}
		mu.Unlock()
		return nil
	})
	if err := g.Wait(); err != nil {
		return PhaseBResult{}, err
	}

	res.WallClockMs = r.clock.Now().Sub(startedAt).Milliseconds()
	// AC-6: surface Phase B wall-clock as an *additional* summary signal via
	// res.WallClockMs and structured logging. Do NOT fold it through
	// Recorder.Record, whose duration_ms column is an additive SUM of per-track
	// stage observations; mixing wall-clock into that column would corrupt
	// per-stage duration accounting.
	r.logger.Info("phase b wall-clock",
		"run_id", preparedReq.RunID,
		"stage", preparedReq.Stage,
		"wall_clock_ms", res.WallClockMs,
	)

	if err := selectPhaseBError(res); err != nil {
		return res, err
	}
	if r.assemble != nil {
		if err := r.assemble(ctx, res); err != nil {
			return res, fmt.Errorf("phase b runner: assemble: %w", err)
		}
		res.AssemblyDone = true
	}
	return res, nil
}

func (r *PhaseBRunner) prepareRequest(ctx context.Context, req PhaseBRequest) (PhaseBRequest, error) {
	if req.RunID == "" {
		return PhaseBRequest{}, fmt.Errorf("phase b runner: run id required: %w", domain.ErrValidation)
	}
	if req.Stage != domain.StageImage && req.Stage != domain.StageTTS {
		return PhaseBRequest{}, fmt.Errorf("phase b runner: invalid phase b stage %q: %w", req.Stage, domain.ErrValidation)
	}

	// Resolve the operator-edited Vision Descriptor from runs.frozen_descriptor
	// when the caller did not pre-populate the override. This keeps AC-6's
	// "operator edit overrides artifact" invariant local to the Phase B
	// entry point: every image_track invocation picks up the latest column
	// value without the caller needing to remember to thread it through. A
	// nil runLoader preserves the legacy behavior (caller-supplied override
	// or artifact fallback) so existing tests and any non-production call
	// sites are unaffected.
	if req.FrozenDescriptorOverride == nil && r.runLoader != nil {
		run, err := r.runLoader.Get(ctx, req.RunID)
		if err != nil {
			return PhaseBRequest{}, fmt.Errorf("phase b runner: load run for frozen descriptor: %w", err)
		}
		if run != nil && run.FrozenDescriptor != nil && *run.FrozenDescriptor != "" {
			override := *run.FrozenDescriptor
			req.FrozenDescriptorOverride = &override
		}
	}

	if req.Scenario != nil {
		return req, nil
	}
	if req.ScenarioPath == "" {
		return PhaseBRequest{}, fmt.Errorf("phase b runner: scenario path required: %w", domain.ErrValidation)
	}
	raw, err := os.ReadFile(req.ScenarioPath)
	if err != nil {
		return PhaseBRequest{}, fmt.Errorf("phase b runner: read scenario: %w", err)
	}
	var scenario agents.PipelineState
	if err := json.Unmarshal(raw, &scenario); err != nil {
		return PhaseBRequest{}, fmt.Errorf("phase b runner: decode scenario: %w", err)
	}
	req.Scenario = &scenario
	return req, nil
}

func (r *PhaseBRunner) recordObservation(ctx context.Context, runID string, obs domain.StageObservation) error {
	if r.recorder == nil || obs.IsZero() {
		return nil
	}
	return r.recorder.Record(ctx, runID, obs)
}

func wrapPhaseBTrackError(stage domain.Stage, err error) error {
	if err == nil {
		return nil
	}
	return &PhaseBTrackError{Stage: stage, Err: err}
}

func selectPhaseBError(res PhaseBResult) error {
	switch {
	case res.Image.Err != nil && res.TTS.Err != nil:
		return errors.Join(res.Image.Err, res.TTS.Err)
	case res.Image.Err != nil:
		return res.Image.Err
	case res.TTS.Err != nil:
		return res.TTS.Err
	default:
		return nil
	}
}

