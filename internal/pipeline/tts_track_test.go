package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// ── fake helpers ──────────────────────────────────────────────────────────────

// fakeTTSSynthesizer writes a deterministic byte stream proportional to the
// input rune count so the test's fake AudioOps can derive predictable
// durations from byte counts. The bytes are not a real WAV — the unit-test
// AudioOps treats them as opaque.
type fakeTTSSynthesizer struct {
	mu           sync.Mutex
	receivedReqs []domain.TTSRequest
	bytesPerRune int
	err          error
	errAfter     int // return err on the Nth call (1-indexed); 0 = never
	callCount    int
}

func newFakeTTS() *fakeTTSSynthesizer {
	return &fakeTTSSynthesizer{bytesPerRune: 4}
}

func (f *fakeTTSSynthesizer) Synthesize(ctx context.Context, req domain.TTSRequest) (domain.TTSResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.err != nil && (f.errAfter == 0 || f.callCount == f.errAfter) {
		return domain.TTSResponse{}, f.err
	}
	f.receivedReqs = append(f.receivedReqs, req)
	if req.OutputPath != "" {
		runeCount := len([]rune(req.Text))
		if runeCount == 0 {
			runeCount = 1
		}
		buf := make([]byte, runeCount*f.bytesPerRune)
		for i := range buf {
			buf[i] = byte(i & 0xFF)
		}
		if err := os.WriteFile(req.OutputPath, buf, 0o644); err != nil {
			return domain.TTSResponse{}, err
		}
	}
	return domain.TTSResponse{
		AudioPath:  req.OutputPath,
		DurationMs: 1200,
		Model:      req.Model,
		Provider:   "dashscope",
		CostUSD:    0.001,
	}, nil
}

// fakeAudioOps simulates ffmpeg/ffprobe over raw byte streams. Concat appends
// chunks; Probe returns duration = bytes / bytesPerSec; Slice copies the
// time-window sub-slice. This contract preserves the real-world invariant
// "concat of slices == canonical run audio bytes" so byte equality assertions
// stand in for the sample-accuracy unit assertion.
type fakeAudioOps struct {
	mu          sync.Mutex
	bytesPerSec float64
	concatCalls int
	probeCalls  int
	sliceCalls  int
	concatErr   error
	probeErr    error
	sliceErr    error
}

func newFakeAudioOps() *fakeAudioOps {
	return &fakeAudioOps{bytesPerSec: 4000} // 1 rune ≈ 1ms at bytesPerRune=4
}

func (f *fakeAudioOps) Concat(ctx context.Context, inputs []string, output string) error {
	f.mu.Lock()
	f.concatCalls++
	err := f.concatErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("no inputs")
	}
	if len(inputs) == 1 {
		return os.Rename(inputs[0], output)
	}
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, p := range inputs {
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if _, err := out.Write(raw); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeAudioOps) Probe(ctx context.Context, path string) (float64, error) {
	f.mu.Lock()
	f.probeCalls++
	err := f.probeErr
	f.mu.Unlock()
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return float64(st.Size()) / f.bytesPerSec, nil
}

func (f *fakeAudioOps) Slice(ctx context.Context, src, dst string, startSec, endSec float64) error {
	f.mu.Lock()
	f.sliceCalls++
	err := f.sliceErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	startByte := int(startSec * f.bytesPerSec)
	endByte := int(endSec * f.bytesPerSec)
	if startByte < 0 {
		startByte = 0
	}
	if endByte > len(raw) {
		endByte = len(raw)
	}
	if endByte < startByte {
		endByte = startByte
	}
	return os.WriteFile(dst, raw[startByte:endByte], 0o644)
}

type fakeTTSStore struct {
	mu      sync.Mutex
	entries map[int]struct {
		path       string
		durationMs int64
	}
	err error
}

func newFakeTTSStore() *fakeTTSStore {
	return &fakeTTSStore{entries: map[int]struct {
		path       string
		durationMs int64
	}{}}
}

func (f *fakeTTSStore) UpsertTTSArtifact(_ context.Context, _ string, sceneIndex int, ttsPath string, ttsDurationMs int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.entries[sceneIndex] = struct {
		path       string
		durationMs int64
	}{ttsPath, ttsDurationMs}
	return nil
}

