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

type fakeTTSSynthesizer struct {
	mu           sync.Mutex
	receivedReqs []domain.TTSRequest
	audioBytes   []byte
	err          error
	errAfter     int // return err on the Nth call (1-indexed); 0 = never
	callCount    int
}

func newFakeTTS() *fakeTTSSynthesizer {
	return &fakeTTSSynthesizer{audioBytes: []byte("fake-wav-bytes")}
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
		if err := os.WriteFile(req.OutputPath, f.audioBytes, 0o644); err != nil {
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

type fakeTTSStore struct {
	mu      sync.Mutex
	entries map[int]struct{ path string; durationMs int64 }
	err     error
}

func newFakeTTSStore() *fakeTTSStore {
	return &fakeTTSStore{entries: map[int]struct{ path string; durationMs int64 }{}}
}

func (f *fakeTTSStore) UpsertTTSArtifact(_ context.Context, _ string, sceneIndex int, ttsPath string, ttsDurationMs int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.entries[sceneIndex] = struct{ path string; durationMs int64 }{ttsPath, ttsDurationMs}
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
	retries []struct{ stage domain.Stage; reason string }
}

func (f *fakeRetryRecorder) RecordRetry(_ context.Context, _ string, stage domain.Stage, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries = append(f.retries, struct{ stage domain.Stage; reason string }{stage, reason})
	return nil
}

// ttsTrackScenario builds a PipelineState with the given narration scenes.
func ttsTrackScenario(runID string, narrations []string) *agents.PipelineState {
	scenes := make([]domain.NarrationScene, len(narrations))
	for i, n := range narrations {
		scenes[i] = domain.NarrationScene{
			SceneNum:  i + 1,
			Narration: n,
		}
	}
	return &agents.PipelineState{
		RunID: runID,
		SCPID: "049",
		Narration: &domain.NarrationScript{
			SCPID:  "049",
			Scenes: scenes,
		},
	}
}

// ttsFixture builds a TTSTrackConfig with injected fakes.
type ttsFixture struct {
	outputDir string
	runID     string
	tts       *fakeTTSSynthesizer
	store     *fakeTTSStore
	limiter   *fakeRetryLimiter
	clk       *clock.FakeClock
	track     pipeline.TTSTrack
	req       pipeline.PhaseBRequest
}

func newTTSFixture(t *testing.T, narrations []string) *ttsFixture {
	t.Helper()
	outputDir := t.TempDir()
	runID := "scp-049-run-1"
	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Now())
	logger, _ := testutil.CaptureLog(t)

	cfg := pipeline.TTSTrackConfig{
		OutputDir:  outputDir,
		TTSModel:   "fake-tts",
		TTSVoice:   "longhua",
		AudioFormat: "wav",
		MaxRetries: 3,
		TTS:        fakeTTS,
		Store:      store,
		Limiter:    limiter,
		Clock:      clk,
		Logger:     logger,
	}
	track, err := pipeline.NewTTSTrack(cfg)
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := ttsTrackScenario(runID, narrations)
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
	}
}

// ── AC-2 tests ────────────────────────────────────────────────────────────────

