package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	ffmpeg "github.com/u2takey/ffmpeg-go"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	llmclient "github.com/sushistack/youtube.pipeline/internal/llmclient"
	krtext "github.com/sushistack/youtube.pipeline/internal/text"
)

// DefaultTTSMaxInputBytes is the per-call UTF-8 byte cap enforced by DashScope
// qwen3-tts ("Range of input length should be [0, 600]"). Korean glyphs are
// 3 bytes each, so 200 KR runes already crowd the cap. Merged monologue inputs
// (~5500 runes ≈ 16500 bytes for SCP-049 dogfood) always exceed the cap and
// fall through to KR sentence chunking — ChunkKR returns a single-element
// slice when the input fits the cap, so the same code path serves both the
// "single call" success case and the chunked fallback case (per spec change
// log entry 2: no dead-layer single-call branch).
const DefaultTTSMaxInputBytes = 560

// runAudioFilename is the canonical continuous TTS output for a run. Per-beat
// slices in tts/scene_NN.wav are sample-accurate `-c copy` derivatives of
// this file, so concatenating the slices reproduces this file byte-for-byte
// and Phase C's per-scene assembly never introduces an audible boundary.
const runAudioFilename = "run_audio.wav"

// TTSArtifactStore is the minimal persistence surface the TTS track needs.
// *db.SegmentStore satisfies it structurally.
type TTSArtifactStore interface {
	UpsertTTSArtifact(ctx context.Context, runID string, sceneIndex int, ttsPath string, ttsDurationMs int64) error
}

// TTSAudioOps abstracts the binary-dependent audio post-processing surface
// (ffmpeg concat/slice + ffprobe duration) so unit tests can avoid the
// ffmpeg/ffprobe dependency and assert byte-level invariants directly. The
// production implementation (DefaultTTSAudioOps) wires through to the same
// helpers used by Phase C.
type TTSAudioOps interface {
	Concat(ctx context.Context, inputs []string, output string) error
	Probe(ctx context.Context, path string) (float64, error)
	Slice(ctx context.Context, src, dst string, startSec, endSec float64) error
}

// DefaultTTSAudioOps is the production AudioOps backed by ffmpeg/ffprobe.
// Reuses concatAudioFiles + probeMediaDuration + sliceAudioByTime so the
// shell-out path is shared across stages.
type DefaultTTSAudioOps struct{}

func (DefaultTTSAudioOps) Concat(ctx context.Context, inputs []string, output string) error {
	return concatAudioFiles(inputs, output)
}

func (DefaultTTSAudioOps) Probe(ctx context.Context, path string) (float64, error) {
	return probeMediaDuration(ctx, path)
}

func (DefaultTTSAudioOps) Slice(ctx context.Context, src, dst string, startSec, endSec float64) error {
	return sliceAudioByTime(ctx, src, dst, startSec, endSec)
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
	// MaxInputBytes is the per-synthesizer-call UTF-8 byte cap. Zero falls back
	// to DefaultTTSMaxInputBytes. The merged monologue is split via the KR
	// sentence chunker until each chunk fits this cap.
	MaxInputBytes int
	// BlockedVoiceIDs lists voice identifiers rejected before any API call.
	// When cfg.TTSVoice matches an entry, the track fails with a compliance error
	// and writes a voice_blocked audit entry.
	BlockedVoiceIDs []string
	// AuditLogger is the optional audit logger. When non-nil, audit entries are
	// written for blocked-voice rejections and the run-level TTS synthesis. Nil
	// is allowed (no-op guard).
	AuditLogger domain.AuditLogger

	TTS       domain.TTSSynthesizer
	Store     TTSArtifactStore
	Limiter   Limiter
	Recorder  RetryRecorder
	Clock     clock.Clock
	Logger    *slog.Logger
	AudioOps  TTSAudioOps
}

// RetryRecorder is the minimal retry-observability surface the TTS track
// needs. *Recorder satisfies it structurally.
type RetryRecorder interface {
	RecordRetry(ctx context.Context, runID string, stage domain.Stage, reason string) error
}

type classifiedMessageError struct {
	msg   string
	cause error
}

func (e classifiedMessageError) Error() string {
	return e.msg
}

