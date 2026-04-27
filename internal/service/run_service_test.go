package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func newTestService(t testing.TB) *service.RunService {
	t.Helper()
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	return service.NewRunService(store, nil)
}

func TestRunService_Create_Valid(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := newTestService(t)
	outDir := t.TempDir()

	run, err := svc.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	testutil.AssertEqual(t, run.ID, "scp-049-run-1")
	testutil.AssertEqual(t, run.Status, domain.StatusPending)
	testutil.AssertEqual(t, run.Stage, domain.StagePending)
}

func TestRunService_Create_EmptySCPID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := newTestService(t)

	_, err := svc.Create(context.Background(), "", t.TempDir())
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestRunService_Create_InvalidSCPID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := newTestService(t)
	outDir := t.TempDir()

	badIDs := []string{
		"../etc",
		"foo/bar",
		"scp 049",
		"scp;rm -rf",
		"foo.bar",
		"\x00null",
	}
	for _, id := range badIDs {
		if _, err := svc.Create(context.Background(), id, outDir); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("scp_id %q: expected ErrValidation, got %v", id, err)
		}
	}
}

func TestRunService_Get_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := newTestService(t)

	_, err := svc.Get(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRunService_Cancel_Conflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := newTestService(t)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := svc.Create(ctx, "049", outDir)

	// Pending status → ErrConflict
	err := svc.Cancel(ctx, run.ID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestRunService_List_Empty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := newTestService(t)

	runs, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	testutil.AssertEqual(t, len(runs), 0)
}

// --- Resume --------------------------------------------------------------

type fakeResumer struct {
	calls        []resumeCall
	prepareCalls []resumeCall
	executeCalls []string
	returnErr    error
	report       *domain.InconsistencyReport
	prepareRun   *domain.Run
}

type resumeCall struct {
	runID string
	opts  pipeline.ResumeOptions
}

func (f *fakeResumer) ResumeWithOptions(ctx context.Context, runID string, opts pipeline.ResumeOptions) (*domain.InconsistencyReport, error) {
	f.calls = append(f.calls, resumeCall{runID: runID, opts: opts})
	return f.report, f.returnErr
}

func (f *fakeResumer) PrepareResume(ctx context.Context, runID string, opts pipeline.ResumeOptions) (*domain.Run, *domain.InconsistencyReport, error) {
	f.prepareCalls = append(f.prepareCalls, resumeCall{runID: runID, opts: opts})
	return f.prepareRun, f.report, f.returnErr
}

func (f *fakeResumer) ExecuteResume(ctx context.Context, runID string) error {
	f.executeCalls = append(f.executeCalls, runID)
	return f.returnErr
}

func TestRunService_Resume_NoResumer_Validation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := newTestService(t) // nil resumer
	_, _, err := svc.Resume(context.Background(), "scp-049-run-1", false)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation when resumer is nil; got %v", err)
	}
}

func TestRunService_Resume_ForwardsForceFlag(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	fr := &fakeResumer{}
	svc := service.NewRunService(store, fr)

	outDir := t.TempDir()
	run, _ := svc.Create(context.Background(), "049", outDir)
	database.ExecContext(context.Background(),
		`UPDATE runs SET status = 'failed' WHERE id = ?`, run.ID)

	if _, _, err := svc.Resume(context.Background(), run.ID, true); err != nil {
		t.Fatalf("Resume(force=true): %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 resumer call, got %d", len(fr.calls))
	}
	if fr.calls[0].runID != run.ID {
		t.Errorf("runID forwarded = %q, want %q", fr.calls[0].runID, run.ID)
	}
	if !fr.calls[0].opts.Force {
		t.Errorf("force flag not forwarded to resumer")
	}
}

// --- AcknowledgeMetadata ---------------------------------------------------

func TestRunService_AcknowledgeMetadata_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := svc.Create(ctx, "049", outDir)

	// Advance run to metadata_ack + waiting.
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = 'metadata_ack', status = 'waiting' WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed stage/status: %v", err)
	}

	updated, err := svc.AcknowledgeMetadata(ctx, run.ID)
	if err != nil {
		t.Fatalf("AcknowledgeMetadata: %v", err)
	}
	testutil.AssertEqual(t, updated.Stage, domain.StageComplete)
	testutil.AssertEqual(t, updated.Status, domain.StatusCompleted)
}

func TestRunService_AcknowledgeMetadata_WrongStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := svc.Create(ctx, "049", outDir)
	// Run is at pending+pending — wrong stage.

	_, err := svc.AcknowledgeMetadata(ctx, run.ID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestRunService_AcknowledgeMetadata_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)

	_, err := svc.AcknowledgeMetadata(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRunService_Resume_PropagatesResumerError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	fr := &fakeResumer{returnErr: domain.ErrConflict}
	svc := service.NewRunService(store, fr)

	_, _, err := svc.Resume(context.Background(), "scp-049-run-1", false)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("expected ErrConflict propagation; got %v", err)
	}
}

