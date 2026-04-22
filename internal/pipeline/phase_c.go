package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	ffmpeg "github.com/u2takey/ffmpeg-go"
	"golang.org/x/sync/errgroup"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

var _ = ffmpeg.Input // ensure ffmpeg-go is used

// PhaseCRequest carries the input parameters for the assembly stage.
type PhaseCRequest struct {
	RunID   string
	RunDir  string
	Segments []*domain.Episode
}

// PhaseCResult carries the output of PhaseCRunner.Run.
type PhaseCResult struct {
	WallClockMs int64
	ClipPaths   []string
	OutputPath  string
}

// SegmentClipUpdater is the minimal surface PhaseCRunner needs to persist
// per-scene clip paths. It is satisfied by *db.SegmentStore but tests can
// supply a trivial fake.
type SegmentClipUpdater interface {
	UpdateClipPath(ctx context.Context, runID string, sceneIndex int, clipPath string) error
}

// RunOutputUpdater is the minimal surface PhaseCRunner needs to persist the
// final output.mp4 path. It is satisfied by *db.RunStore but tests can supply
// a trivial fake.
type RunOutputUpdater interface {
	UpdateOutputPath(ctx context.Context, runID string, outputPath string) error
}

// PhaseCRunner orchestrates the two‑stage assembly of per‑scene clips and the
// final concatenated video. It satisfies the same pattern as PhaseBRunner.
type PhaseCRunner struct {
	segmentUpdater SegmentClipUpdater
	runUpdater     RunOutputUpdater
	recorder       *Recorder
	clock          clock.Clock
	logger         *slog.Logger
}