func (e classifiedMessageError) Unwrap() error {
	return e.cause
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
	if format != "wav" {
		// `-c copy` slicing is only sample-accurate for PCM containers (WAV).
		// Compressed formats (MP3, AAC) snap to packet boundaries when the
		// codec is copied, breaking the "concat of slices == canonical run
		// audio bit-for-bit" invariant. Forbid until a re-encoding slicer
		// lands.
		return nil, fmt.Errorf("tts track: %w: audio_format=%q not supported; D3 slicing requires PCM WAV", domain.ErrValidation, format)
	}
	if cfg.MaxInputBytes <= 0 {
		cfg.MaxInputBytes = DefaultTTSMaxInputBytes
	}
	if cfg.AudioOps == nil {
		cfg.AudioOps = DefaultTTSAudioOps{}
	}

	return func(ctx context.Context, req PhaseBRequest) (TTSTrackResult, error) {
		return runTTSTrack(ctx, cfg, clk, logger, format, req)
	}, nil
}

// beatSpan records a beat's position relative to the merged monologue so we
// can map BeatAnchor rune offsets to time offsets in the canonical run audio.
type beatSpan struct {
	sceneIndex int
	origStart  int
	origEnd    int
	actID      string
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
	if state.Narration == nil || len(state.Narration.Acts) == 0 {
		return TTSTrackResult{}, fmt.Errorf("tts track: %w: scenario.json missing v2 narration acts", domain.ErrValidation)
	}

	runDir := filepath.Join(cfg.OutputDir, req.RunID)
	ttsRoot := filepath.Join(runDir, "tts")
	if err := os.MkdirAll(ttsRoot, 0o755); err != nil {
		return TTSTrackResult{}, fmt.Errorf("tts track: prepare tts dir: %w", err)
	}

	if err := blockedVoiceCheck(ctx, cfg, clk, req.RunID); err != nil {
		return TTSTrackResult{}, err
	}

	mergedOrig, beats, err := mergeMonologueAndBeats(state.Narration.Acts)
	if err != nil {
		return TTSTrackResult{}, err
	}
	if mergedOrig == "" {
		return TTSTrackResult{}, fmt.Errorf("tts track: %w: merged monologue is empty", domain.ErrValidation)
	}

	mergedSynth := Transliterate(mergedOrig)
	if strings.TrimSpace(mergedSynth) == "" {
		return TTSTrackResult{}, fmt.Errorf("tts track: %w: transliterated monologue is empty", domain.ErrValidation)
	}

	chunks := krtext.ChunkKR(mergedSynth, cfg.MaxInputBytes)
	if len(chunks) == 0 {
		return TTSTrackResult{}, fmt.Errorf("tts track: %w: chunker returned no chunks", domain.ErrValidation)
	}

	result := TTSTrackResult{
		Observation: domain.NewStageObservation(domain.StageTTS),
	}

	start := clk.Now()
	canonicalAbs := filepath.Join(ttsRoot, runAudioFilename)

	totalCost, providerName, modelName, chunkPaths, synthErr := synthesizeChunks(
		ctx, cfg, clk, logger, format, ttsRoot, chunks, req.RunID,
	)
	if synthErr != nil {
		cleanupTTSArtifacts(chunkPaths, nil, canonicalAbs)
		return result, synthErr
	}

	if err := cfg.AudioOps.Concat(ctx, chunkPaths, canonicalAbs); err != nil {
		cleanupTTSArtifacts(chunkPaths, nil, canonicalAbs)
		return result, fmt.Errorf("tts track: concat run audio: %w", err)
	}
	for _, p := range chunkPaths {
		_ = os.Remove(p)
	}

	totalDurationSec, err := cfg.AudioOps.Probe(ctx, canonicalAbs)
	if err != nil {
		cleanupTTSArtifacts(nil, nil, canonicalAbs)
		return result, fmt.Errorf("tts track: probe canonical duration: %w", err)
	}
	totalDurationMs := int64(math.Round(totalDurationSec * 1000))

	scenePaths, sceneDurations, err := sliceBeats(ctx, cfg.AudioOps, ttsRoot, format, canonicalAbs, mergedSynth, mergedOrig, beats, totalDurationSec)
	if err != nil {
		cleanupTTSArtifacts(nil, scenePaths, canonicalAbs)
		return result, fmt.Errorf("tts track: slice beats: %w", err)
	}

	for i, span := range beats {
		relPath := filepath.Join("tts", filepath.Base(scenePaths[i]))
		if err := cfg.Store.UpsertTTSArtifact(ctx, req.RunID, span.sceneIndex, relPath, sceneDurations[i]); err != nil {
			cleanupTTSArtifacts(nil, scenePaths, canonicalAbs)
			return result, fmt.Errorf("tts track: persist scene %d: %w", span.sceneIndex, err)
		}
	}

	result.Observation.CostUSD = totalCost
	result.Observation.DurationMs = clk.Now().Sub(start).Milliseconds()
	if result.Observation.DurationMs < 0 {
		result.Observation.DurationMs = 0
	}

	result.Artifacts = append(result.Artifacts, canonicalAbs)
	result.Artifacts = append(result.Artifacts, scenePaths...)

	if cfg.AuditLogger != nil {
		if logErr := cfg.AuditLogger.Log(ctx, domain.AuditEntry{
			Timestamp: clk.Now(),
			EventType: domain.AuditEventTTSSynthesis,
			RunID:     req.RunID,
			Stage:     string(domain.StageTTS),
			Provider:  providerName,
			Model:     modelName,
			Prompt:    truncatePrompt(mergedSynth, 2048),
			CostUSD:   totalCost,
		}); logErr != nil {
			logger.Warn("audit log write failed", "run_id", req.RunID, "error", logErr)
		}
	}

	logger.Info("tts track run audio assembled",
		"run_id", req.RunID,
		"chunks", len(chunks),
		"beats", len(beats),
		"total_duration_ms", totalDurationMs,
		"cost_usd", totalCost,
	)
	return result, nil
}