func TestTTSTrack_TransliteratesNarrationBeforeSynthesize(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Narration with English word + number — transliteration must convert both.
	f := newTTSFixture(t, []string{"SCP-049 entity class doctor"})
	_, err := f.track(context.Background(), f.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	f.tts.mu.Lock()
	defer f.tts.mu.Unlock()
	if len(f.tts.receivedReqs) != 1 {
		t.Fatalf("expected 1 synthesize call, got %d", len(f.tts.receivedReqs))
	}

	got := f.tts.receivedReqs[0].Text
	want := pipeline.Transliterate("SCP-049 entity class doctor")
	testutil.AssertEqual(t, got, want)

	// The transliterated text must contain no ASCII digits and no isolated Latin words.
	if strings.ContainsAny(got, "0123456789") {
		t.Errorf("transliterated text contains ASCII digits: %q", got)
	}
}

func TestTTSTrack_PreservesOriginalNarrationInScenarioAndSegments(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	original := "SCP-049 entity class doctor"

	// Wire the real SegmentStore so we can verify segments.narration is not
	// overwritten by the TTS track (AC-2 rule: raw narration preserved in DB).
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	outputDir := t.TempDir()

	run, err := runStore.Create(context.Background(), "049", outputDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	runID := run.ID

	// Seed segments.narration with the original narration BEFORE the TTS track
	// runs so we can detect any stomp on the narration column.
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO segments (run_id, scene_index, narration, status) VALUES (?, ?, ?, 'pending')`,
		runID, 0, original); err != nil {
		t.Fatalf("seed narration: %v", err)
	}

	fakeTTS := newFakeTTS()
	limiter := &fakeRetryLimiter{}
	logger, _ := testutil.CaptureLog(t)

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:   outputDir,
		TTSModel:    "fake-tts",
		TTSVoice:    "longhua",
		AudioFormat: "wav",
		MaxRetries:  3,
		TTS:         fakeTTS,
		Store:       segStore,
		Limiter:     limiter,
		Clock:       clock.RealClock{},
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := ttsTrackScenario(runID, []string{original})
	req := pipeline.PhaseBRequest{RunID: runID, Stage: domain.StageTTS, Scenario: scenario}

	if _, err := track(context.Background(), req); err != nil {
		t.Fatalf("track: %v", err)
	}

	// Scenario narration must be untouched in memory.
	if scenario.Narration.Scenes[0].Narration != original {
		t.Errorf("in-memory narration mutated: got %q, want %q",
			scenario.Narration.Scenes[0].Narration, original)
	}

	// segments.narration must be untouched in DB.
	segs, err := segStore.ListByRunID(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment row, got %d", len(segs))
	}
	if segs[0].Narration == nil || *segs[0].Narration != original {
		var got string
		if segs[0].Narration != nil {
			got = *segs[0].Narration
		}
		t.Errorf("segments.narration mutated by TTS track: got %q, want %q", got, original)
	}
}

func TestTTSTrack_EmptyNarrationFailsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// nil Narration
	f := newTTSFixture(t, nil)
	f.req.Scenario.Narration = nil
	_, err := f.track(context.Background(), f.req)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("nil narration: expected ErrValidation, got %v", err)
	}

	// empty narration string in a scene
	f2 := newTTSFixture(t, []string{""})
	_, err2 := f2.track(context.Background(), f2.req)
	if !errors.Is(err2, domain.ErrValidation) {
		t.Errorf("empty narration: expected ErrValidation, got %v", err2)
	}
}

// ── AC-4 tests ────────────────────────────────────────────────────────────────

func TestTTSTrack_UsesSharedDashScopeLimiter(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"씬 하나", "씬 둘", "씬 셋"})
	_, err := f.track(context.Background(), f.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	f.limiter.mu.Lock()
	got := f.limiter.calls
	f.limiter.mu.Unlock()
	testutil.AssertEqual(t, got, 3) // one limiter.Do per scene
}

func TestTTSTrack_RetriesOn429AndRecordsRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"씬 하나"})

	// First call returns rate-limit error, second succeeds.
	f.tts.err = fmt.Errorf("rate limited: %w", domain.ErrRateLimited)
	f.tts.errAfter = 1

	retries := &fakeRetryRecorder{}
	logger, _ := testutil.CaptureLog(t)
	cfg := pipeline.TTSTrackConfig{
		OutputDir:   f.outputDir,
		TTSModel:    "fake-tts",
		TTSVoice:    "longhua",
		AudioFormat: "wav",
		MaxRetries:  3,
		TTS:         f.tts,
		Store:       f.store,
		Limiter:     f.limiter,
		Recorder:    retries,
		Clock:       f.clk,
		Logger:      logger,
	}
	track, err := pipeline.NewTTSTrack(cfg)
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	doneCh := make(chan error, 1)
	go func() {
		_, err := track(context.Background(), f.req)
		doneCh <- err
	}()

	// The retry helper sleeps before retry; advance the clock to unblock.
	waitForPendingSleepers(t, f.clk, 1)
	// Clear the error so the second call succeeds.
	f.tts.mu.Lock()
	f.tts.err = nil
	f.tts.mu.Unlock()
	// Advance by 2s: backoff for attempt 0 is 1s+jitter(0-500ms), so 2s is sufficient.
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
	if retries.retries[0].reason != "rate_limit" {
		t.Errorf("retry reason = %q, want %q", retries.retries[0].reason, "rate_limit")
	}
}

func TestTTSTrack_NonRetryableErrorSurfacesImmediately(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"씬 하나"})
	f.tts.err = fmt.Errorf("bad request: %w", domain.ErrValidation)

	_, err := f.track(context.Background(), f.req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Non-retryable error must surface without consuming retries.
	f.tts.mu.Lock()
	calls := f.tts.callCount
	f.tts.mu.Unlock()
	if calls > 1 {
		t.Errorf("expected 1 call (no retry for non-retryable), got %d", calls)
	}
}

// ── AC-5 tests ────────────────────────────────────────────────────────────────

func TestTTSTrack_WritesAudioToSceneCanonicalPaths(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"씬 하나", "씬 둘"})
	_, err := f.track(context.Background(), f.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	runDir := filepath.Join(f.outputDir, f.runID)
	for _, want := range []string{
		filepath.Join(runDir, "tts", "scene_01.wav"),
		filepath.Join(runDir, "tts", "scene_02.wav"),
	} {
		if _, err := os.Stat(want); os.IsNotExist(err) {
			t.Errorf("expected audio file %s, not found", want)
		}
	}
}

func TestTTSTrack_StoresRelativePathInSegments(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"씬 하나"})
	_, err := f.track(context.Background(), f.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	f.store.mu.Lock()
	entry, ok := f.store.entries[0]
	f.store.mu.Unlock()
	if !ok {
		t.Fatal("no entry for scene index 0")
	}
	if !strings.HasPrefix(entry.path, "tts/") {
		t.Errorf("tts_path = %q, want prefix \"tts/\"", entry.path)
	}
	if strings.Contains(entry.path, f.outputDir) {
		t.Errorf("tts_path contains absolute run dir: %q", entry.path)
	}
}

func TestTTSTrack_RerunOverwritesDeterministically(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"씬 하나"})
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("second run: %v", err)
	}

	runDir := filepath.Join(f.outputDir, f.runID)
	p := filepath.Join(runDir, "tts", "scene_01.wav")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected %s after re-run: %v", p, err)
	}
}

// ── AC-7 tests ────────────────────────────────────────────────────────────────

func TestTTSTrack_PassesConfiguredModelAndVoiceToProvider(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	f := newTTSFixture(t, []string{"씬 하나"})
	// The fixture already uses TTSModel="fake-tts" and TTSVoice="longhua"
	_, err := f.track(context.Background(), f.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	f.tts.mu.Lock()
	defer f.tts.mu.Unlock()
	if len(f.tts.receivedReqs) == 0 {
		t.Fatal("no synthesize calls recorded")
	}
	req := f.tts.receivedReqs[0]
	testutil.AssertEqual(t, req.Model, "fake-tts")
	testutil.AssertEqual(t, req.Voice, "longhua")
}

// ── AC-8 tests (no-regression for PhaseBRunner) ───────────────────────────────

func TestPhaseBRunner_TTSFailureDoesNotCancelImageTrack(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	req := phaseBRequest(t)
	logger, _ := testutil.CaptureLog(t)
	imageDone := make(chan struct{})
	var imageCtxErrAfterTTSFail error

	runner := pipeline.NewPhaseBRunner(
		func(ctx context.Context, r pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
			// Wait for TTS to fail, then check if our context was cancelled.
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
			// TTS fails immediately
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

// ── integration tests ─────────────────────────────────────────────────────────

// TestPhaseBRunner_RealTTSTrack_WithFakeProvider_CompletesAllScenes wires the
// real TTSTrack (fake TTSSynthesizer + real SQLite SegmentStore) and drives a
// 3-scene Phase B run end-to-end, asserting disk artifacts and DB rows.
func TestPhaseBRunner_RealTTSTrack_WithFakeProvider_CompletesAllScenes(t *testing.T) {
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

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:   outputDir,
		TTSModel:    "fake-tts",
		TTSVoice:    "longhua",
		AudioFormat: "wav",
		MaxRetries:  3,
		TTS:         fakeTTS,
		Store:       segStore,
		Limiter:     limiter,
		Clock:       clock.RealClock{},
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	narrations := []string{"SCP-049 entity class doctor", "씬 둘 text", "씬 셋 text"}
	scenario := ttsTrackScenario(runID, narrations)
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}

	result, err := track(context.Background(), req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	if len(result.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(result.Artifacts))
	}

	// All three audio files must exist on disk.
	for i := 1; i <= 3; i++ {
		p := filepath.Join(outputDir, runID, "tts", fmt.Sprintf("scene_%02d.wav", i))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("scene %d: expected file %s: %v", i, p, err)
		}
	}

	// Real SegmentStore must have tts_path and tts_duration_ms for each scene.
	segs, err := segStore.ListByRunID(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(segs) != 3 {
		t.Fatalf("expected 3 segment rows, got %d", len(segs))
	}
	for _, seg := range segs {
		if seg.TTSPath == nil || *seg.TTSPath == "" {
			t.Errorf("scene index %d: tts_path not set in DB", seg.SceneIndex)
		}
		if seg.TTSDurationMs == nil || *seg.TTSDurationMs <= 0 {
			t.Errorf("scene index %d: tts_duration_ms not set in DB", seg.SceneIndex)
		}
	}

	// Transliteration applied: first scene had English + SCP number.
	fakeTTS.mu.Lock()
	defer fakeTTS.mu.Unlock()
	if len(fakeTTS.receivedReqs) == 0 {
		t.Fatal("no synthesize calls recorded")
	}
	wantText := pipeline.Transliterate("SCP-049 entity class doctor")
	testutil.AssertEqual(t, fakeTTS.receivedReqs[0].Text, wantText)
}

// TestResume_PhaseBRegenerationRebuildsTTSAfterFailure proves that after
// TTS artifacts are cleaned (simulating a resume), re-running the TTS track
// deterministically rebuilds tts/ files and segment rows.
func TestResume_PhaseBRegenerationRebuildsTTSAfterFailure(t *testing.T) {
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

	buildTrack := func() pipeline.TTSTrack {
		track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
			OutputDir:   outputDir,
			TTSModel:    "fake-tts",
			TTSVoice:    "longhua",
			AudioFormat: "wav",
			MaxRetries:  3,
			TTS:         fakeTTS,
			Store:       segStore,
			Limiter:     limiter,
			Clock:       clock.RealClock{},
			Logger:      logger,
		})
		if err != nil {
			t.Fatalf("NewTTSTrack: %v", err)
		}
		return track
	}

	narrations := []string{"씬 하나", "씬 둘"}
	scenario := ttsTrackScenario(runID, narrations)
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}

	// First run: completes successfully.
	if _, err := buildTrack()(context.Background(), req); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Simulate resume cleanup: clear DB rows and delete tts/ directory.
	if _, err := segStore.ClearTTSArtifactsByRunID(context.Background(), runID); err != nil {
		t.Fatalf("ClearTTSArtifactsByRunID: %v", err)
	}
	ttsDir := filepath.Join(outputDir, runID, "tts")
	if err := os.RemoveAll(ttsDir); err != nil {
		t.Fatalf("RemoveAll tts dir: %v", err)
	}
	if _, err := os.Stat(ttsDir); !os.IsNotExist(err) {
		t.Fatal("tts dir should be absent after cleanup")
	}

	// Second run (resume): must rebuild artifacts and DB rows.
	if _, err := buildTrack()(context.Background(), req); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// Files must be back on disk.
	for i := 1; i <= 2; i++ {
		p := filepath.Join(ttsDir, fmt.Sprintf("scene_%02d.wav", i))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("rebuild: expected %s: %v", p, err)
		}
	}

	// DB rows must have tts_path populated again.
	segs, err := segStore.ListByRunID(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListByRunID after resume: %v", err)
	}
	for _, seg := range segs {
		if seg.TTSPath == nil || *seg.TTSPath == "" {
			t.Errorf("scene index %d: tts_path not rebuilt in DB", seg.SceneIndex)
		}
	}
}

// ── Audit logging ──────────────────────────────────────────────────────────────

func TestTTSTrack_WritesAuditLogOnSuccess(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	runID := "scp-049-audit"
	narrations := []string{"Scene one narration.", "Scene two narration."}

	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Date(2026, 4, 18, 15, 0, 0, 0, time.UTC))
	logger, _ := testutil.CaptureLog(t)
	auditLogger := pipeline.NewFileAuditLogger(outputDir)

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:   outputDir,
		TTSModel:    "fake-tts",
		TTSVoice:    "longhua",
		AudioFormat: "wav",
		MaxRetries:  3,
		AuditLogger: auditLogger,
		TTS:         fakeTTS,
		Store:       store,
		Limiter:     limiter,
		Clock:       clk,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := ttsTrackScenario(runID, narrations)
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}

	_, err = track(context.Background(), req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	auditPath := filepath.Join(outputDir, runID, "audit.log")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 audit lines for 2 scenes, got %d", len(lines))
	}
	for i, line := range lines {
		var entry domain.AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		if entry.EventType != domain.AuditEventTTSSynthesis {
			t.Errorf("line %d: event_type=%q, want %q", i, entry.EventType, domain.AuditEventTTSSynthesis)
		}
		if entry.RunID != runID {
			t.Errorf("line %d: run_id=%q, want %q", i, entry.RunID, runID)
		}
		if entry.Stage != string(domain.StageTTS) {
			t.Errorf("line %d: stage=%q, want %q", i, entry.Stage, domain.StageTTS)
		}
	}
}

func TestTTSTrack_BlockedVoiceRejectedWithAuditLog(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	runID := "scp-049-blocked"
	auditLogger := pipeline.NewFileAuditLogger(outputDir)
	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Date(2026, 4, 18, 15, 0, 0, 0, time.UTC))
	logger, _ := testutil.CaptureLog(t)

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
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := ttsTrackScenario(runID, []string{"Narration"})
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}

	_, err = track(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for blocked voice, got nil")
	}
	if got, want := err.Error(), "Voice profile 'blocked-voice' is blocked by compliance policy"; got != want {
		t.Fatalf("error message = %q, want %q", got, want)
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("error does not wrap ErrValidation: %v", err)
	}

	if fakeTTS.callCount > 0 {
		t.Errorf("fake TTS called %d times, expected 0", fakeTTS.callCount)
	}

	auditPath := filepath.Join(outputDir, runID, "audit.log")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	var entry domain.AuditEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal audit entry: %v", err)
	}
	if entry.EventType != domain.AuditEventVoiceBlocked {
		t.Errorf("event_type=%q, want %q", entry.EventType, domain.AuditEventVoiceBlocked)
	}
	if entry.BlockedID != "blocked-voice" {
		t.Errorf("blocked_id=%q, want %q", entry.BlockedID, "blocked-voice")
	}
}

func TestTTSTrack_BlockedVoiceAllowsOtherVoices(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	runID := "scp-049-allowed"
	auditLogger := pipeline.NewFileAuditLogger(outputDir)
	fakeTTS := newFakeTTS()
	store := newFakeTTSStore()
	limiter := &fakeRetryLimiter{}
	clk := clock.NewFakeClock(time.Date(2026, 4, 18, 15, 0, 0, 0, time.UTC))
	logger, _ := testutil.CaptureLog(t)

	track, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:       outputDir,
		TTSModel:        "fake-tts",
		TTSVoice:        "allowed-voice",
		AudioFormat:     "wav",
		MaxRetries:      3,
		BlockedVoiceIDs: []string{"blocked-voice-1", "blocked-voice-2"},
		AuditLogger:     auditLogger,
		TTS:             fakeTTS,
		Store:           store,
		Limiter:         limiter,
		Clock:           clk,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("NewTTSTrack: %v", err)
	}

	scenario := ttsTrackScenario(runID, []string{"Narration"})
	req := pipeline.PhaseBRequest{
		RunID:    runID,
		Stage:    domain.StageTTS,
		Scenario: scenario,
	}

	_, err = track(context.Background(), req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	if fakeTTS.callCount == 0 {
		t.Fatal("fake TTS was never called (voice incorrectly blocked)")
	}

	auditPath := filepath.Join(outputDir, runID, "audit.log")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	var entry domain.AuditEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("unmarshal audit entry: %v", err)
	}
	if entry.EventType == domain.AuditEventVoiceBlocked {
		t.Fatal("voice was incorrectly blocked")
	}
	if entry.EventType != domain.AuditEventTTSSynthesis {
		t.Errorf("event_type=%q, want %q", entry.EventType, domain.AuditEventTTSSynthesis)
	}
}

func TestTTSTrack_NilAuditLoggerDoesNotPanic(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	fx := newTTSFixture(t, []string{"Hello world"})
	fx.req.Stage = domain.StageTTS

	_, err := fx.track(context.Background(), fx.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}
}
