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

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// --- fakes -----------------------------------------------------------------

type fakeRunStore struct {
	mu             sync.Mutex
	run            *domain.Run
	resetCalls     []resetCall
	phaseACalls    []domain.PhaseAAdvanceResult
	getErr         error
	resetErr       error
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

type fakeSegmentStore struct {
	mu          sync.Mutex
	byRun       map[string][]*domain.Episode
	deleteCalls []string
	listErr     error
	deleteErr   error
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

func TestResume_PhaseBFailure_DeletesSegmentsAndTTS(t *testing.T) {
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
		{RunID: runID, SceneIndex: 2},
	}
	// Artifacts present on disk: both Phase B tracks.
	runDir := filepath.Join(outDir, runID)
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_02.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")

	eng := newEngine(t, runs, segs, outDir)
	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// Phase B clean slate: all segments deleted.
	if _, ok := segs.byRun[runID]; ok {
		t.Errorf("segments not cleared; map entry remains")
	}
	if len(segs.deleteCalls) != 1 || segs.deleteCalls[0] != runID {
		t.Errorf("DeleteByRunID calls = %+v, want [%s]", segs.deleteCalls, runID)
	}
	// Failure in tts also wipes images/ — the whole phase re-runs.
	assertMissing(t, filepath.Join(runDir, "tts"))
	assertMissing(t, filepath.Join(runDir, "images"))
	// Status reset to running.
	if runs.resetCalls[0].status != domain.StatusRunning {
		t.Errorf("status = %q, want running", runs.resetCalls[0].status)
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
