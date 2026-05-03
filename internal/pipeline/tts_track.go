package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	ffmpeg "github.com/u2takey/ffmpeg-go"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	llmclient "github.com/sushistack/youtube.pipeline/internal/llmclient"
)

// ttsMaxInputBytes is the per-call UTF-8 byte cap enforced by DashScope qwen3-tts
// ("Range of input length should be [0, 600]"). The limit is bytes, not
// codepoints — Korean glyphs occupy 3 bytes each, so 200 Korean characters
// already crowd the cap. Narration above this size is split at sentence
// boundaries and each chunk synthesised separately; the resulting WAV files
// are concatenated into a single per-scene artifact. We use 560 to leave
// margin for any length the upstream measures slightly differently.
const ttsMaxInputBytes = 560

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
	// BlockedVoiceIDs lists voice identifiers rejected before any API call.
	// When cfg.TTSVoice matches an entry, the track fails with a compliance error
	// and writes a voice_blocked audit entry.
	BlockedVoiceIDs []string
	// AuditLogger is the optional audit logger. When non-nil, audit entries are
	// written for blocked-voice rejections and successful TTS calls. Nil is
	// allowed (no-op guard).
	AuditLogger domain.AuditLogger

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
	legacyScenes := state.Narration.LegacyScenes()
	if len(legacyScenes) == 0 {
		return TTSTrackResult{}, fmt.Errorf("tts track: %w: scenario.json has zero scenes", domain.ErrValidation)
	}

	runDir := filepath.Join(cfg.OutputDir, req.RunID)
	ttsRoot := filepath.Join(runDir, "tts")
	if err := os.MkdirAll(ttsRoot, 0o755); err != nil {
		return TTSTrackResult{}, fmt.Errorf("tts track: prepare tts dir: %w", err)
	}

	// FR47: voice blocklist check — reject before any API call.
	for _, blockedID := range cfg.BlockedVoiceIDs {
		if cfg.TTSVoice == blockedID {
			if cfg.AuditLogger != nil {
				_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
					Timestamp: clk.Now(),
					EventType: domain.AuditEventVoiceBlocked,
					RunID:     req.RunID,
					Stage:     string(domain.StageTTS),
					Provider:  "dashscope",
					Model:     cfg.TTSModel,
					BlockedID: blockedID,
				})
			}
			return TTSTrackResult{}, classifiedMessageError{
				msg:   fmt.Sprintf("Voice profile '%s' is blocked by compliance policy", blockedID),
				cause: domain.ErrValidation,
			}
		}
	}

	result := TTSTrackResult{
		Observation: domain.NewStageObservation(domain.StageTTS),
	}

	seenSceneNum := make(map[int]struct{}, len(legacyScenes))
	start := clk.Now()
	for _, scene := range legacyScenes {
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
		chunks := splitNarrationForTTS(transliterated, ttsMaxInputBytes)

		relPath := filepath.Join("tts", fmt.Sprintf("scene_%02d.%s", scene.SceneNum, format))
		absPath := filepath.Join(runDir, relPath)

		var totalCost float64
		var totalDuration int64
		var providerName, modelName string
		if len(chunks) == 1 {
			resp, err := invokeTTSProvider(ctx, cfg, clk, chunks[0], absPath, format, req.RunID)
			if err != nil {
				return result, fmt.Errorf("tts track: scene %d: %w", scene.SceneNum, err)
			}
			totalCost = resp.CostUSD
			totalDuration = resp.DurationMs
			providerName = resp.Provider
			modelName = resp.Model
		} else {
			chunkPaths := make([]string, 0, len(chunks))
			for i, chunk := range chunks {
				chunkPath := filepath.Join(runDir, "tts",
					fmt.Sprintf("scene_%02d.part_%02d.%s", scene.SceneNum, i+1, format))
				resp, err := invokeTTSProvider(ctx, cfg, clk, chunk, chunkPath, format, req.RunID)
				if err != nil {
					return result, fmt.Errorf("tts track: scene %d chunk %d: %w", scene.SceneNum, i+1, err)
				}
				chunkPaths = append(chunkPaths, chunkPath)
				totalCost += resp.CostUSD
				totalDuration += resp.DurationMs
				providerName = resp.Provider
				modelName = resp.Model
			}
			if err := concatAudioFiles(chunkPaths, absPath); err != nil {
				return result, fmt.Errorf("tts track: scene %d concat: %w", scene.SceneNum, err)
			}
			for _, p := range chunkPaths {
				_ = os.Remove(p)
			}
			logger.Info("tts track chunks concatenated",
				"run_id", req.RunID,
				"scene", scene.SceneNum,
				"chunks", len(chunks),
			)
		}

		result.Observation.CostUSD += totalCost
		result.Artifacts = append(result.Artifacts, absPath)

		// scene_index is 0-based; scene.SceneNum is 1-based (validated above).
		sceneIndex := scene.SceneNum - 1
		if err := cfg.Store.UpsertTTSArtifact(ctx, req.RunID, sceneIndex, relPath, totalDuration); err != nil {
			return result, fmt.Errorf("tts track: persist scene %d: %w", scene.SceneNum, err)
		}

		// Non-fatal audit write after successful TTS synthesis.
		if cfg.AuditLogger != nil {
			if logErr := cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: clk.Now(),
				EventType: domain.AuditEventTTSSynthesis,
				RunID:     req.RunID,
				Stage:     string(domain.StageTTS),
				Provider:  providerName,
				Model:     modelName,
				Prompt:    truncatePrompt(transliterated, 2048),
				CostUSD:   totalCost,
			}); logErr != nil {
				logger.Warn("audit log write failed", "run_id", req.RunID, "error", logErr)
			}
		}

		logger.Info("tts track scene",
			"run_id", req.RunID,
			"scene", scene.SceneNum,
			"tts_path", relPath,
			"duration_ms", totalDuration,
			"cost_usd", totalCost,
			"chunks", len(chunks),
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

// splitNarrationForTTS slices text at sentence boundaries so each chunk fits
// within maxBytes UTF-8 bytes. Boundaries are `.`, `!`, `?`, `…`, `\n`. A
// single sentence longer than maxBytes falls through to a hard codepoint
// split — rare in practice; the alternative would be to silently truncate,
// which corrupts narration. Returns the original text in a single chunk when
// it already fits (the common case). Byte-based — DashScope's TTS limit is
// measured in bytes, and Korean glyphs are 3 bytes each.
func splitNarrationForTTS(text string, maxBytes int) []string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return []string{text}
	}

	sentences := splitIntoSentences(text)
	chunks := make([]string, 0, 4)
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	for _, s := range sentences {
		sBytes := len(s)
		if sBytes > maxBytes {
			flush()
			for _, sub := range hardSplitByBytes(s, maxBytes) {
				chunks = append(chunks, sub)
			}
			continue
		}
		if current.Len()+sBytes > maxBytes {
			flush()
		}
		current.WriteString(s)
	}
	flush()
	return chunks
}

