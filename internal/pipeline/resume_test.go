package pipeline_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// --- fakes -----------------------------------------------------------------

type fakeRunStore struct {
	mu          sync.Mutex
	run         *domain.Run
	resetCalls  []resetCall
	phaseACalls []domain.PhaseAAdvanceResult
	getErr      error
	resetErr    error
}

type resetCall struct {
	id     string
	status domain.Status
}

func (f *fakeRunStore) Get(ctx context.Context, id string) (*domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.run == nil || f.run.ID != id {
		return nil, domain.ErrNotFound
	}
	copy := *f.run
	return &copy, nil
}

func (f *fakeRunStore) ResetForResume(ctx context.Context, id string, status domain.Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetCalls = append(f.resetCalls, resetCall{id, status})
	if f.resetErr != nil {
		return f.resetErr
	}
	if f.run != nil && f.run.ID == id {
		f.run.Status = status
		f.run.RetryReason = nil
		f.run.RetryCount++
	}
	return nil
}

func (f *fakeRunStore) ApplyPhaseAResult(ctx context.Context, id string, res domain.PhaseAAdvanceResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.phaseACalls = append(f.phaseACalls, res)
	if f.run != nil && f.run.ID == id {
		f.run.Stage = res.Stage
		f.run.Status = res.Status
		f.run.RetryReason = res.RetryReason
		f.run.CriticScore = res.CriticScore
		f.run.ScenarioPath = res.ScenarioPath
	}
	return nil
}

type fakePhaseBExecutor struct {
	run   func(context.Context, pipeline.PhaseBRequest) (pipeline.PhaseBResult, error)
	calls []pipeline.PhaseBRequest
}

func (f *fakePhaseBExecutor) Run(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.PhaseBResult, error) {
	f.calls = append(f.calls, req)
	if f.run == nil {
		return pipeline.PhaseBResult{Request: req}, nil
	}
	return f.run(ctx, req)
}

type fakeSegmentStore struct {
	mu              sync.Mutex
	byRun           map[string][]*domain.Episode
	deleteCalls     []string
	clearImageCalls []string
	clearTTSCalls   []string
	listErr         error
	deleteErr       error
}

func newFakeSegmentStore() *fakeSegmentStore {
	return &fakeSegmentStore{byRun: map[string][]*domain.Episode{}}
}

func (f *fakeSegmentStore) ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.byRun[runID], nil
}

func (f *fakeSegmentStore) DeleteByRunID(ctx context.Context, runID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, runID)
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	n := int64(len(f.byRun[runID]))
	delete(f.byRun, runID)
	return n, nil
}

func (f *fakeSegmentStore) ClearClipPathsByRunID(ctx context.Context, runID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	segs := f.byRun[runID]
	var n int64
	for _, seg := range segs {
		if seg.ClipPath != nil {
			seg.ClipPath = nil
			n++
		}
	}
	return n, nil
}

func (f *fakeSegmentStore) ClearImageArtifactsByRunID(ctx context.Context, runID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearImageCalls = append(f.clearImageCalls, runID)
	var n int64
	for _, seg := range f.byRun[runID] {
		cleared := false
		for i := range seg.Shots {
			if seg.Shots[i].ImagePath != "" {
				seg.Shots[i].ImagePath = ""
				cleared = true
			}
		}
		if cleared {
			n++
		}
	}
	return n, nil
}

func (f *fakeSegmentStore) ClearTTSArtifactsByRunID(ctx context.Context, runID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearTTSCalls = append(f.clearTTSCalls, runID)
	var n int64
	for _, seg := range f.byRun[runID] {
		if seg.TTSPath != nil || seg.TTSDurationMs != nil {
			seg.TTSPath = nil
			seg.TTSDurationMs = nil
			n++
		}
	}
	return n, nil
}

// --- helpers ---------------------------------------------------------------

func newEngine(t *testing.T, runs pipeline.RunStore, segments pipeline.SegmentStore, outputDir string) *pipeline.Engine {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{}))
	return pipeline.NewEngine(runs, segments, nil, clock.RealClock{}, outputDir, logger)
}

// --- tests -----------------------------------------------------------------