// mergeMonologueAndBeats concatenates Acts[i].Monologue with single-space
// joiners (per spec Intent: `acts[0].Monologue + " " + acts[1].Monologue + …`)
// and produces the global rune-offset span for every beat across all acts.
// Beats are flattened in (act, beat) order with running 0-based sceneIndex.
//
// Per-act validation rejects: out-of-range, zero-length, non-monotonic, and
// overlapping anchors. The D1 stage-2 segmenter's validator already enforces
// these but TTS does NOT trust upstream — silent clamping in sliceBeats would
// otherwise produce zero-duration scenes that pass file-existence checks but
// break downstream assembly.
func mergeMonologueAndBeats(acts []domain.ActScript) (string, []beatSpan, error) {
	var b strings.Builder
	beats := make([]beatSpan, 0)
	runesSoFar := 0
	for k, act := range acts {
		if strings.TrimSpace(act.Monologue) == "" {
			return "", nil, fmt.Errorf("tts track: %w: act %d (%s) has empty monologue", domain.ErrValidation, k, act.ActID)
		}
		if k > 0 {
			b.WriteRune(' ')
			runesSoFar++
		}
		actRuneCount := utf8.RuneCountInString(act.Monologue)
		var prevEnd int
		for j, anchor := range act.Beats {
			switch {
			case anchor.StartOffset < 0 || anchor.EndOffset > actRuneCount:
				return "", nil, fmt.Errorf("tts track: %w: act %d beat %d offsets out of range [%d,%d) vs runes=%d", domain.ErrValidation, k, j, anchor.StartOffset, anchor.EndOffset, actRuneCount)
			case anchor.EndOffset <= anchor.StartOffset:
				return "", nil, fmt.Errorf("tts track: %w: act %d beat %d has zero or negative length [%d,%d)", domain.ErrValidation, k, j, anchor.StartOffset, anchor.EndOffset)
			case j > 0 && anchor.StartOffset < prevEnd:
				return "", nil, fmt.Errorf("tts track: %w: act %d beat %d overlaps or precedes prior beat (start=%d, prevEnd=%d)", domain.ErrValidation, k, j, anchor.StartOffset, prevEnd)
			}
			beats = append(beats, beatSpan{
				sceneIndex: len(beats),
				origStart:  runesSoFar + anchor.StartOffset,
				origEnd:    runesSoFar + anchor.EndOffset,
				actID:      act.ActID,
			})
			prevEnd = anchor.EndOffset
		}
		b.WriteString(act.Monologue)
		runesSoFar += actRuneCount
	}
	if len(beats) == 0 {
		return "", nil, fmt.Errorf("tts track: %w: narration has zero beats across all acts", domain.ErrValidation)
	}
	return b.String(), beats, nil
}