// fakePromptVersionProvider supplies a canned active prompt tag for
// AC-3 stamping tests.
type fakePromptVersionProvider struct {
	tag *db.PromptVersionTag
}

func (f *fakePromptVersionProvider) ActivePromptVersion() *db.PromptVersionTag {
	if f.tag == nil {
		return nil
	}
	v := *f.tag
	return &v
}

func TestRunService_Create_StampsActivePromptVersion(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	svc.SetPromptVersionProvider(&fakePromptVersionProvider{
		tag: &db.PromptVersionTag{Version: "20260424T000000Z-abc1234", Hash: "deadbeef"},
	})

	run, err := svc.Create(context.Background(), "049", t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.CriticPromptVersion == nil || *run.CriticPromptVersion != "20260424T000000Z-abc1234" {
		t.Errorf("critic_prompt_version = %v, want 20260424T000000Z-abc1234", run.CriticPromptVersion)
	}
	if run.CriticPromptHash == nil || *run.CriticPromptHash != "deadbeef" {
		t.Errorf("critic_prompt_hash = %v, want deadbeef", run.CriticPromptHash)
	}
}

func TestRunService_Create_NilProviderLeavesColumnsNull(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	// Provider intentionally nil → "no prompt saved this session" path.

	run, err := svc.Create(context.Background(), "049", t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.CriticPromptVersion != nil {
		t.Errorf("want nil version, got %q", *run.CriticPromptVersion)
	}
	if run.CriticPromptHash != nil {
		t.Errorf("want nil hash, got %q", *run.CriticPromptHash)
	}
}

func TestRunService_Create_ProviderReturningNilLeavesColumnsNull(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	svc.SetPromptVersionProvider(&fakePromptVersionProvider{tag: nil})

	run, err := svc.Create(context.Background(), "049", t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.CriticPromptVersion != nil || run.CriticPromptHash != nil {
		t.Errorf("want both nil, got version=%v hash=%v",
			run.CriticPromptVersion, run.CriticPromptHash)
	}
}

type fakeDryRunProvider struct {
	value bool
	err   error
}

func (f *fakeDryRunProvider) EffectiveDryRun(_ context.Context) (bool, error) {
	return f.value, f.err
}

func TestRunService_Create_SnapshotsDryRunFromProvider(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	svc.SetDryRunProvider(&fakeDryRunProvider{value: true})

	run, err := svc.Create(context.Background(), "049", t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !run.DryRun {
		t.Errorf("DryRun = false, want true (provider returned true)")
	}
}

func TestRunService_Create_NilDryRunProviderDefaultsFalse(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	// No SetDryRunProvider — production-default safety stance.

	run, err := svc.Create(context.Background(), "049", t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if run.DryRun {
		t.Errorf("DryRun = true, want false (no provider)")
	}
}

func TestRunService_Create_DryRunFlagPersistsAfterToggle(t *testing.T) {
	// Settings toggling after row creation must not retroactively change
	// runs.dry_run. The snapshot is the contract.
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	provider := &fakeDryRunProvider{value: true}
	svc.SetDryRunProvider(provider)

	run, err := svc.Create(context.Background(), "049", t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Operator flips Settings off mid-life.
	provider.value = false

	got, err := svc.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.DryRun {
		t.Errorf("DryRun = false after Settings toggle, want true (snapshot must be immutable)")
	}
}

// --- ApproveScenarioReview ------------------------------------------------

type fakeHITLSessionStore struct {
	upserts []*domain.HITLSession
	deletes []string
	session *domain.HITLSession
	counts  *pipeline.DecisionCounts // override: returned by DecisionCountsByRunID when non-nil
}

func (f *fakeHITLSessionStore) ListByRunID(_ context.Context, _ string) ([]*domain.Decision, error) {
	return nil, nil
}
func (f *fakeHITLSessionStore) DecisionCountsByRunID(_ context.Context, _ string) (pipeline.DecisionCounts, error) {
	if f.counts != nil {
		return *f.counts, nil
	}
	return pipeline.DecisionCounts{TotalScenes: 8}, nil
}
func (f *fakeHITLSessionStore) GetSession(_ context.Context, _ string) (*domain.HITLSession, error) {
	return f.session, nil
}
func (f *fakeHITLSessionStore) UpsertSession(_ context.Context, s *domain.HITLSession) error {
	clone := *s
	f.upserts = append(f.upserts, &clone)
	f.session = &clone
	return nil
}
func (f *fakeHITLSessionStore) DeleteSession(_ context.Context, runID string) error {
	f.deletes = append(f.deletes, runID)
	f.session = nil
	return nil
}

func TestRunService_ApproveScenarioReview_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status, scenario_path) VALUES (?, ?, ?, ?, ?)`,
		"scp-049-run-1", "049", string(domain.StageScenarioReview), string(domain.StatusWaiting), "scenario.json"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	sessions := &fakeHITLSessionStore{}
	svc := service.NewRunService(store, nil)
	svc.SetHITLSessionStore(sessions, nil)

	got, err := svc.ApproveScenarioReview(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("ApproveScenarioReview: %v", err)
	}
	if got.Stage != domain.StageCharacterPick {
		t.Fatalf("stage = %q, want character_pick", got.Stage)
	}
	if got.Status != domain.StatusWaiting {
		t.Fatalf("status = %q, want waiting", got.Status)
	}
	if len(sessions.deletes) != 1 || sessions.deletes[0] != "scp-049-run-1" {
		t.Fatalf("delete calls = %+v, want one for scp-049-run-1", sessions.deletes)
	}
	if len(sessions.upserts) != 1 || sessions.upserts[0].Stage != domain.StageCharacterPick {
		t.Fatalf("upsert calls = %+v, want one for character_pick", sessions.upserts)
	}
}

func TestRunService_ApproveScenarioReview_WrongStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status) VALUES (?, ?, ?, ?)`,
		"scp-049-run-1", "049", string(domain.StageWrite), string(domain.StatusFailed)); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	svc := service.NewRunService(store, nil)
	_, err := svc.ApproveScenarioReview(context.Background(), "scp-049-run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestRunService_ApproveScenarioReview_WrongStatus(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status) VALUES (?, ?, ?, ?)`,
		"scp-049-run-1", "049", string(domain.StageScenarioReview), string(domain.StatusRunning)); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	svc := service.NewRunService(store, nil)
	_, err := svc.ApproveScenarioReview(context.Background(), "scp-049-run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestRunService_ApproveScenarioReview_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	_, err := svc.ApproveScenarioReview(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- FinalizeBatchReview --------------------------------------------------

func TestRunService_FinalizeBatchReview_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status, scenario_path) VALUES (?, ?, ?, ?, ?)`,
		"scp-049-run-1", "049", string(domain.StageBatchReview), string(domain.StatusWaiting), "scenario.json"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	sessions := &fakeHITLSessionStore{
		counts: &pipeline.DecisionCounts{Approved: 3, TotalScenes: 3},
	}
	svc := service.NewRunService(store, nil)
	svc.SetHITLSessionStore(sessions, nil)

	got, err := svc.FinalizeBatchReview(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("FinalizeBatchReview: %v", err)
	}
	if got.Stage != domain.StageAssemble {
		t.Fatalf("stage = %q, want assemble", got.Stage)
	}
	if got.Status != domain.StatusWaiting {
		t.Fatalf("status = %q, want waiting (manual gate)", got.Status)
	}
	// Assemble is non-HITL, so the hitl_sessions row must be dropped — no
	// upsert should happen for the new stage.
	if len(sessions.deletes) != 1 || sessions.deletes[0] != "scp-049-run-1" {
		t.Fatalf("delete calls = %+v, want one for scp-049-run-1", sessions.deletes)
	}
	if len(sessions.upserts) != 0 {
		t.Fatalf("upsert calls = %+v, want none (assemble is non-HITL)", sessions.upserts)
	}
}

func TestRunService_FinalizeBatchReview_PendingScenes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status) VALUES (?, ?, ?, ?)`,
		"scp-049-run-1", "049", string(domain.StageBatchReview), string(domain.StatusWaiting)); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	// 3 scenes, only 1 approved → 2 still pending.
	sessions := &fakeHITLSessionStore{
		counts: &pipeline.DecisionCounts{Approved: 1, TotalScenes: 3},
	}
	svc := service.NewRunService(store, nil)
	svc.SetHITLSessionStore(sessions, nil)

	_, err := svc.FinalizeBatchReview(context.Background(), "scp-049-run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for pending scenes, got %v", err)
	}
	// Verify the run was NOT advanced.
	run, err := store.Get(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("re-fetch run: %v", err)
	}
	if run.Stage != domain.StageBatchReview {
		t.Fatalf("stage = %q, want batch_review (transition must abort)", run.Stage)
	}
}

func TestRunService_FinalizeBatchReview_WrongStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status) VALUES (?, ?, ?, ?)`,
		"scp-049-run-1", "049", string(domain.StageScenarioReview), string(domain.StatusWaiting)); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	svc := service.NewRunService(store, nil)
	_, err := svc.FinalizeBatchReview(context.Background(), "scp-049-run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestRunService_FinalizeBatchReview_WrongStatus(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status) VALUES (?, ?, ?, ?)`,
		"scp-049-run-1", "049", string(domain.StageBatchReview), string(domain.StatusRunning)); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	svc := service.NewRunService(store, nil)
	_, err := svc.FinalizeBatchReview(context.Background(), "scp-049-run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestRunService_FinalizeBatchReview_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	_, err := svc.FinalizeBatchReview(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
