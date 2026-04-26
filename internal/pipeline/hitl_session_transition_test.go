package pipeline_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeHITLSessionStore is a minimal pipeline.HITLSessionStore stub that
// tracks calls so the transition-time upsert assertion can verify the
// Engine actually persisted a session row at the boundary.
type fakeHITLSessionStore struct {
	mu       sync.Mutex
	upserts  []*domain.HITLSession
	deletes  []string
	sessions map[string]*domain.HITLSession
}

func newFakeHITLSessionStore() *fakeHITLSessionStore {
	return &fakeHITLSessionStore{sessions: map[string]*domain.HITLSession{}}
}

func (f *fakeHITLSessionStore) ListByRunID(_ context.Context, _ string) ([]*domain.Decision, error) {
	return nil, nil
}

func (f *fakeHITLSessionStore) DecisionCountsByRunID(_ context.Context, _ string) (pipeline.DecisionCounts, error) {
	// 8-scene fixture is enough to exercise BuildSessionSnapshot; the
	// concrete count matters only insofar as the upsert payload mentions
	// "TotalScenes" — the boundary test asserts that an upsert happened,
	// not its scene count.
	return pipeline.DecisionCounts{TotalScenes: 8}, nil
}

func (f *fakeHITLSessionStore) GetSession(_ context.Context, runID string) (*domain.HITLSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[runID], nil
}

func (f *fakeHITLSessionStore) UpsertSession(_ context.Context, session *domain.HITLSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := *session
	f.upserts = append(f.upserts, &clone)
	f.sessions[session.RunID] = &clone
	return nil
}

func (f *fakeHITLSessionStore) DeleteSession(_ context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, runID)
	delete(f.sessions, runID)
	return nil
}

func (f *fakeHITLSessionStore) lastUpsertFor(runID string) *domain.HITLSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.upserts) - 1; i >= 0; i-- {
		if f.upserts[i].RunID == runID {
			return f.upserts[i]
		}
	}
	return nil
}

// TestEngine_RunPhaseB_UpsertsHITLSessionOnBatchReview reproduces the
// observed bug: when Phase B succeeds and the run lands in
// StageBatchReview/StatusWaiting, hitl_sessions must hold a row so the UI
// can render the scene list. Without the upsert hook the row stays missing
// and hitl_service warns "hitl session row missing for waiting run".
func TestEngine_RunPhaseB_UpsertsHITLSessionOnBatchReview(t *testing.T) {
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
	sessions := newFakeHITLSessionStore()
	eng := newEngine(t, runs, segs, outDir)
	eng.SetPhaseBExecutor(phaseB)
	eng.SetHITLSessionStore(sessions)

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if runs.run.Stage != domain.StageBatchReview {
		t.Fatalf("stage = %q, want %q", runs.run.Stage, domain.StageBatchReview)
	}
	got := sessions.lastUpsertFor(runID)
	if got == nil {
		t.Fatal("expected hitl_sessions upsert at batch_review transition; got none")
	}
	if got.Stage != domain.StageBatchReview {
		t.Fatalf("upserted session.Stage = %q, want %q", got.Stage, domain.StageBatchReview)
	}
	if got.SnapshotJSON == "" {
		t.Fatal("upserted session.SnapshotJSON is empty; want a serialized DecisionSnapshot")
	}
}

// TestEngine_RunPhaseB_NoUpsert_WhenStoreUnset ensures the legacy nil-store
// path stays a no-op (back-compat for tests/tools that don't wire HITL
// session persistence).
func TestEngine_RunPhaseB_NoUpsert_WhenStoreUnset(t *testing.T) {
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
	eng := newEngine(t, runs, segs, outDir) // no SetHITLSessionStore call
	eng.SetPhaseBExecutor(phaseB)

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	// Transition still happens; we just don't observe a panic or compile
	// error from the unset store.
	if runs.run.Stage != domain.StageBatchReview {
		t.Fatalf("stage = %q, want %q", runs.run.Stage, domain.StageBatchReview)
	}
}

// TestEngine_UpsertHelper_NonHITLStageIsNoop covers the boundary in
// upsertHITLSessionAtTransition: a successful transition INTO a non-HITL
// stage (e.g. Phase A retry → StageWrite/failed) must not produce a session
// row. Exposed indirectly here via Resume of a Phase A failure that lands
// in a non-HITL stage.
func TestEngine_UpsertHelper_NonHITLStageIsNoop(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outDir := t.TempDir()
	runID := "scp-049-run-1"
	runs := &fakeRunStore{run: &domain.Run{
		ID:     runID,
		SCPID:  "049",
		Stage:  domain.StageWrite,
		Status: domain.StatusFailed,
	}}
	segs := newFakeSegmentStore()
	mustMkdir(t, filepath.Join(outDir, runID))

	sessions := newFakeHITLSessionStore()
	eng := newEngine(t, runs, segs, outDir)
	eng.SetHITLSessionStore(sessions)

	if _, err := eng.Resume(context.Background(), runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := sessions.lastUpsertFor(runID); got != nil {
		t.Fatalf("expected no upsert for non-HITL Resume; got %+v", got)
	}
}