type fakeRetryLimiter struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeRetryLimiter) Do(ctx context.Context, fn func(context.Context) error) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return fn(ctx)
}

type fakeRetryRecorder struct {
	mu      sync.Mutex
	retries []struct {
		stage  domain.Stage
		reason string
	}
}

func (f *fakeRetryRecorder) RecordRetry(_ context.Context, _ string, stage domain.Stage, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries = append(f.retries, struct {
		stage  domain.Stage
		reason string
	}{stage, reason})
	return nil
}

// v2Scenario builds a PipelineState whose Narration is the V2 acts shape:
// each `acts[i]` carries one continuous monologue and one BeatAnchor per
// supplied beat string. The flat list of beats across all acts yields the
// 0-indexed scene order the TTS track flattens to.
func v2Scenario(runID string, actMonologues []string, beatsPerAct [][]int) *agents.PipelineState {
	acts := make([]domain.ActScript, 0, len(actMonologues))
	actIDs := []string{domain.ActIncident, domain.ActMystery, domain.ActRevelation, domain.ActUnresolved}
	for i, mono := range actMonologues {
		actID := actIDs[i%len(actIDs)]
		runes := []rune(mono)
		anchors := make([]domain.BeatAnchor, 0, len(beatsPerAct[i]))
		var cursor int
		for _, runeLen := range beatsPerAct[i] {
			end := cursor + runeLen
			if end > len(runes) {
				end = len(runes)
			}
			anchors = append(anchors, domain.BeatAnchor{
				StartOffset:       cursor,
				EndOffset:         end,
				Mood:              "calm",
				Location:          "site-19",
				CharactersPresent: []string{"unknown"},
				EntityVisible:     false,
				ColorPalette:      "neutral",
				Atmosphere:        "subdued",
				FactTags:          []domain.FactTag{},
			})
			cursor = end
		}
		acts = append(acts, domain.ActScript{
			ActID:     actID,
			Monologue: mono,
			Beats:     anchors,
			Mood:      "calm",
		})
	}
	return &agents.PipelineState{
		RunID: runID,
		SCPID: "049",
		Narration: &domain.NarrationScript{
			SCPID:         "049",
			SourceVersion: domain.NarrationSourceVersionV2,
			Acts:          acts,
		},
	}
}

type ttsFixture struct {
	outputDir string
	runID     string
	tts       *fakeTTSSynthesizer
	store     *fakeTTSStore
	limiter   *fakeRetryLimiter
	clk       *clock.FakeClock
	track     pipeline.TTSTrack
	req       pipeline.PhaseBRequest
	audio     *fakeAudioOps
}

func newTTSFixture(t *testing.T, actMonologues []string, beatsPerAct [][]int) *ttsFixture {
	t.Helper()
	outputDir := t.TempDir()
	runID := "scp-049-run-1"
	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Now())
	logger, _ := testutil.CaptureLog(t)
	audio := newFakeAudioOps()

	cfg := pipeline.TTSTrackConfig{
		OutputDir:     outputDir,
		TTSModel:      "fake-tts",
		TTSVoice:      "longhua",
		AudioFormat:   "wav",
		MaxRetries:    3,
		MaxInputBytes: 1 << 20, // huge — single-call path by default
		TTS:           fakeTTS,
		Store:         store,
		Limiter:       limiter,
		Clock:         clk,
		Logger:        logger,
		AudioOps:      audio,
	}
	track, err := pipeline.NewTTSTrack(cfg)
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := v2Scenario(runID, actMonologues, beatsPerAct)
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageImage,
		Scenario: scenario,
	}
	return &ttsFixture{
		outputDir: outputDir,
		runID:     runID,
		tts:       fakeTTS,
		store:     store,
		limiter:   limiter,
		clk:       clk,
		track:     track,
		req:       req,
		audio:     audio,
	}
}

// ── Synthesis path tests (I/O matrix) ────────────────────────────────────────