func TestResume_PhaseAFailure_NoCleanupNoSegmentDelete(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageWrite,
		Status: domain.StatusFailed,
	}}
	segs := newFakeSegmentStore()
	mustMkdir(t, filepath.Join(outDir, runID))

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if len(segs.deleteCalls) != 0 {
		t.Errorf("Phase A resume must not DELETE segments; got %d calls", len(segs.deleteCalls))
	}
	if len(runs.resetCalls) != 1 {
		t.Fatalf("expected 1 ResetForResume call; got %d", len(runs.resetCalls))
	}
	if runs.resetCalls[0].status != domain.StatusRunning {
		t.Errorf("status = %q, want running", runs.resetCalls[0].status)
	}
	if runs.run.RetryReason != nil {
		t.Errorf("retry_reason = %v, want nil (cleared)", runs.run.RetryReason)
	}
	if runs.run.RetryCount != 1 {
		t.Errorf("retry_count = %d, want 1", runs.run.RetryCount)
	}
}

func TestResume_PhaseBMixedFailure_TTSStagePreservesImages(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageTTS,
		Status: domain.StatusFailed,
	}}
	segs := newFakeSegmentStore()
	segs.byRun[runID] = []*domain.Episode{
		{
			RunID:         runID,
			SceneIndex:    0,
			TTSPath:       strPtr("tts/scene_01.wav"),
			TTSDurationMs: intPtr(1000),
			Shots:         []domain.Shot{{ImagePath: "images/scene_01/shot_01.png"}},
		},
	}
	runDir := filepath.Join(outDir, runID)
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if len(segs.deleteCalls) != 0 {
		t.Errorf("mixed tts failure should preserve segments; delete calls = %+v", segs.deleteCalls)
	}
	if len(segs.clearTTSCalls) != 1 || segs.clearTTSCalls[0] != runID {
		t.Errorf("ClearTTSArtifactsByRunID calls = %+v, want [%s]", segs.clearTTSCalls, runID)
	}
	assertMissing(t, filepath.Join(runDir, "tts"))
	assertPresent(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"))
	if segs.byRun[runID][0].TTSPath != nil || segs.byRun[runID][0].TTSDurationMs != nil {
		t.Fatalf("tts fields not cleared: %+v", segs.byRun[runID][0])
	}
	if segs.byRun[runID][0].Shots[0].ImagePath == "" {
		t.Fatal("image path unexpectedly cleared")
	}
	if runs.resetCalls[0].status != domain.StatusRunning {
		t.Errorf("status = %q, want running", runs.resetCalls[0].status)
	}
}

func TestResume_PhaseBMixedFailure_ImageStagePreservesTTS(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageImage,
		Status: domain.StatusFailed,
	}}
	ttsPath := "tts/scene_01.wav"
	segs := newFakeSegmentStore()
	segs.byRun[runID] = []*domain.Episode{{
		RunID:         runID,
		SceneIndex:    0,
		TTSPath:       &ttsPath,
		TTSDurationMs: intPtr(1000),
		Shots:         []domain.Shot{{ImagePath: "images/scene_01/shot_01.png"}},
	}}

	runDir := filepath.Join(outDir, runID)
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if len(segs.deleteCalls) != 0 {
		t.Errorf("mixed image failure should preserve segments; delete calls = %+v", segs.deleteCalls)
	}
	if len(segs.clearImageCalls) != 1 || segs.clearImageCalls[0] != runID {
		t.Errorf("ClearImageArtifactsByRunID calls = %+v, want [%s]", segs.clearImageCalls, runID)
	}
	assertMissing(t, filepath.Join(runDir, "images"))
	assertPresent(t, filepath.Join(runDir, "tts", "scene_01.wav"))
	if segs.byRun[runID][0].TTSPath == nil || *segs.byRun[runID][0].TTSPath != ttsPath {
		t.Fatalf("tts path not preserved: %+v", segs.byRun[runID][0])
	}
	if segs.byRun[runID][0].Shots[0].ImagePath != "" {
		t.Fatalf("image path still present: %+v", segs.byRun[runID][0].Shots[0])
	}
}