// splitIntoSentences breaks text after `.`, `!`, `?`, `…`, or `\n` while
// preserving the terminator and any trailing whitespace. The returned slice
// elements concatenate back to the original input byte-for-byte so callers
// can rejoin without losing punctuation.
func splitIntoSentences(text string) []string {
	if text == "" {
		return nil
	}
	out := make([]string, 0, 4)
	var cur strings.Builder
	runes := []rune(text)
	for i, r := range runes {
		cur.WriteRune(r)
		isTerminator := r == '.' || r == '!' || r == '?' || r == '…' || r == '\n'
		if !isTerminator {
			continue
		}
		// Consume a single trailing space to keep "Hello. World" cleanly split.
		if i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\t') {
			continue
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// hardSplitByBytes splits s into chunks of at most maxBytes UTF-8 bytes
// while never breaking a multi-byte codepoint mid-sequence. Used as the
// fallback path when a single sentence exceeds the per-call cap.
func hardSplitByBytes(s string, maxBytes int) []string {
	if maxBytes <= 0 {
		return []string{s}
	}
	out := make([]string, 0, len(s)/maxBytes+1)
	var cur strings.Builder
	for _, r := range s {
		rb := utf8.RuneLen(r)
		if rb < 0 {
			rb = 1
		}
		if cur.Len()+rb > maxBytes && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
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
		// Single chunk — rename instead of re-encoding.
		return os.Rename(inputPaths[0], outputPath)
	}
	listFile, err := os.CreateTemp(filepath.Dir(outputPath), ".tts-concat-*.txt")
	if err != nil {
		return fmt.Errorf("concat audio: create list file: %w", err)
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)
	for _, p := range inputPaths {
		// ffmpeg concat demuxer requires absolute paths and `'` escaping.
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