func TestTTSTrack_SingleCallSynthesisProducesCanonicalRunAudio(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	mono := "한 번에 합성 가능한 짧은 모놀로그입니다."
	f := newTTSFixture(t,
		[]string{mono},
		[][]int{{12, 12}},
	)
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	canonical := filepath.Join(f.outputDir, f.runID, "tts", "run_audio.wav")
	st, err := os.Stat(canonical)
	if err != nil {
		t.Fatalf("run_audio.wav missing: %v", err)
	}

	f.tts.mu.Lock()
	got := len(f.tts.receivedReqs)
	f.tts.mu.Unlock()
	if got != 1 {
		t.Errorf("expected 1 synthesize call (single-call path), got %d", got)
	}

	// AC #1: canonical run audio's duration must reflect the merged-monologue
	// rune count at the configured pacing rate. With the test's fakeAudioOps
	// (4000 bytes/sec) and fakeTTS (4 bytes/rune), 1 rune ≈ 1ms, so the
	// canonical duration in ms must be ≥ rune count.
	durMs := int64(float64(st.Size()) / f.audio.bytesPerSec * 1000)
	wantMin := int64(len([]rune(pipeline.Transliterate(mono))))
	if durMs < wantMin {
		t.Errorf("canonical duration = %dms, want ≥ %dms (rune-count floor)", durMs, wantMin)
	}
}

func TestTTSTrack_LargeMonologueChunksOnSentenceBoundaries(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	body := strings.Repeat("문장 하나입니다. ", 30)
	runID := "scp-049-chunked"
	outputDir := t.TempDir()
	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Now())
	logger, _ := testutil.CaptureLog(t)
	audio := newFakeAudioOps()

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:     outputDir,
		TTSModel:      "fake-tts",
		TTSVoice:      "longhua",
		AudioFormat:   "wav",
		MaxRetries:    3,
		MaxInputBytes: 90,
		TTS:           fakeTTS,
		Store:         store,
		Limiter:       limiter,
		Clock:         clk,
		Logger:        logger,
		AudioOps:      audio,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	bodyRunes := len([]rune(body))
	scenario := v2Scenario(runID, []string{body}, [][]int{{bodyRunes / 4, bodyRunes / 4, bodyRunes / 4, bodyRunes - 3*(bodyRunes/4)}})
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageImage,
		Scenario: scenario,
	}
	if _, err := track(context.Background(), req); err != nil {
		t.Fatalf("track: %v", err)
	}

	fakeTTS.mu.Lock()
	calls := len(fakeTTS.receivedReqs)
	fakeTTS.mu.Unlock()
	if calls < 2 {
		t.Errorf("expected multi-chunk synthesis (≥2 calls), got %d", calls)
	}
	for _, r := range fakeTTS.receivedReqs {
		if len(r.Text) > 90 {
			t.Errorf("chunk text exceeds cap: %d bytes", len(r.Text))
		}
	}
}

func TestTTSTrack_PerBeatSlicesConcatToCanonicalRunAudioByteForByte(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t,
		[]string{"첫 번째 액트 모놀로그입니다.", "두 번째 액트 모놀로그입니다.", "세 번째.", "네 번째 마지막 액트입니다."},
		[][]int{{6, 8}, {6, 8}, {2, 2}, {6, 8}},
	)
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	runDir := filepath.Join(f.outputDir, f.runID)
	canonicalBytes, err := os.ReadFile(filepath.Join(runDir, "tts", "run_audio.wav"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}

	var concat []byte
	for i := 1; i <= 8; i++ {
		p := filepath.Join(runDir, "tts", fmt.Sprintf("scene_%02d.wav", i))
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read scene %d: %v", i, err)
		}
		concat = append(concat, raw...)
	}
	if len(concat) != len(canonicalBytes) {
		t.Fatalf("concat(slices) len=%d, canonical len=%d", len(concat), len(canonicalBytes))
	}
	for i := range concat {
		if concat[i] != canonicalBytes[i] {
			t.Fatalf("byte mismatch at offset %d", i)
		}
	}
}