// TestResume_PhaseBFailure_NoPartialSuccess_FallsBackToCleanSlate guards
// AC-7 rule "this story should not regress the existing clean-slate behavior
// for full-Phase-B reruns when both tracks are invalid or when no partial
// success exists". When neither track has persisted artifacts, the engine
// must DELETE segments and clean both image/ and tts/ directories.
func TestResume_PhaseBFailure_NoPartialSuccess_FallsBackToCleanSlate(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageImage,
		Status: domain.StatusFailed,
	}}
	// Segment exists but neither image nor TTS artifacts were persisted.
	segs := newFakeSegmentStore()
	segs.byRun[runID] = []*domain.Episode{{
		RunID:      runID,
		SceneIndex: 0,
		Shots:      []domain.Shot{{ImagePath: ""}},
	}}
	runDir := filepath.Join(outDir, runID)
	mustMkdir(t, runDir)

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if len(segs.deleteCalls) != 1 {
		t.Errorf("clean-slate path must DELETE segments; got %d calls", len(segs.deleteCalls))
	}
	if len(segs.clearImageCalls) != 0 || len(segs.clearTTSCalls) != 0 {
		t.Errorf("clean-slate path must not use track-scoped clear helpers; image=%v tts=%v",
			segs.clearImageCalls, segs.clearTTSCalls)
	}
	assertMissing(t, filepath.Join(runDir, "images"))
	assertMissing(t, filepath.Join(runDir, "tts"))
}

func TestResume_PhaseBConfigured_SuccessAdvancesToBatchReview(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID:           runID,
		SCPID:        "049",
		Stage:        domain.StageTTS,
		Status:       domain.StatusFailed,
		ScenarioPath: strPtr("scenario.json"),
	}}
	segs := newFakeSegmentStore()
	mustWrite(t, filepath.Join(outDir, runID, "scenario.json"), `{"run_id":"scp-049-run-1"}`)

	phaseB := &fakePhaseBExecutor{}
	eng := newEngine(t, runs, segs, outDir)
	eng.SetPhaseBExecutor(phaseB)

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if len(phaseB.calls) != 1 {
		t.Fatalf("phaseB calls = %d, want 1", len(phaseB.calls))
	}
	wantScenarioPath := filepath.Join(outDir, runID, "scenario.json")
	if got := phaseB.calls[0].ScenarioPath; got != wantScenarioPath {
		t.Fatalf("ScenarioPath = %q, want %q", got, wantScenarioPath)
	}
	if runs.run.Stage != domain.StageBatchReview {
		t.Fatalf("stage = %q, want %q", runs.run.Stage, domain.StageBatchReview)
	}
	if runs.run.Status != domain.StatusWaiting {
		t.Fatalf("status = %q, want %q", runs.run.Status, domain.StatusWaiting)
	}
}

func TestResume_PhaseBConfigured_FailureRestoresFailedStatus(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID:           runID,
		SCPID:        "049",
		Stage:        domain.StageImage,
		Status:       domain.StatusFailed,
		ScenarioPath: strPtr("scenario.json"),
	}}
	segs := newFakeSegmentStore()
	mustWrite(t, filepath.Join(outDir, runID, "scenario.json"), `{"run_id":"scp-049-run-1"}`)

	phaseB := &fakePhaseBExecutor{
		run: func(context.Context, pipeline.PhaseBRequest) (pipeline.PhaseBResult, error) {
			return pipeline.PhaseBResult{}, errors.New("tts provider exploded")
		},
	}
	eng := newEngine(t, runs, segs, outDir)
	eng.SetPhaseBExecutor(phaseB)

	_, err := eng.Resume(context.Background(), runID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if runs.run.Stage != domain.StageImage {
		t.Fatalf("stage = %q, want %q", runs.run.Stage, domain.StageImage)
	}
	if runs.run.Status != domain.StatusFailed {
		t.Fatalf("status = %q, want %q", runs.run.Status, domain.StatusFailed)
	}
}

func TestResume_AssembleFailure_RemovesClipsAndOutputMP4(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageAssemble,
		Status: domain.StatusFailed,
	}}
	segs := newFakeSegmentStore()
	segs.byRun[runID] = []*domain.Episode{{RunID: runID, SceneIndex: 0}}

	runDir := filepath.Join(outDir, runID)
	mustWrite(t, filepath.Join(runDir, "clips", "scene_01.mp4"), "clip")
	mustWrite(t, filepath.Join(runDir, "output.mp4"), "final")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	assertMissing(t, filepath.Join(runDir, "clips"))
	assertMissing(t, filepath.Join(runDir, "output.mp4"))
	assertPresent(t, filepath.Join(runDir, "tts", "scene_01.wav"))
	// Segments NOT deleted — assemble is Phase C, not Phase B.
	if len(segs.deleteCalls) != 0 {
		t.Errorf("assemble resume must not DELETE segments; got %d calls", len(segs.deleteCalls))
	}
}