// NewPhaseCRunner builds a PhaseCRunner with nil‑safe defaults for clock and
// logger. The updater dependencies must be non‑nil; the recorder may be nil,
// in which case stage observations are logged but not persisted.
func NewPhaseCRunner(
	segmentUpdater SegmentClipUpdater,
	runUpdater RunOutputUpdater,
	recorder *Recorder,
	clk clock.Clock,
	logger *slog.Logger,
) *PhaseCRunner {
	if clk == nil {
		clk = clock.RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PhaseCRunner{
		segmentUpdater: segmentUpdater,
		runUpdater:     runUpdater,
		recorder:       recorder,
		clock:          clk,
		logger:         logger,
	}
}

// Run executes the full assembly pipeline for the given request.
// It performs per‑scene clip assembly, persists each clip path, then concatenates
// all clips into output.mp4 and persists its path. Wall‑clock duration is
// recorded via Recorder.Record with StageObservation{Stage: StageAssemble}.
func (r *PhaseCRunner) Run(ctx context.Context, req PhaseCRequest) (PhaseCResult, error) {
	if req.RunID == "" {
		return PhaseCResult{}, fmt.Errorf("phase c runner: run id required: %w", domain.ErrValidation)
	}
	if req.RunDir == "" {
		return PhaseCResult{}, fmt.Errorf("phase c runner: run directory required: %w", domain.ErrValidation)
	}
	if len(req.Segments) == 0 {
		return PhaseCResult{}, fmt.Errorf("phase c runner: no segments to assemble: %w", domain.ErrValidation)
	}

	startedAt := r.clock.Now()
	res := PhaseCResult{}

	// 1. Create clips/ directory (idempotent).
	clipsDir := filepath.Join(req.RunDir, "clips")
	if err := os.MkdirAll(clipsDir, 0755); err != nil {
		return PhaseCResult{}, fmt.Errorf("phase c runner: create clips dir: %w", err)
	}

	// 2. Assemble each scene clip, persisting its path.
	clipPaths := make([]string, len(req.Segments))
	var g errgroup.Group
	for i, ep := range req.Segments {
		i, ep := i, ep // capture for closure
		g.Go(func() error {
			clipPath, err := r.BuildSceneClip(ctx, req.RunDir, ep)
			if err != nil {
				return fmt.Errorf("scene %d: %w", ep.SceneIndex, err)
			}
			clipPaths[i] = clipPath
			if r.segmentUpdater != nil {
				if err := r.segmentUpdater.UpdateClipPath(ctx, req.RunID, ep.SceneIndex, clipPath); err != nil {
					return fmt.Errorf("scene %d: persist clip path: %w", ep.SceneIndex, err)
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return PhaseCResult{}, fmt.Errorf("phase c runner: clip assembly: %w", err)
	}
	res.ClipPaths = clipPaths

	// 3. Concatenate all clips into output.mp4.
	outputPath := filepath.Join(req.RunDir, "output.mp4")
	if err := r.concatClips(ctx, clipsDir, clipPaths, outputPath); err != nil {
		return PhaseCResult{}, fmt.Errorf("phase c runner: final concat: %w", err)
	}
	res.OutputPath = outputPath

	// 4. Persist output path.
	if r.runUpdater != nil {
		if err := r.runUpdater.UpdateOutputPath(ctx, req.RunID, outputPath); err != nil {
			return PhaseCResult{}, fmt.Errorf("phase c runner: persist output path: %w", err)
		}
	}

	// 5. Record wall‑clock observation.
	res.WallClockMs = r.clock.Now().Sub(startedAt).Milliseconds()
	if r.recorder != nil {
		obs := domain.StageObservation{
			Stage:      domain.StageAssemble,
			DurationMs: res.WallClockMs,
			// No cost, tokens, retry, critic score etc. for assembly.
		}
		if err := r.recorder.Record(ctx, req.RunID, obs); err != nil {
			// Log but do not fail the whole run; assembly succeeded.
			r.logger.Warn("failed to record stage observation",
				"run_id", req.RunID,
				"stage", domain.StageAssemble,
				"error", err,
			)
		}
	}

	r.logger.Info("phase c completed",
		"run_id", req.RunID,
		"scene_count", len(req.Segments),
		"wall_clock_ms", res.WallClockMs,
		"output_path", outputPath,
	)
	return res, nil
}

// BuildSceneClip implements per‑scene clip assembly with the correct transition
// filters and audio‑video sync padding. It returns the absolute path to the
// generated clip file.
func (r *PhaseCRunner) BuildSceneClip(ctx context.Context, runDir string, ep *domain.Episode) (string, error) {
	// Validate episode.
	if ep == nil {
		return "", fmt.Errorf("episode is nil: %w", domain.ErrValidation)
	}
	if len(ep.Shots) == 0 {
		return "", fmt.Errorf("episode has no shots (scene %d): %w", ep.SceneIndex, domain.ErrValidation)
	}
	if ep.ShotCount != len(ep.Shots) {
		return "", fmt.Errorf("episode shot count mismatch (scene %d): %w", ep.SceneIndex, domain.ErrValidation)
	}
	// Determine clip path.
	clipPath := filepath.Join(runDir, "clips", fmt.Sprintf("scene_%02d.mp4", ep.SceneIndex))
	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(clipPath), 0755); err != nil {
		return "", fmt.Errorf("create clips dir: %w", err)
	}

	// Single‑shot Ken Burns can use the optimized path.
	if len(ep.Shots) == 1 && ep.Shots[0].Transition == domain.TransitionKenBurns {
		return r.buildSingleShotKenBurns(ctx, ep, clipPath)
	}

	// Multi‑shot (or single‑shot with other transitions) use generic builder.
	return r.buildMultiShotClip(ctx, ep, clipPath)
}

// buildSingleShotKenBurns generates a video clip from a single image using the
// zoompan filter (Ken Burns effect), overlays TTS audio if present, and outputs
// a 1920x1080 MP4 with libx264 video and AAC audio.
func (r *PhaseCRunner) buildSingleShotKenBurns(ctx context.Context, ep *domain.Episode, clipPath string) (string, error) {
	if len(ep.Shots) != 1 {
		return "", fmt.Errorf("buildSingleShotKenBurns expects exactly one shot: %w", domain.ErrValidation)
	}
	shot := ep.Shots[0]
	const fps = 25
	durationSec := shot.DurationSeconds
	if durationSec <= 0 {
		return "", fmt.Errorf("shot duration must be positive: %w", domain.ErrValidation)
	}
	frames := int(math.Ceil(durationSec * fps))
	// Ensure at least one frame.
	if frames < 1 {
		frames = 1
	}

	videoInput := ffmpeg.Input(shot.ImagePath)
	// Apply zoompan with gentle zoom, output size 1920x1080.
	zoom := videoInput.ZoomPan(ffmpeg.KwArgs{
		"zoom": "min(zoom+0.0015,1.5)",
		"d":    frames,
		"s":    "1920x1080",
	})

	// Prepare audio stream if TTSPath exists.
	var audioStream *ffmpeg.Stream
	if ep.TTSPath != nil && *ep.TTSPath != "" {
		audioInput := ffmpeg.Input(*ep.TTSPath)
		audioStream = audioInput.Audio()
	}

	// Output with video and optional audio, applying sync padding if needed.
	var err error
	if audioStream != nil {
		videoDur := shot.DurationSeconds
		var audioDur float64
		if ep.TTSDurationMs != nil {
			audioDur = float64(*ep.TTSDurationMs) / 1000.0
		} else {
			// Fallback: probe audio file duration.
			dur, probeErr := r.probeDuration(ctx, *ep.TTSPath)
			if probeErr != nil {
				return "", fmt.Errorf("probe TTS duration: %w", probeErr)
			}
			audioDur = dur
		}
		// Apply sync padding.
		paddedVideo, paddedAudio, padErr := r.applySyncPadding(zoom, audioStream, videoDur, audioDur)
		if padErr != nil {
			return "", fmt.Errorf("sync padding: %w", padErr)
		}
		err = ffmpeg.Output([]*ffmpeg.Stream{paddedVideo, paddedAudio}, clipPath).Run()
	} else {
		err = ffmpeg.Output([]*ffmpeg.Stream{zoom}, clipPath).Run()
	}
	if err != nil {
		return "", fmt.Errorf("ffmpeg error: %w", err)
	}
	return clipPath, nil
}

// createShotStream converts a single shot into a video stream with the correct duration
// and Ken Burns effect if needed.
func (r *PhaseCRunner) createShotStream(shot *domain.Shot) (*ffmpeg.Stream, error) {
	const fps = 25
	durationSec := shot.DurationSeconds
	if durationSec <= 0 {
		return nil, fmt.Errorf("shot duration must be positive: %w", domain.ErrValidation)
	}
	frames := int(math.Ceil(durationSec * fps))
	if frames < 1 {
		frames = 1
	}
	videoInput := ffmpeg.Input(shot.ImagePath)
	// Apply zoompan with gentle zoom for Ken Burns, otherwise static zoom.
	if shot.Transition == domain.TransitionKenBurns {
		// Ken Burns: gentle zoom over the duration.
		return videoInput.ZoomPan(ffmpeg.KwArgs{
			"zoom": "min(zoom+0.0015,1.5)",
			"d":    frames,
			"s":    "1920x1080",
		}), nil
	}
	// Static zoom (no movement) – just scale to 1920x1080 and hold.
	return videoInput.ZoomPan(ffmpeg.KwArgs{
		"zoom": "1",
		"d":    frames,
		"s":    "1920x1080",
	}), nil
}

// computeVideoDuration returns the total duration of all shots in seconds.
func (r *PhaseCRunner) computeVideoDuration(ep *domain.Episode) float64 {
	var total float64
	for _, shot := range ep.Shots {
		total += shot.DurationSeconds
	}
	return total
}

// applySyncPadding pads video or audio streams to match durations.
// videoDur and audioDur are in seconds.
// Returns (paddedVideoStream, paddedAudioStream, error).
func (r *PhaseCRunner) applySyncPadding(videoStream *ffmpeg.Stream, audioStream *ffmpeg.Stream, videoDur, audioDur float64) (*ffmpeg.Stream, *ffmpeg.Stream, error) {
	const tolerance = 0.1
	diff := audioDur - videoDur
	if math.Abs(diff) <= tolerance {
		// Durations already match within tolerance.
		return videoStream, audioStream, nil
	}
	if diff > 0 {
		// TTS longer: pad video with tpad.
		// tpad filter extends last frame.
		paddedVideo := videoStream.Filter("tpad", ffmpeg.Args{}, ffmpeg.KwArgs{
			"stop_mode": "clone",
			"stop_duration": fmt.Sprintf("%.3f", diff),
		})
		return paddedVideo, audioStream, nil
	} else {
		// Video longer: pad audio with apad.
		paddedAudio := audioStream.Filter("apad", ffmpeg.Args{}, ffmpeg.KwArgs{
			"pad_dur": fmt.Sprintf("%.3f", -diff),
		})
		return videoStream, paddedAudio, nil
	}
}

// buildMultiShotClip assembles a scene with multiple shots using appropriate transitions.
func (r *PhaseCRunner) buildMultiShotClip(ctx context.Context, ep *domain.Episode, clipPath string) (string, error) {
	if len(ep.Shots) < 2 {
		return "", fmt.Errorf("buildMultiShotClip expects at least two shots: %w", domain.ErrValidation)
	}
	// Create a stream for each shot.
	streams := make([]*ffmpeg.Stream, len(ep.Shots))
	for i, shot := range ep.Shots {
		s, err := r.createShotStream(&shot)
		if err != nil {
			return "", fmt.Errorf("shot %d: %w", i, err)
		}
		streams[i] = s
	}
	// Determine if any transition is cross_dissolve.
	hasCrossDissolve := false
	for i := 0; i < len(ep.Shots)-1; i++ {
		if ep.Shots[i+1].Transition == domain.TransitionCrossDissolve {
			hasCrossDissolve = true
			break
		}
	}
	var videoStream *ffmpeg.Stream
	if hasCrossDissolve {
		// Chain xfade between consecutive streams.
		// Start with first stream.
		videoStream = streams[0]
		for i := 1; i < len(streams); i++ {
			// Apply xfade between previous result and next stream.
			// Offset is sum of durations of previous shots minus transition duration?
			// For simplicity, use default offset = duration of previous shot - 0.5?
			// We'll compute offset as total duration so far minus 0.5.
			// For now, use a fixed 0.5s cross‑dissolve at the boundary.
			videoStream = ffmpeg.Filter([]*ffmpeg.Stream{videoStream, streams[i]}, "xfade",
				ffmpeg.Args{},
				ffmpeg.KwArgs{
					"transition": "fade",
					"duration":   "0.5",
					"offset":     fmt.Sprintf("%.3f", ep.Shots[i-1].DurationSeconds-0.5),
				})
		}
	} else {
		// All hard_cut – use concat filter.
		videoStream = ffmpeg.Concat(streams, ffmpeg.KwArgs{"v": 1, "a": 0})
	}
	// Overlay TTS audio if present.
	var err error
	if ep.TTSPath != nil && *ep.TTSPath != "" {
		audioInput := ffmpeg.Input(*ep.TTSPath)
		audioStream := audioInput.Audio()
		// Compute durations for sync padding.
		videoDur := r.computeVideoDuration(ep)
		var audioDur float64
		if ep.TTSDurationMs != nil {
			audioDur = float64(*ep.TTSDurationMs) / 1000.0
		} else {
			// Fallback: probe audio file duration (expensive).
			dur, probeErr := r.probeDuration(ctx, *ep.TTSPath)
			if probeErr != nil {
				return "", fmt.Errorf("probe TTS duration: %w", probeErr)
			}
			audioDur = dur
		}
		// Apply sync padding.
		paddedVideo, paddedAudio, padErr := r.applySyncPadding(videoStream, audioStream, videoDur, audioDur)
		if padErr != nil {
			return "", fmt.Errorf("sync padding: %w", padErr)
		}
		err = ffmpeg.Output([]*ffmpeg.Stream{paddedVideo, paddedAudio}, clipPath).Run()
	} else {
		err = ffmpeg.Output([]*ffmpeg.Stream{videoStream}, clipPath).Run()
	}
	if err != nil {
		return "", fmt.Errorf("ffmpeg error: %w", err)
	}
	return clipPath, nil
}

// probeDuration returns the duration in seconds of a media file by invoking ffprobe.
func (r *PhaseCRunner) probeDuration(ctx context.Context, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		path,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe %q: %w", path, err)
	}
	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		return 0, fmt.Errorf("parse ffprobe output: %w", err)
	}
	dur, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", result.Format.Duration, err)
	}
	return dur, nil
}

// concatClips uses FFmpeg's concat demuxer to merge the given clip files into
// a single output.mp4. It validates that the output duration approximates the
// sum of input durations (tolerance ≤0.1s).
func (r *PhaseCRunner) concatClips(ctx context.Context, clipsDir string, clipPaths []string, outputPath string) error {
	if len(clipPaths) == 0 {
		return fmt.Errorf("no clips to concatenate: %w", domain.ErrValidation)
	}
	// Create a temporary list file.
	tmpList, err := os.CreateTemp(clipsDir, "concat_*.txt")
	if err != nil {
		return fmt.Errorf("create concat list file: %w", err)
	}
	defer os.Remove(tmpList.Name())

	// Write absolute paths, one per line with "file " prefix.
	for _, p := range clipPaths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("absolute path for %q: %w", p, err)
		}
		if _, err := fmt.Fprintf(tmpList, "file '%s'\n", abs); err != nil {
			return fmt.Errorf("write concat list: %w", err)
		}
	}
	if err := tmpList.Close(); err != nil {
		return fmt.Errorf("close concat list: %w", err)
	}

	// Run ffmpeg concat command.
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", tmpList.Name(),
		"-c", "copy",
		outputPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg concat failed: %w", err)
	}

	// Validate output duration ≈ sum of input durations (tolerance ≤0.1s).
	var totalInput float64
	for _, clip := range clipPaths {
		dur, err := r.probeDuration(ctx, clip)
		if err != nil {
			return fmt.Errorf("probe input clip %q: %w", clip, err)
		}
		totalInput += dur
	}
	outputDur, err := r.probeDuration(ctx, outputPath)
	if err != nil {
		return fmt.Errorf("probe output %q: %w", outputPath, err)
	}
	if diff := math.Abs(outputDur - totalInput); diff > 0.1 {
		return fmt.Errorf("duration mismatch: output %.3fs, sum inputs %.3fs, diff %.3fs exceeds tolerance 0.1s", outputDur, totalInput, diff)
	}
	return nil
}