func TestTTSTrack_AtomicFailureRemovesPartialFiles(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	body := strings.Repeat("문장 하나입니다. ", 5)
	f := newTTSFixture(t, []string{body}, [][]int{{30, 50}})

	// Force chunked path; second chunk fails non-retryably.
	logger, _ := testutil.CaptureLog(t)
	f.tts.err = fmt.Errorf("validation: %w", domain.ErrValidation)
	f.tts.errAfter = 2
	chunkedTrack, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:     f.outputDir,
		TTSModel:      "fake-tts",
		TTSVoice:      "longhua",
		AudioFormat:   "wav",
		MaxRetries:    3,
		MaxInputBytes: 60,
		TTS:           f.tts,
		Store:         f.store,
		Limiter:       f.limiter,
		Clock:         f.clk,
		Logger:        logger,
		AudioOps:      f.audio,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	if _, err := chunkedTrack(context.Background(), f.req); err == nil {
		t.Fatal("expected chunk failure to surface, got nil")
	}

	runDir := filepath.Join(f.outputDir, f.runID)
	canonical := filepath.Join(runDir, "tts", "run_audio.wav")
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Errorf("canonical run_audio.wav must NOT exist after atomic failure: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(runDir, "tts", ".chunk_*.wav"))
	if len(matches) != 0 {
		t.Errorf("partial chunk files leaked: %v", matches)
	}
}

func TestTTSTrack_RateLimitErrorRetriesAndRecordsRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"한 번 합성합니다."}, [][]int{{6}})
	f.tts.err = fmt.Errorf("rate limited: %w", domain.ErrRateLimited)
	f.tts.errAfter = 1

	retries := &fakeRetryRecorder{}
	logger, _ := testutil.CaptureLog(t)
	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:     f.outputDir,
		TTSModel:      "fake-tts",
		TTSVoice:      "longhua",
		AudioFormat:   "wav",
		MaxRetries:    3,
		MaxInputBytes: 1 << 20,
		TTS:           f.tts,
		Store:         f.store,
		Limiter:       f.limiter,
		Recorder:      retries,
		Clock:         f.clk,
		Logger:        logger,
		AudioOps:      f.audio,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	doneCh := make(chan error, 1)
	go func() {
		_, err := track(context.Background(), f.req)
		doneCh <- err
	}()

	waitForPendingSleepers(t, f.clk, 1)
	f.tts.mu.Lock()
	f.tts.err = nil
	f.tts.mu.Unlock()
	f.clk.Advance(2 * time.Second)

	if err := <-doneCh; err != nil {
		t.Fatalf("track after retry: %v", err)
	}

	retries.mu.Lock()
	defer retries.mu.Unlock()
	if len(retries.retries) != 1 {
		t.Fatalf("expected 1 retry recorded, got %d", len(retries.retries))
	}
	if retries.retries[0].stage != domain.StageTTS {
		t.Errorf("retry stage = %v, want %v", retries.retries[0].stage, domain.StageTTS)
	}
}

// ── Validation tests ─────────────────────────────────────────────────────────

func TestTTSTrack_NilNarrationFailsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"무대로."}, [][]int{{4}})
	f.req.Scenario.Narration = nil
	_, err := f.track(context.Background(), f.req)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("nil narration: expected ErrValidation, got %v", err)
	}
}

func TestTTSTrack_EmptyActsFailsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"무대로."}, [][]int{{4}})
	f.req.Scenario.Narration.Acts = nil
	_, err := f.track(context.Background(), f.req)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("empty acts: expected ErrValidation, got %v", err)
	}
}

func TestTTSTrack_BeatOffsetOutOfRangeFailsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"짧다."}, [][]int{{2}})
	f.req.Scenario.Narration.Acts[0].Beats[0].EndOffset = 9999
	_, err := f.track(context.Background(), f.req)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("beat overflow: expected ErrValidation, got %v", err)
	}
}

func TestTTSTrack_ZeroLengthBeatFailsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"짧다."}, [][]int{{2}})
	f.req.Scenario.Narration.Acts[0].Beats[0].EndOffset = f.req.Scenario.Narration.Acts[0].Beats[0].StartOffset
	_, err := f.track(context.Background(), f.req)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("zero-length beat: expected ErrValidation, got %v", err)
	}
}

func TestTTSTrack_NonMonotonicBeatsInActFailValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"한 액트의 모놀로그."}, [][]int{{4, 4}})
	f.req.Scenario.Narration.Acts[0].Beats[1].StartOffset = 1
	f.req.Scenario.Narration.Acts[0].Beats[1].EndOffset = 3
	_, err := f.track(context.Background(), f.req)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("overlapping beats: expected ErrValidation, got %v", err)
	}
}