// synthesizeChunks calls the provider once per chunk, sharing voice + model
// params across calls so seams land on KR sentence boundaries with no
// audible voice change. Returns the temp chunk paths in order; concatAudioFiles
// (`-c copy`) merges them into the canonical run audio without re-encoding
// or inserted silence.
func synthesizeChunks(
	ctx context.Context,
	cfg TTSTrackConfig,
	clk clock.Clock,
	logger *slog.Logger,
	format string,
	ttsRoot string,
	chunks []string,
	runID string,
) (totalCost float64, providerName, modelName string, paths []string, err error) {
	paths = make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		chunkPath := filepath.Join(ttsRoot, fmt.Sprintf(".chunk_%03d.%s", i+1, format))
		resp, callErr := invokeTTSProvider(ctx, cfg, clk, chunk, chunkPath, format, runID)
		if callErr != nil {
			return 0, "", "", paths, fmt.Errorf("tts track: chunk %d/%d: %w", i+1, len(chunks), callErr)
		}
		paths = append(paths, chunkPath)
		totalCost += resp.CostUSD
		providerName = resp.Provider
		modelName = resp.Model
	}
	logger.Debug("tts track chunks synthesized", "run_id", runID, "chunks", len(chunks))
	return totalCost, providerName, modelName, paths, nil
}

// sliceBeats writes per-beat sample-accurate slices of canonicalAbs into
// ttsRoot/scene_NN.wav. Slice boundaries are contiguous so concatenating
// every slice in scene order reproduces canonicalAbs byte-for-byte:
//
//   - boundary[0] = 0
//   - boundary[i] (0<i<len(beats)) = totalDur * origStart_i / origRunes
//   - boundary[len(beats)] = totalDur
//   - slice_i = [boundary[i], boundary[i+1])
//
// Joiner runes (the single space inserted between adjacent acts) and any
// uncovered tail of an act monologue are absorbed into the slice that ends
// at the next beat's start — they belong somewhere, and merging them into
// the trailing slice avoids fabricating extra "joiner" scenes.
//
// Mapping is rune-proportional against `origRunes` (the merged-monologue
// rune count BEFORE transliteration); see spec change log entry 4 for the
// approximation tradeoff. The synth/orig scaling cancels because total audio
// duration corresponds to the entire synthesized text, so per-rune time =
// totalDur / origRunes regardless of synth-vs-orig length difference.
func sliceBeats(
	ctx context.Context,
	audio TTSAudioOps,
	ttsRoot, format, canonicalAbs string,
	mergedSynth, mergedOrig string,
	beats []beatSpan,
	totalDurationSec float64,
) (paths []string, durations []int64, err error) {
	origRunes := utf8.RuneCountInString(mergedOrig)
	synthRunes := utf8.RuneCountInString(mergedSynth)
	if origRunes == 0 || synthRunes == 0 {
		return nil, nil, fmt.Errorf("%w: merged monologue rune count is zero (orig=%d synth=%d)", domain.ErrValidation, origRunes, synthRunes)
	}

	boundaries := make([]float64, len(beats)+1)
	boundaries[0] = 0
	boundaries[len(beats)] = totalDurationSec
	for j := 1; j < len(beats); j++ {
		boundaries[j] = totalDurationSec * float64(beats[j].origStart) / float64(origRunes)
		if boundaries[j] < boundaries[j-1] {
			boundaries[j] = boundaries[j-1]
		}
		if boundaries[j] > totalDurationSec {
			boundaries[j] = totalDurationSec
		}
	}

	paths = make([]string, len(beats))
	durations = make([]int64, len(beats))
	for i, span := range beats {
		startSec := boundaries[i]
		endSec := boundaries[i+1]
		if endSec < startSec {
			endSec = startSec
		}
		out := filepath.Join(ttsRoot, fmt.Sprintf("scene_%02d.%s", span.sceneIndex+1, format))
		if err := audio.Slice(ctx, canonicalAbs, out, startSec, endSec); err != nil {
			return paths, durations, fmt.Errorf("scene %d: %w", span.sceneIndex+1, err)
		}
		paths[i] = out
		durations[i] = int64(math.Round((endSec - startSec) * 1000))
	}
	return paths, durations, nil
}