func TestResume_InconsistencyWithoutForce_Aborts(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageTTS,
		Status: domain.StatusFailed,
	}}
	tts := "tts/scene_01.wav"
	segs := newFakeSegmentStore()
	segs.byRun[runID] = []*domain.Episode{
		{RunID: runID, SceneIndex: 0, TTSPath: &tts},
	}
	mustMkdir(t, filepath.Join(outDir, runID))
	// tts file NOT on disk → inconsistency.

	eng := newEngine(t, runs, segs, outDir)
	_, err := eng.Resume(context.Background(), runID)
	if err == nil {
		t.Fatal("expected inconsistency abort; got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want errors.Is ErrValidation", err)
	}
	// No mutations performed.
	if len(runs.resetCalls) != 0 {
		t.Errorf("ResetForResume was called despite abort (%d calls)", len(runs.resetCalls))
	}
	if len(segs.deleteCalls) != 0 {
		t.Errorf("DeleteByRunID was called despite abort (%d calls)", len(segs.deleteCalls))
	}
	if runs.run.RetryCount != 0 {
		t.Errorf("retry_count incremented despite abort (%d)", runs.run.RetryCount)
	}
}

func TestResume_InconsistencyWithForce_ProceedsWithWarnings(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageTTS,
		Status: domain.StatusFailed,
	}}
	tts := "tts/scene_01.wav"
	segs := newFakeSegmentStore()
	segs.byRun[runID] = []*domain.Episode{
		{RunID: runID, SceneIndex: 0, TTSPath: &tts},
	}
	mustMkdir(t, filepath.Join(outDir, runID))

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.ResumeWithOptions(context.Background(), runID,
		pipeline.ResumeOptions{Force: true}); err != nil {
		t.Fatalf("Resume with Force: %v", err)
	}
	if len(runs.resetCalls) != 1 {
		t.Errorf("expected ResetForResume despite mismatch (Force=true); got %d", len(runs.resetCalls))
	}
}

func TestResume_CompletedRun_Conflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageComplete,
		Status: domain.StatusCompleted,
	}}
	segs := newFakeSegmentStore()

	eng := newEngine(t, runs, segs, outDir)
	_, err := eng.Resume(context.Background(), runID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict for completed run", err)
	}
}

func TestResume_CancelledRun_Conflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageTTS,
		Status: domain.StatusCancelled,
	}}
	segs := newFakeSegmentStore()

	eng := newEngine(t, runs, segs, outDir)
	_, err := eng.Resume(context.Background(), runID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict for cancelled run", err)
	}
}

func TestResume_PendingRun_Conflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StagePending,
		Status: domain.StatusPending,
	}}
	segs := newFakeSegmentStore()

	eng := newEngine(t, runs, segs, outDir)
	_, err := eng.Resume(context.Background(), runID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict for pending run", err)
	}
}

func TestResume_NonexistentRun_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runs := &fakeRunStore{} // no run seeded
	segs := newFakeSegmentStore()

	eng := newEngine(t, runs, segs, outDir)
	_, err := eng.Resume(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestResume_WaitingHITL_ResetsToWaiting(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageBatchReview,
		Status: domain.StatusWaiting,
	}}
	segs := newFakeSegmentStore()
	mustMkdir(t, filepath.Join(outDir, runID))

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if runs.resetCalls[0].status != domain.StatusWaiting {
		t.Errorf("HITL resume status = %q, want waiting", runs.resetCalls[0].status)
	}
	// No segments delete — batch_review is HITL, not Phase B.
	if len(segs.deleteCalls) != 0 {
		t.Errorf("HITL resume must not delete segments; got %d calls", len(segs.deleteCalls))
	}
}