func TestNewTTSTrack_RejectsNonWAVFormat(t *testing.T) {
	_, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:   t.TempDir(),
		TTSModel:    "fake-tts",
		TTSVoice:    "longhua",
		AudioFormat: "mp3",
		TTS:         newFakeTTS(),
		Store:       newFakeTTSStore(),
		Limiter:     &fakeRetryLimiter{},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("non-WAV format: expected ErrValidation, got %v", err)
	}
}

func TestTTSTrack_ZeroBeatsFailsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"짧다."}, [][]int{{}})
	_, err := f.track(context.Background(), f.req)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("zero beats: expected ErrValidation, got %v", err)
	}
}

// ── Wiring tests ─────────────────────────────────────────────────────────────

func TestTTSTrack_TransliteratesMergedMonologueBeforeSynthesize(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t,
		[]string{"SCP-049 보고서.", "엔터티 격리."},
		[][]int{{8}, {6}},
	)
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	f.tts.mu.Lock()
	defer f.tts.mu.Unlock()
	if len(f.tts.receivedReqs) == 0 {
		t.Fatal("no synthesize calls")
	}
	got := f.tts.receivedReqs[0].Text
	if strings.Contains(got, "SCP-049") {
		t.Errorf("transliteration not applied: input still contains SCP-049: %q", got)
	}
}

func TestTTSTrack_PassesConfiguredModelAndVoiceToProvider(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"안녕."}, [][]int{{2}})
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	f.tts.mu.Lock()
	defer f.tts.mu.Unlock()
	if len(f.tts.receivedReqs) == 0 {
		t.Fatal("no synthesize calls")
	}
	req := f.tts.receivedReqs[0]
	testutil.AssertEqual(t, req.Model, "fake-tts")
	testutil.AssertEqual(t, req.Voice, "longhua")
}

func TestTTSTrack_StoresPerBeatRelativePathInSegments(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t,
		[]string{"첫 번째.", "두 번째."},
		[][]int{{4}, {4}},
	)
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	if len(f.store.entries) != 2 {
		t.Fatalf("expected 2 segment rows, got %d", len(f.store.entries))
	}
	for sceneIndex, entry := range f.store.entries {
		if !strings.HasPrefix(entry.path, "tts/scene_") {
			t.Errorf("scene %d: path = %q, want prefix tts/scene_", sceneIndex, entry.path)
		}
		if strings.Contains(entry.path, f.outputDir) {
			t.Errorf("scene %d: path contains absolute run dir: %q", sceneIndex, entry.path)
		}
		if entry.durationMs <= 0 {
			t.Errorf("scene %d: duration_ms = %d, want > 0", sceneIndex, entry.durationMs)
		}
	}
}

func TestTTSTrack_RunLevelAuditEmittedOnce(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	runID := "scp-049-audit"
	auditLogger := pipeline.NewFileAuditLogger(outputDir)
	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Date(2026, 5, 4, 15, 0, 0, 0, time.UTC))
	logger, _ := testutil.CaptureLog(t)
	audio := newFakeAudioOps()

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:     outputDir,
		TTSModel:      "fake-tts",
		TTSVoice:      "longhua",
		AudioFormat:   "wav",
		MaxRetries:    3,
		MaxInputBytes: 1 << 20,
		AuditLogger:   auditLogger,
		TTS:           fakeTTS,
		Store:         store,
		Limiter:       limiter,
		Clock:         clk,
		Logger:        logger,
		AudioOps:      audio,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := v2Scenario(runID,
		[]string{"첫 번째.", "두 번째."},
		[][]int{{4}, {4}},
	)
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}
	if _, err := track(context.Background(), req); err != nil {
		t.Fatalf("track: %v", err)
	}

	auditPath := filepath.Join(outputDir, runID, "audit.log")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 run-level audit line, got %d: %s", len(lines), raw)
	}
	var entry domain.AuditEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("invalid audit JSON: %v", err)
	}
	if entry.EventType != domain.AuditEventTTSSynthesis {
		t.Errorf("event_type = %q, want %q", entry.EventType, domain.AuditEventTTSSynthesis)
	}
	if entry.Stage != string(domain.StageTTS) {
		t.Errorf("stage = %q, want %q", entry.Stage, domain.StageTTS)
	}
}