// sliceAudioByTime extracts [startSec, endSec) from src into dst using ffmpeg
// `-c copy`. WAV PCM has constant bytes-per-second so `-c copy` produces a
// sample-accurate slice — no re-encoding, no silence padding. The output is
// stat-checked: ffmpeg can silently emit a 0-byte file when startSec >= endSec,
// and a 0-byte WAV would pass file-existence checks but break Phase C.
func sliceAudioByTime(ctx context.Context, src, dst string, startSec, endSec float64) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("source missing: %w", err)
	}
	if endSec <= startSec {
		return fmt.Errorf("%w: invalid time slice [%.6fs, %.6fs)", domain.ErrValidation, startSec, endSec)
	}
	args := ffmpeg.KwArgs{
		"ss": fmt.Sprintf("%.6f", startSec),
		"to": fmt.Sprintf("%.6f", endSec),
	}
	err := ffmpeg.Input(src).
		Output(dst, ffmpeg.MergeKwArgs([]ffmpeg.KwArgs{args, {"c": "copy"}})).
		OverWriteOutput().
		Silent(true).
		Run()
	if err != nil {
		return fmt.Errorf("ffmpeg slice: %w", err)
	}
	st, statErr := os.Stat(dst)
	if statErr != nil {
		return fmt.Errorf("post-slice stat: %w", statErr)
	}
	if st.Size() == 0 {
		return fmt.Errorf("%w: ffmpeg produced empty slice for [%.6fs, %.6fs)", domain.ErrStageFailed, startSec, endSec)
	}
	return nil
}


// blockedVoiceCheck enforces the FR47 voice blocklist before any API call.
func blockedVoiceCheck(ctx context.Context, cfg TTSTrackConfig, clk clock.Clock, runID string) error {
	for _, blockedID := range cfg.BlockedVoiceIDs {
		if cfg.TTSVoice == blockedID {
			if cfg.AuditLogger != nil {
				_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
					Timestamp: clk.Now(),
					EventType: domain.AuditEventVoiceBlocked,
					RunID:     runID,
					Stage:     string(domain.StageTTS),
					Provider:  "dashscope",
					Model:     cfg.TTSModel,
					BlockedID: blockedID,
				})
			}
			return classifiedMessageError{
				msg:   fmt.Sprintf("Voice profile '%s' is blocked by compliance policy", blockedID),
				cause: domain.ErrValidation,
			}
		}
	}
	return nil
}

// cleanupTTSArtifacts removes all partial files so a failed TTS stage cannot
// leave half-synthesized audio on disk. Called from atomic-failure branches.
func cleanupTTSArtifacts(chunkPaths, scenePaths []string, canonicalAbs string) {
	for _, p := range chunkPaths {
		if p != "" {
			_ = os.Remove(p)
		}
	}
	for _, p := range scenePaths {
		if p != "" {
			_ = os.Remove(p)
		}
	}
	if canonicalAbs != "" {
		_ = os.Remove(canonicalAbs)
	}
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

// concatAudioFiles joins multiple audio files into a single output via the
// ffmpeg concat demuxer. All inputs must share the same codec/sample rate;
// DashScope qwen3-tts always returns the requested format with consistent
// parameters within a run, so `-c copy` is safe.
func concatAudioFiles(inputPaths []string, outputPath string) error {
	if len(inputPaths) == 0 {
		return fmt.Errorf("concat audio: no inputs")
	}
	if len(inputPaths) == 1 {
		return os.Rename(inputPaths[0], outputPath)
	}
	listFile, err := os.CreateTemp(filepath.Dir(outputPath), ".tts-concat-*.txt")
	if err != nil {
		return fmt.Errorf("concat audio: create list file: %w", err)
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)
	for _, p := range inputPaths {
		abs, err := filepath.Abs(p)
		if err != nil {
			_ = listFile.Close()
			return fmt.Errorf("concat audio: resolve %s: %w", p, err)
		}
		escaped := strings.ReplaceAll(abs, "'", `'\''`)
		if _, err := fmt.Fprintf(listFile, "file '%s'\n", escaped); err != nil {
			_ = listFile.Close()
			return fmt.Errorf("concat audio: write list: %w", err)
		}
	}
	if err := listFile.Close(); err != nil {
		return fmt.Errorf("concat audio: close list: %w", err)
	}
	err = ffmpeg.Input(listPath, ffmpeg.KwArgs{"f": "concat", "safe": "0"}).
		Output(outputPath, ffmpeg.KwArgs{"c": "copy"}).
		OverWriteOutput().
		Silent(true).
		Run()
	if err != nil {
		return fmt.Errorf("concat audio: ffmpeg: %w", err)
	}
	return nil
}
