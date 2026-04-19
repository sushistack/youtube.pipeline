package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	llmclient "github.com/sushistack/youtube.pipeline/internal/llmclient"
)

// TTSArtifactStore is the minimal persistence surface the TTS track needs.
// *db.SegmentStore satisfies it structurally.
type TTSArtifactStore interface {
	UpsertTTSArtifact(ctx context.Context, runID string, sceneIndex int, ttsPath string, ttsDurationMs int64) error
}

// TTSTrackConfig bundles the dependencies required to build a TTSTrack
// function. Provider/model/voice identifiers are config-driven per NFR-M1
// so business logic does not hard-code DashScope model or voice literals.
type TTSTrackConfig struct {
	// OutputDir is the base output directory; per-run directories are joined
	// by run ID under this root.
	OutputDir string
	// TTSModel is the model identifier passed to TTSSynthesizer (from cfg.TTSModel).
	TTSModel string
	// TTSVoice is the voice identifier passed to TTSSynthesizer.
	TTSVoice string
	// AudioFormat is the output file extension without the dot (e.g. "wav", "mp3").
	// Defaults to "wav" when empty.
	AudioFormat string
	// MaxRetries is the maximum number of retry attempts for retryable provider errors.
	MaxRetries int

	TTS      domain.TTSSynthesizer
	Store    TTSArtifactStore
	Limiter  Limiter
	Recorder RetryRecorder
	Clock    clock.Clock
	Logger   *slog.Logger
}

// RetryRecorder is the minimal retry-observability surface the TTS track
// needs. *Recorder satisfies it structurally.
type RetryRecorder interface {
	RecordRetry(ctx context.Context, runID string, stage domain.Stage, reason string) error
}

// NewTTSTrack constructs the Phase B TTS track from cfg. The returned function
// is safe to pass as pipeline.TTSTrack to NewPhaseBRunner.
func NewTTSTrack(cfg TTSTrackConfig) (TTSTrack, error) {
	if cfg.OutputDir == "" {
		return nil, fmt.Errorf("tts track: %w: output dir is empty", domain.ErrValidation)
	}
	if cfg.TTSModel == "" {
		return nil, fmt.Errorf("tts track: %w: tts model is empty", domain.ErrValidation)
	}
	if cfg.TTSVoice == "" {
		return nil, fmt.Errorf("tts track: %w: tts voice is empty", domain.ErrValidation)
	}
	if cfg.TTS == nil {
		return nil, fmt.Errorf("tts track: %w: tts synthesizer is nil", domain.ErrValidation)
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("tts track: %w: tts store is nil", domain.ErrValidation)
	}
	if cfg.Limiter == nil {
		return nil, fmt.Errorf("tts track: %w: limiter is nil", domain.ErrValidation)
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	format := cfg.AudioFormat
	if format == "" {
		format = "wav"
	}

	return func(ctx context.Context, req PhaseBRequest) (TTSTrackResult, error) {
		return runTTSTrack(ctx, cfg, clk, logger, format, req)
	}, nil
}

func runTTSTrack(
	ctx context.Context,
	cfg TTSTrackConfig,
	clk clock.Clock,
	logger *slog.Logger,
	format string,
	req PhaseBRequest,
) (TTSTrackResult, error) {
	state, err := loadScenarioState(req)
	if err != nil {
		return TTSTrackResult{}, err
	}
	if state.Narration == nil {
		return TTSTrackResult{}, fmt.Errorf("tts track: %w: scenario.json missing narration", domain.ErrValidation)
	}
	if len(state.Narration.Scenes) == 0 {
		return TTSTrackResult{}, fmt.Errorf("tts track: %w: scenario.json has zero scenes", domain.ErrValidation)
	}

	runDir := filepath.Join(cfg.OutputDir, req.RunID)
	ttsRoot := filepath.Join(runDir, "tts")
	if err := os.MkdirAll(ttsRoot, 0o755); err != nil {
		return TTSTrackResult{}, fmt.Errorf("tts track: prepare tts dir: %w", err)
	}

	result := TTSTrackResult{
		Observation: domain.NewStageObservation(domain.StageTTS),
	}

	seenSceneNum := make(map[int]struct{}, len(state.Narration.Scenes))
	start := clk.Now()
	for _, scene := range state.Narration.Scenes {
		if scene.SceneNum <= 0 {
			return result, fmt.Errorf("tts track: %w: scene_num must be >= 1, got %d", domain.ErrValidation, scene.SceneNum)
		}
		if _, dup := seenSceneNum[scene.SceneNum]; dup {
			return result, fmt.Errorf("tts track: %w: duplicate scene_num %d", domain.ErrValidation, scene.SceneNum)
		}
		seenSceneNum[scene.SceneNum] = struct{}{}

		narration := scene.Narration
		if strings.TrimSpace(narration) == "" {
			return result, fmt.Errorf("tts track: %w: scene %d has empty narration", domain.ErrValidation, scene.SceneNum)
		}

		transliterated := Transliterate(narration)

		relPath := filepath.Join("tts", fmt.Sprintf("scene_%02d.%s", scene.SceneNum, format))
		absPath := filepath.Join(runDir, relPath)

		resp, err := invokeTTSProvider(ctx, cfg, clk, transliterated, absPath, format, req.RunID)
		if err != nil {
			return result, fmt.Errorf("tts track: scene %d: %w", scene.SceneNum, err)
		}

		result.Observation.CostUSD += resp.CostUSD
		result.Artifacts = append(result.Artifacts, absPath)

		// scene_index is 0-based; scene.SceneNum is 1-based (validated above).
		sceneIndex := scene.SceneNum - 1
		if err := cfg.Store.UpsertTTSArtifact(ctx, req.RunID, sceneIndex, relPath, resp.DurationMs); err != nil {
			return result, fmt.Errorf("tts track: persist scene %d: %w", scene.SceneNum, err)
		}

		logger.Info("tts track scene",
			"run_id", req.RunID,
			"scene", scene.SceneNum,
			"tts_path", relPath,
			"duration_ms", resp.DurationMs,
			"cost_usd", resp.CostUSD,
		)
	}

	result.Observation.DurationMs = clk.Now().Sub(start).Milliseconds()
	if result.Observation.DurationMs < 0 {
		result.Observation.DurationMs = 0
	}
	return result, nil
}

func invokeTTSProvider(
	ctx context.Context,
	cfg TTSTrackConfig,
	clk clock.Clock,
	text string,
	outputPath string,
	format string,
	runID string,
) (domain.TTSResponse, error) {
	var resp domain.TTSResponse
	call := func() error {
		out, err := cfg.TTS.Synthesize(ctx, domain.TTSRequest{
			Text:       text,
			Model:      cfg.TTSModel,
			Voice:      cfg.TTSVoice,
			OutputPath: outputPath,
			Format:     format,
		})
		if err != nil {
			return err
		}
		resp = out
		return nil
	}

	limiterCall := func(inner context.Context) error {
		onRetry := func(attempt int, reason string) {
			if cfg.Recorder != nil {
				// Non-fatal: retry observability must not abort synthesis.
				_ = cfg.Recorder.RecordRetry(inner, runID, domain.StageTTS, reason)
			}
		}
		return llmclient.WithRetry(inner, clk, cfg.MaxRetries, call, onRetry)
	}

	if err := cfg.Limiter.Do(ctx, limiterCall); err != nil {
		return domain.TTSResponse{}, err
	}
	return resp, nil
}