func TestTTSTrack_BlockedVoiceRejectedBeforeAnyAPICall(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	runID := "scp-049-blocked"
	auditLogger := pipeline.NewFileAuditLogger(outputDir)
	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Date(2026, 5, 4, 15, 0, 0, 0, time.UTC))
	logger, _ := testutil.CaptureLog(t)
	audio := newFakeAudioOps()

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:       outputDir,
		TTSModel:        "fake-tts",
		TTSVoice:        "blocked-voice",
		AudioFormat:     "wav",
		MaxRetries:      3,
		BlockedVoiceIDs: []string{"blocked-voice", "other-blocked"},
		AuditLogger:     auditLogger,
		TTS:             fakeTTS,
		Store:           store,
		Limiter:         limiter,
		Clock:           clk,
		Logger:          logger,
		AudioOps:        audio,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := v2Scenario(runID, []string{"내레이션."}, [][]int{{5}})
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}
	_, err = track(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for blocked voice")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("error does not wrap ErrValidation: %v", err)
	}
	if fakeTTS.callCount > 0 {
		t.Errorf("fake TTS called %d times, expected 0", fakeTTS.callCount)
	}
}

// ── No-regression: PhaseBRunner concurrency ─────────────────────────────────

func TestPhaseBRunner_TTSFailureDoesNotCancelImageTrack(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	imageDone := make(chan struct{})
	var imageCtxErrAfterTTSFail error

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, r pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			select {
			case <-imageDone:
			case <-ctx.Done():
			}
			imageCtxErrAfterTTSFail = ctx.Err()
			return pipeline.ImageTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageImage},
			}, nil
		},
		func(ctx context.Context, r pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			close(imageDone)
			return pipeline.TTSTrackResult{}, errors.New("tts failed")
		},
		nil,
		clock.RealClock{},
		logger,
		nil,
		nil,
	)

	_, _ = runner.Run(context.Background(), req)
	if imageCtxErrAfterTTSFail != nil {
		t.Errorf("TTS failure canceled image context: %v", imageCtxErrAfterTTSFail)
	}
}

// ── Integration: real SegmentStore ──────────────────────────────────────────

func TestPhaseBRunner_RealTTSTrack_PopulatesPerBeatSegments(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	outputDir := t.TempDir()

	run, err := runStore.Create(context.Background(), "049", outputDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	runID := run.ID

	fakeTTS := newFakeTTS()
	limiter := &fakeRetryLimiter{}
	logger, _ := testutil.CaptureLog(t)
	audio := newFakeAudioOps()

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:     outputDir,
		TTSModel:      "fake-tts",
		TTSVoice:      "longhua",
		AudioFormat:   "wav",
		MaxRetries:    3,
		MaxInputBytes: 1 << 20,
		TTS:           fakeTTS,
		Store:         segStore,
		Limiter:       limiter,
		Clock:         clock.RealClock{},
		Logger:        logger,
		AudioOps:      audio,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := v2Scenario(runID,
		[]string{"첫째 액트.", "둘째 액트.", "셋째.", "넷째."},
		[][]int{{4}, {4}, {2}, {2}},
	)
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}

	result, err := track(context.Background(), req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	if len(result.Artifacts) < 5 {
		t.Errorf("expected ≥5 artifacts (run_audio + 4 scenes), got %d", len(result.Artifacts))
	}

	canonical := filepath.Join(outputDir, runID, "tts", "run_audio.wav")
	if _, err := os.Stat(canonical); err != nil {
		t.Errorf("canonical run_audio missing: %v", err)
	}
	for i := 1; i <= 4; i++ {
		p := filepath.Join(outputDir, runID, "tts", fmt.Sprintf("scene_%02d.wav", i))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("scene %d slice missing: %v", i, err)
		}
	}

	segs, err := segStore.ListByRunID(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(segs) != 4 {
		t.Fatalf("expected 4 segment rows, got %d", len(segs))
	}
	for _, seg := range segs {
		if seg.TTSPath == nil || *seg.TTSPath == "" {
			t.Errorf("scene index %d: tts_path not set", seg.SceneIndex)
		}
		if seg.TTSDurationMs == nil || *seg.TTSDurationMs <= 0 {
			t.Errorf("scene index %d: tts_duration_ms not set", seg.SceneIndex)
		}
	}
}