func TestResume_Idempotent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageTTS,
		Status: domain.StatusFailed,
	}}
	segs := newFakeSegmentStore()
	segs.byRun[runID] = []*domain.Episode{
		{RunID: runID, SceneIndex: 0},
		{RunID: runID, SceneIndex: 1},
	}
	runDir := filepath.Join(outDir, runID)
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")

	eng := newEngine(t, runs, segs, outDir)

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("first Resume: %v", err)
	}
	stageAfter1 := runs.run.Stage
	statusAfter1 := runs.run.Status
	segCountAfter1 := len(segs.byRun[runID])
	treeAfter1 := snapshotFileTree(t, runDir)

	// Re-set status to failed so the second Resume has a legal entry point;
	// this simulates the same run having failed again (e.g., retry exhausted).
	runs.run.Status = domain.StatusFailed

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("second Resume: %v", err)
	}
	stageAfter2 := runs.run.Stage
	statusAfter2 := runs.run.Status
	segCountAfter2 := len(segs.byRun[runID])
	treeAfter2 := snapshotFileTree(t, runDir)

	if stageAfter1 != stageAfter2 {
		t.Errorf("stage drift: %q → %q", stageAfter1, stageAfter2)
	}
	if statusAfter1 != statusAfter2 {
		t.Errorf("status drift: %q → %q", statusAfter1, statusAfter2)
	}
	if segCountAfter1 != segCountAfter2 {
		t.Errorf("segment count drift: %d → %d", segCountAfter1, segCountAfter2)
	}
	if !slicesEqual(treeAfter1, treeAfter2) {
		t.Errorf("on-disk tree drift:\nfirst:  %v\nsecond: %v", treeAfter1, treeAfter2)
	}
	// retry_count is expected to increment each time — NOT part of NFR-R1 idempotency.
	if runs.run.RetryCount != 2 {
		t.Errorf("retry_count increments = %d, want 2 (once per resume)", runs.run.RetryCount)
	}
}

func TestResume_Advance_Stub(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runs := &fakeRunStore{}
	segs := newFakeSegmentStore()

	eng := newEngine(t, runs, segs, outDir)
	err := eng.Advance(context.Background(), "scp-049-run-1")
	if err == nil {
		t.Fatal("Advance should return 'not implemented' stub error")
	}
}

// TestResume_MetadataAck_MetadataBuilderWired verifies that when resuming from
// StageMetadataAck, the metadata builder is invoked exactly once (P6: re-run
// after CleanStageArtifacts removes metadata.json / manifest.json).
// Note: the full StageAssemble → metadata_ack integration path requires a real
// PhaseCRunner (ffmpeg) and is covered by the E2E test.
func TestResume_MetadataAck_MetadataBuilderWired(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"

	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageMetadataAck,
		Status: domain.StatusFailed,
	}}
	segs := newFakeSegmentStore()
	mustMkdir(t, filepath.Join(outDir, runID))

	var buildCalls int
	mock := pipeline.MetadataBuilderFunc(func(_ context.Context, id string) (domain.MetadataBundle, domain.SourceManifest, error) {
		if id != runID {
			t.Errorf("Build called with runID %q, want %q", id, runID)
		}
		buildCalls++
		return domain.MetadataBundle{Version: 1, RunID: id}, domain.SourceManifest{Version: 1, RunID: id}, nil
	})

	eng := newEngine(t, runs, segs, outDir)
	eng.SetPhaseCMetadataBuilder(mock)

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if buildCalls != 1 {
		t.Errorf("MetadataBuilder.Build called %d times, want 1", buildCalls)
	}
}

// TestResume_PhaseAEntryStage_DispatchesPhaseAAsync verifies that Resume on a
// failed Phase A stage (e.g. stage=write) asynchronously kicks off the Phase A
// executor goroutine. Without the dispatch, the run stays status=running forever
// after Resume returns 200 — the root cause of the zombie bug fixed in this PR.
func TestResume_PhaseAEntryStage_DispatchesPhaseAAsync(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID: runID, SCPID: "049",
		Stage:  domain.StageWrite,
		Status: domain.StatusFailed,
	}}
	segs := newFakeSegmentStore()
	mustMkdir(t, filepath.Join(outDir, runID))

	dispatched := make(chan string, 1)
	fakeExec := &fakePhaseAExecutor{
		runFn: func(_ context.Context, state *agents.PipelineState) error {
			dispatched <- state.RunID
			return nil
		},
	}

	eng := newEngine(t, runs, segs, outDir)
	eng.SetPhaseAExecutor(fakeExec)

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	select {
	case got := <-dispatched:
		if got != runID {
			t.Errorf("PhaseA dispatched with runID %q, want %q", got, runID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PhaseA executor not dispatched within 2s after Resume")
	}
}

type fakePhaseAExecutor struct {
	runFn func(context.Context, *agents.PipelineState) error
}

func (f *fakePhaseAExecutor) Run(ctx context.Context, state *agents.PipelineState) error {
	if f.runFn != nil {
		return f.runFn(ctx, state)
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func snapshotFileTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		paths = append(paths, rel)
		return nil
	})
	return paths
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intPtr(v int) *int {
	return &v
}
