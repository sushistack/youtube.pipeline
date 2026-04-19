package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeSegmentStore provides a minimal in-memory segment store for tests.
type fakeSegmentStore struct {
	scenes    []*domain.Episode
	updateErr error
}

type fakeSceneDecisionStore struct {
	decisions          []*domain.Decision
	timelineItems      []*db.TimelineDecision
	timelineCursor     *db.TimelineCursor
	timelineErr        error
	timelineOpts       []db.TimelineListOptions
	counts             db.DecisionCounts
	session            *domain.HITLSession
	recorded           []service.SceneDecisionInput
	recordErr          error
	batchApproveResult *db.BatchApproveResult
	batchApproveErr    error
	batchApproveCalls  []struct {
		RunID              string
		AggregateCommandID string
		FocusSceneIndex    int
	}
	upsertCalls        int
	deleteCalls        int
	upsertSessionErr   error
	deleteSessionErr   error
	listErr            error
	countsErr          error
	getSessionErr      error
	latestUndoable     *db.UndoableDecision
	latestUndoableErr  error
	applyUndoErr       error
	undoSceneIndex     int
	undoDecisionType   string
	undoCommandKind    string
	priorRejection     *db.PriorRejection
	priorRejectionErr  error
	regenAttempts      map[int]int
	regenAttemptsErr   error
	regenAttemptsCalls []int
}

func (f *fakeSegmentStore) ListByRunID(_ context.Context, _ string) ([]*domain.Episode, error) {
	return f.scenes, nil
}

func (f *fakeSegmentStore) UpdateNarration(_ context.Context, _ string, sceneIndex int, narration string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	for _, ep := range f.scenes {
		if ep.SceneIndex == sceneIndex {
			ep.Narration = &narration
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeSceneDecisionStore) ListByRunID(_ context.Context, _ string) ([]*domain.Decision, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.decisions, nil
}

func (f *fakeSceneDecisionStore) ListTimeline(_ context.Context, opts db.TimelineListOptions) ([]*db.TimelineDecision, *db.TimelineCursor, error) {
	if f.timelineErr != nil {
		return nil, nil, f.timelineErr
	}
	f.timelineOpts = append(f.timelineOpts, opts)
	return f.timelineItems, f.timelineCursor, nil
}

func (f *fakeSceneDecisionStore) DecisionCountsByRunID(_ context.Context, _ string) (db.DecisionCounts, error) {
	if f.countsErr != nil {
		return db.DecisionCounts{}, f.countsErr
	}
	return f.counts, nil
}

func (f *fakeSceneDecisionStore) GetSession(_ context.Context, _ string) (*domain.HITLSession, error) {
	if f.getSessionErr != nil {
		return nil, f.getSessionErr
	}
	return f.session, nil
}

func (f *fakeSceneDecisionStore) UpsertSession(_ context.Context, session *domain.HITLSession) error {
	if f.upsertSessionErr != nil {
		return f.upsertSessionErr
	}
	f.upsertCalls++
	f.session = session
	return nil
}

func (f *fakeSceneDecisionStore) DeleteSession(_ context.Context, _ string) error {
	if f.deleteSessionErr != nil {
		return f.deleteSessionErr
	}
	f.deleteCalls++
	return nil
}

func (f *fakeSceneDecisionStore) RecordSceneDecision(_ context.Context, runID string, sceneIndex int, decisionType string, contextSnapshot *string, note *string) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded = append(f.recorded, service.SceneDecisionInput{
		RunID:           runID,
		SceneIndex:      sceneIndex,
		DecisionType:    decisionType,
		ContextSnapshot: contextSnapshot,
		Note:            note,
	})
	return nil
}

func (f *fakeSceneDecisionStore) ApproveAllRemaining(_ context.Context, runID string, aggregateCommandID string, focusSceneIndex int) (*db.BatchApproveResult, error) {
	if f.batchApproveErr != nil {
		return nil, f.batchApproveErr
	}
	f.batchApproveCalls = append(f.batchApproveCalls, struct {
		RunID              string
		AggregateCommandID string
		FocusSceneIndex    int
	}{
		RunID:              runID,
		AggregateCommandID: aggregateCommandID,
		FocusSceneIndex:    focusSceneIndex,
	})
	if f.batchApproveResult == nil {
		return &db.BatchApproveResult{
			AggregateCommandID: aggregateCommandID,
			ApprovedCount:      0,
			ApprovedSceneIDs:   []int{},
			FocusSceneIndex:    focusSceneIndex,
		}, nil
	}
	return f.batchApproveResult, nil
}

func (f *fakeSceneDecisionStore) LatestUndoableDecision(_ context.Context, _ string) (*db.UndoableDecision, error) {
	return f.latestUndoable, f.latestUndoableErr
}

func (f *fakeSceneDecisionStore) ApplyUndo(_ context.Context, _ string, originalID int64) (*db.UndoApplication, error) {
	if f.applyUndoErr != nil {
		return nil, f.applyUndoErr
	}
	return &db.UndoApplication{
		OriginalDecisionID: originalID,
		ReversalDecisionID: originalID + 100,
		SceneIndex:         f.undoSceneIndex,
		DecisionType:       f.undoDecisionType,
		CommandKind:        f.undoCommandKind,
	}, nil
}

func (f *fakeSceneDecisionStore) PriorRejectionForScene(_ context.Context, _ string, _ int) (*db.PriorRejection, error) {
	return f.priorRejection, f.priorRejectionErr
}

func (f *fakeSceneDecisionStore) CountRegenAttempts(_ context.Context, _ string, sceneIndex int) (int, error) {
	if f.regenAttemptsErr != nil {
		return 0, f.regenAttemptsErr
	}
	f.regenAttemptsCalls = append(f.regenAttemptsCalls, sceneIndex)
	if f.regenAttempts == nil {
		return 0, nil
	}
	return f.regenAttempts[sceneIndex], nil
}

func scenarioReviewRun(id string) *domain.Run {
	return &domain.Run{
		ID:     id,
		Stage:  domain.StageScenarioReview,
		Status: domain.StatusWaiting,
	}
}

func batchReviewRun(id string, scenarioPath string) *domain.Run {
	return &domain.Run{
		ID:           id,
		SCPID:        "049",
		Stage:        domain.StageBatchReview,
		Status:       domain.StatusWaiting,
		ScenarioPath: &scenarioPath,
	}
}

func narrationPtr(s string) *string { return &s }

func writeReviewScenarioFixture(t *testing.T, scenes []domain.NarrationScene) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scenario.json")
	payload := map[string]any{
		"narration": map[string]any{
			"scp_id":         "049",
			"title":          "Scenario",
			"scenes":         scenes,
			"metadata":       map[string]any{"language": "ko", "scene_count": len(scenes)},
			"source_version": "v1-llm-writer",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

// ── ListScenes ────────────────────────────────────────────────────────────────

func TestSceneService_ListScenes_ReturnsScenes(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{SceneIndex: 0, Narration: narrationPtr("장면 0")},
		{SceneIndex: 1, Narration: narrationPtr("장면 1")},
	}}
	svc := service.NewSceneService(runs, segments, nil, clock.RealClock{})

	scenes, err := svc.ListScenes(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertEqual(t, len(scenes), 2)
	testutil.AssertEqual(t, *scenes[0].Narration, "장면 0")
}

func TestSceneService_ListScenes_ReturnsConflictWhenNotAtScenarioReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": {ID: "run-1", Stage: domain.StageResearch, Status: domain.StatusRunning},
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, nil, clock.RealClock{})

	_, err := svc.ListScenes(context.Background(), "run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestSceneService_ListScenes_ReturnsNotFoundForMissingRun(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, nil, clock.RealClock{})

	_, err := svc.ListScenes(context.Background(), "no-such")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSceneService_ListReviewItems_ComputesHighLeverageAndQueueOrdering(t *testing.T) {
	scenarioPath := writeReviewScenarioFixture(t, []domain.NarrationScene{
		{SceneNum: 1, ActID: "act_hook", EntityVisible: true, CharactersPresent: []string{"연구원"}},
		{SceneNum: 2, ActID: "act_hook", EntityVisible: false, CharactersPresent: []string{"연구원"}},
		{SceneNum: 3, ActID: "act_2", EntityVisible: false, CharactersPresent: []string{"연구원"}},
	})
	criticSub := `{"hook_strength":0.91,"fact_accuracy":0.88,"emotional_variation":0.64,"immersion":0.45}`
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", scenarioPath),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{SceneIndex: 1, Narration: narrationPtr("regular waiting"), ReviewStatus: domain.ReviewStatusWaitingForReview},
		{SceneIndex: 0, Narration: narrationPtr("hook scene"), ReviewStatus: domain.ReviewStatusWaitingForReview, CriticScore: floatPtr(0.82), CriticSub: &criticSub},
		{SceneIndex: 2, Narration: narrationPtr("approved act boundary"), ReviewStatus: domain.ReviewStatusApproved},
	}}
	svc := service.NewSceneService(runs, segments, nil, clock.RealClock{})

	items, err := svc.ListReviewItems(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertEqual(t, len(items), 3)
	testutil.AssertEqual(t, items[0].SceneIndex, 0)
	testutil.AssertEqual(t, items[0].HighLeverage, true)
	testutil.AssertEqual(t, items[0].HighLeverageReasonCode, "first_appearance")
	testutil.AssertEqual(t, items[0].HighLeverageReason, "First appearance of SCP-049; Opening hook scene")
	if items[0].CriticBreakdown == nil || items[0].CriticBreakdown.HookStrength == nil {
		t.Fatalf("expected critic breakdown to parse")
	}
	testutil.AssertEqual(t, int(*items[0].CriticBreakdown.HookStrength), 91)
	testutil.AssertEqual(t, items[1].SceneIndex, 1)
	testutil.AssertEqual(t, items[2].SceneIndex, 2)
	testutil.AssertEqual(t, items[2].HighLeverage, true)
	testutil.AssertEqual(t, items[2].HighLeverageReason, "Act boundary: act_2")
}

func TestSceneService_ListReviewItems_RetryExhaustedAtCapBoundary(t *testing.T) {
	// AC-4 boundary: when a scene has reached MaxSceneRegenAttempts (=2),
	// ListReviewItems must report retry_exhausted=true so the batch-review
	// UI can swap the action bar for the manual-edit / skip-and-flag CTAs.
	// Regression guard against the `>` vs `>=` inconsistency — if someone
	// flips this to `>`, a cap-reached scene would render as still-retryable.
	scenarioPath := writeReviewScenarioFixture(t, []domain.NarrationScene{
		{SceneNum: 1, ActID: "act_2", EntityVisible: false, CharactersPresent: []string{"연구원"}},
		{SceneNum: 2, ActID: "act_2", EntityVisible: false, CharactersPresent: []string{"연구원"}},
	})
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", scenarioPath),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{SceneIndex: 0, Narration: narrationPtr("one retry done"), ReviewStatus: domain.ReviewStatusRejected},
		{SceneIndex: 1, Narration: narrationPtr("cap reached"), ReviewStatus: domain.ReviewStatusRejected},
	}}
	decisions := &fakeSceneDecisionStore{
		regenAttempts: map[int]int{
			0: 1, // one prior retry — not yet exhausted
			1: 2, // cap reached — exhausted at the boundary
		},
	}
	svc := service.NewSceneService(runs, segments, decisions, clock.RealClock{})

	items, err := svc.ListReviewItems(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertEqual(t, len(items), 2)

	byIndex := map[int]*service.ReviewItem{}
	for _, item := range items {
		byIndex[item.SceneIndex] = item
	}
	testutil.AssertEqual(t, byIndex[0].RegenAttempts, 1)
	testutil.AssertEqual(t, byIndex[0].RetryExhausted, false)
	testutil.AssertEqual(t, byIndex[1].RegenAttempts, 2)
	testutil.AssertEqual(t, byIndex[1].RetryExhausted, true)
}

func TestSceneService_ListDecisionsTimeline_MapsSnapshotReasonAndCursor(t *testing.T) {
	decisionType := domain.DecisionTypeReject
	beforeCreatedAt := "2026-04-18T10:00:00Z"
	beforeID := int64(77)
	store := &fakeSceneDecisionStore{
		timelineItems: []*db.TimelineDecision{
			{
				ID:              88,
				RunID:           "run-1",
				SCPID:           "scp-049",
				DecisionType:    decisionType,
				ContextSnapshot: narrationPtr(`{"reason":"Needs clearer medical framing"}`),
				CreatedAt:       "2026-04-18T10:05:00Z",
			},
		},
		timelineCursor: &db.TimelineCursor{
			BeforeCreatedAt: beforeCreatedAt,
			BeforeID:        beforeID,
		},
	}
	svc := service.NewSceneService(nil, &fakeSegmentStore{}, store, clock.RealClock{})

	result, err := svc.ListDecisionsTimeline(context.Background(), service.TimelineListInput{
		DecisionType:    &decisionType,
		Limit:           250,
		BeforeCreatedAt: &beforeCreatedAt,
		BeforeID:        &beforeID,
	})
	if err != nil {
		t.Fatalf("ListDecisionsTimeline: %v", err)
	}
	testutil.AssertEqual(t, len(result.Items), 1)
	testutil.AssertEqual(t, result.Items[0].SCPID, "scp-049")
	if result.Items[0].ReasonFromSnapshot == nil {
		t.Fatalf("expected parsed snapshot reason")
	}
	testutil.AssertEqual(t, *result.Items[0].ReasonFromSnapshot, "Needs clearer medical framing")
	if result.NextCursor == nil {
		t.Fatalf("expected next cursor")
	}
	testutil.AssertEqual(t, result.NextCursor.BeforeCreatedAt, beforeCreatedAt)
	testutil.AssertEqual(t, result.NextCursor.BeforeID, beforeID)
	testutil.AssertEqual(t, len(store.timelineOpts), 1)
	testutil.AssertEqual(t, store.timelineOpts[0].Limit, 250)
}

func TestSceneService_ListDecisionsTimeline_UsesJoinedScpIDFromStore(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id) VALUES ('run-1', 'scp-049');
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at)
		VALUES (1, 'run-1', '0', 'approve', '2026-04-18T10:00:00Z')`,
	); err != nil {
		t.Fatalf("seed timeline decision: %v", err)
	}

	svc := service.NewSceneService(
		nil,
		&fakeSegmentStore{},
		db.NewDecisionStore(database),
		clock.RealClock{},
	)

	result, err := svc.ListDecisionsTimeline(context.Background(), service.TimelineListInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListDecisionsTimeline: %v", err)
	}
	testutil.AssertEqual(t, len(result.Items), 1)
	testutil.AssertEqual(t, result.Items[0].SCPID, "scp-049")
}

func TestSceneService_ListReviewItems_ReturnsConflictOutsideBatchReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, nil, clock.RealClock{})

	_, err := svc.ListReviewItems(context.Background(), "run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

// ── EditNarration ─────────────────────────────────────────────────────────────

func TestSceneService_EditNarration_UpdatesText(t *testing.T) {
	original := "원래 나레이션"
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{SceneIndex: 0, Narration: &original},
	}}
	svc := service.NewSceneService(runs, segments, nil, clock.RealClock{})

	err := svc.EditNarration(context.Background(), "run-1", 0, "새 나레이션")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertEqual(t, *segments.scenes[0].Narration, "새 나레이션")
}

func TestSceneService_EditNarration_ReturnsValidationErrorForEmptyNarration(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, nil, clock.RealClock{})

	err := svc.EditNarration(context.Background(), "run-1", 0, "")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestSceneService_EditNarration_ReturnsConflictWhenNotAtScenarioReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": {ID: "run-1", Stage: domain.StageWrite, Status: domain.StatusRunning},
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, nil, clock.RealClock{})

	err := svc.EditNarration(context.Background(), "run-1", 0, "text")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestSceneService_EditNarration_ReturnsNotFoundForMissingScene(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{}} // no scene index 0
	svc := service.NewSceneService(runs, segments, nil, clock.RealClock{})

	err := svc.EditNarration(context.Background(), "run-1", 0, "text")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSceneService_RecordSceneDecision_UpsertsSessionAfterApprove(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	decisions := &fakeSceneDecisionStore{
		decisions: []*domain.Decision{
			{RunID: "run-1", SceneID: narrationPtr("0"), DecisionType: domain.DecisionTypeApprove},
		},
		counts: db.DecisionCounts{Approved: 1, Rejected: 0, TotalScenes: 3},
	}
	clk := clock.NewFakeClock(time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC))
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clk)

	result, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   0,
		DecisionType: domain.DecisionTypeApprove,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions.recorded) != 1 {
		t.Fatalf("expected one recorded decision, got %d", len(decisions.recorded))
	}
	testutil.AssertEqual(t, decisions.upsertCalls, 1)
	testutil.AssertEqual(t, result.NextSceneIndex, 1)
	if decisions.session == nil {
		t.Fatalf("expected session upsert")
	}
	testutil.AssertEqual(t, decisions.session.LastInteractionTimestamp, "2026-04-19T12:00:00Z")
}

func TestSceneService_RecordSceneDecision_RejectsSkipWithoutSnapshot(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, &fakeSceneDecisionStore{}, clock.RealClock{})

	_, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   1,
		DecisionType: domain.DecisionTypeSkipAndRemember,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestSceneService_RecordSceneDecision_BuildsSkipSnapshotFromServerState(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	criticScore := 0.72
	criticSub := `{"hook_strength": 0.40, "fact_accuracy": 0.80, "emotional_variation": 0.55, "immersion": 0.60}`
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{
			RunID:          "run-1",
			SceneIndex:     2,
			ReviewStatus:   domain.ReviewStatusWaitingForReview,
			SafeguardFlags: []string{domain.SafeguardFlagMinors},
			CriticScore:    &criticScore,
			CriticSub:      &criticSub,
		},
	}}
	decisions := &fakeSceneDecisionStore{
		counts: db.DecisionCounts{Approved: 0, Rejected: 0, TotalScenes: 3},
	}
	svc := service.NewSceneService(runs, segments, decisions, clock.RealClock{})

	clientLie := `{"scene_index": 99, "content_flags": ["not-a-real-flag"], "critic_score": 9.99}`
	_, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:           "run-1",
		SceneIndex:      2,
		DecisionType:    domain.DecisionTypeSkipAndRemember,
		ContextSnapshot: &clientLie,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions.recorded) != 1 {
		t.Fatalf("expected one recorded decision, got %d", len(decisions.recorded))
	}
	if decisions.recorded[0].ContextSnapshot == nil {
		t.Fatalf("expected server-built context_snapshot, got nil")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(*decisions.recorded[0].ContextSnapshot), &payload); err != nil {
		t.Fatalf("stored snapshot is not valid JSON: %v", err)
	}
	// Server must ignore the client-sent `scene_index: 99` and the
	// forged content_flags / critic_score values.
	testutil.AssertEqual(t, payload["scene_index"].(float64), float64(2))
	testutil.AssertEqual(t, payload["action_source"].(string), "batch_review")
	testutil.AssertEqual(t, payload["review_status_before"].(string), string(domain.ReviewStatusWaitingForReview))
	flags, ok := payload["content_flags"].([]any)
	if !ok || len(flags) != 1 || flags[0].(string) != domain.SafeguardFlagMinors {
		t.Fatalf("content_flags should come from safeguard_flags, got %v", payload["content_flags"])
	}
	score, ok := payload["critic_score"].(float64)
	if !ok || score != 72 {
		t.Fatalf("critic_score should be server-normalized to 72, got %v", payload["critic_score"])
	}
}

func TestSceneService_RecordSceneDecision_ReturnsConflictOutsideBatchReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, &fakeSceneDecisionStore{}, clock.RealClock{})

	note := "stage guard check"
	_, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   0,
		DecisionType: domain.DecisionTypeReject,
		Note:         &note,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func floatPtr(value float64) *float64 { return &value }

// ── RecordSceneDecision (reject/FR53/retry) ──────────────────────────────────

type fakeSceneRegenerator struct {
	dispatches []int
	err        error
}

func (f *fakeSceneRegenerator) DispatchRegeneration(_ context.Context, _ string, sceneIndex int) error {
	if f.err != nil {
		return f.err
	}
	f.dispatches = append(f.dispatches, sceneIndex)
	return nil
}

func TestSceneService_RecordSceneDecision_RejectRequiresNote(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, &fakeSceneDecisionStore{}, clock.RealClock{})

	_, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   0,
		DecisionType: domain.DecisionTypeReject,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation for reject without note, got %v", err)
	}

	blank := "   "
	_, err = svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   0,
		DecisionType: domain.DecisionTypeReject,
		Note:         &blank,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation for reject with blank note, got %v", err)
	}
}

func TestSceneService_RecordSceneDecision_RejectReturnsFR53Warning(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	decisions := &fakeSceneDecisionStore{
		counts: db.DecisionCounts{TotalScenes: 5},
		priorRejection: &db.PriorRejection{
			RunID:      "prior-run",
			SCPID:      "049",
			SceneIndex: 2,
			Reason:     "tone too sarcastic",
			CreatedAt:  "2026-03-15T11:00:00Z",
		},
		regenAttempts: map[int]int{2: 1},
	}
	clk := clock.NewFakeClock(time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC))
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clk)

	note := "still off-tone"
	result, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   2,
		DecisionType: domain.DecisionTypeReject,
		Note:         &note,
	})
	if err != nil {
		t.Fatalf("RecordSceneDecision: %v", err)
	}
	if result.PriorRejection == nil {
		t.Fatalf("expected prior_rejection in result")
	}
	testutil.AssertEqual(t, result.PriorRejection.RunID, "prior-run")
	testutil.AssertEqual(t, result.PriorRejection.Reason, "tone too sarcastic")
	testutil.AssertEqual(t, result.RegenAttempts, 1)
	testutil.AssertEqual(t, result.RetryExhausted, false)
}

func TestSceneService_RecordSceneDecision_RejectWithoutPriorOmitsWarning(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	decisions := &fakeSceneDecisionStore{
		counts:         db.DecisionCounts{TotalScenes: 5},
		priorRejection: nil,
	}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})

	note := "dialogue missing"
	result, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   0,
		DecisionType: domain.DecisionTypeReject,
		Note:         &note,
	})
	if err != nil {
		t.Fatalf("RecordSceneDecision: %v", err)
	}
	if result.PriorRejection != nil {
		t.Fatalf("expected no prior rejection warning, got %+v", result.PriorRejection)
	}
}

func TestSceneService_DispatchSceneRegeneration_DispatchesUnderCap(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	decisions := &fakeSceneDecisionStore{
		counts:        db.DecisionCounts{TotalScenes: 5},
		regenAttempts: map[int]int{3: 1},
	}
	regen := &fakeSceneRegenerator{}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})
	svc.SetSceneRegenerator(regen)

	result, err := svc.DispatchSceneRegeneration(context.Background(), "run-1", 3)
	if err != nil {
		t.Fatalf("DispatchSceneRegeneration: %v", err)
	}
	if len(regen.dispatches) != 1 || regen.dispatches[0] != 3 {
		t.Fatalf("expected single dispatch for scene 3, got %v", regen.dispatches)
	}
	testutil.AssertEqual(t, result.SceneIndex, 3)
	testutil.AssertEqual(t, result.RegenAttempts, 1)
	testutil.AssertEqual(t, result.RetryExhausted, false)
}

func TestSceneService_DispatchSceneRegeneration_BlocksThirdAttempt(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	decisions := &fakeSceneDecisionStore{
		counts:        db.DecisionCounts{TotalScenes: 5},
		regenAttempts: map[int]int{3: 3},
	}
	regen := &fakeSceneRegenerator{}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})
	svc.SetSceneRegenerator(regen)

	_, err := svc.DispatchSceneRegeneration(context.Background(), "run-1", 3)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict for 3rd attempt, got %v", err)
	}
	if len(regen.dispatches) != 0 {
		t.Fatalf("regenerator should not have been invoked, got %v", regen.dispatches)
	}
}

func TestSceneService_DispatchSceneRegeneration_ReturnsConflictOutsideBatchReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	decisions := &fakeSceneDecisionStore{counts: db.DecisionCounts{TotalScenes: 1}}
	regen := &fakeSceneRegenerator{}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})
	svc.SetSceneRegenerator(regen)

	_, err := svc.DispatchSceneRegeneration(context.Background(), "run-1", 0)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict outside batch_review, got %v", err)
	}
}

func TestSceneService_DispatchSceneRegeneration_RejectsWhenDispatcherFails(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	decisions := &fakeSceneDecisionStore{
		counts:        db.DecisionCounts{TotalScenes: 5},
		regenAttempts: map[int]int{0: 1},
	}
	regen := &fakeSceneRegenerator{err: errors.New("dispatch failed")}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})
	svc.SetSceneRegenerator(regen)

	_, err := svc.DispatchSceneRegeneration(context.Background(), "run-1", 0)
	if err == nil {
		t.Fatal("expected dispatcher error to surface")
	}
}

func TestSceneService_ApproveAllRemaining_StoresAggregateBatchAndRefreshesSession(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2026, 4, 19, 12, 34, 56, 0, time.UTC))
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", writeReviewScenarioFixture(t, []domain.NarrationScene{
			{SceneNum: 1},
			{SceneNum: 2},
		})),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{SceneIndex: 0, ReviewStatus: domain.ReviewStatusWaitingForReview},
		{SceneIndex: 1, ReviewStatus: domain.ReviewStatusWaitingForReview},
	}}
	decisions := &fakeSceneDecisionStore{
		counts: db.DecisionCounts{Approved: 2, Rejected: 0, TotalScenes: 2},
		batchApproveResult: &db.BatchApproveResult{
			AggregateCommandID: "run-1-batch-approve-123",
			ApprovedCount:      2,
			ApprovedSceneIDs:   []int{0, 1},
			FocusSceneIndex:    1,
		},
	}
	svc := service.NewSceneService(runs, segments, decisions, clk)

	result, err := svc.ApproveAllRemaining(context.Background(), service.BatchApproveAllRemainingInput{
		RunID:           "run-1",
		FocusSceneIndex: 1,
	})
	if err != nil {
		t.Fatalf("ApproveAllRemaining: %v", err)
	}

	testutil.AssertEqual(t, result.AggregateCommandID, "run-1-batch-approve-123")
	testutil.AssertEqual(t, result.ApprovedCount, 2)
	testutil.AssertEqual(t, result.FocusSceneIndex, 1)
	testutil.AssertEqual(t, decisions.upsertCalls, 1)
	if len(decisions.batchApproveCalls) != 1 {
		t.Fatalf("expected one batch approve call, got %d", len(decisions.batchApproveCalls))
	}
	testutil.AssertEqual(t, decisions.batchApproveCalls[0].RunID, "run-1")
	testutil.AssertEqual(t, decisions.batchApproveCalls[0].FocusSceneIndex, 1)
	if decisions.batchApproveCalls[0].AggregateCommandID == "" {
		t.Fatal("expected aggregate command id to be generated")
	}
}

func TestSceneService_ApproveAllRemaining_ReturnsConflictOutsideBatchReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, &fakeSceneDecisionStore{}, clock.RealClock{})

	_, err := svc.ApproveAllRemaining(context.Background(), service.BatchApproveAllRemainingInput{
		RunID:           "run-1",
		FocusSceneIndex: 0,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

// ── UndoLastDecision ──────────────────────────────────────────────────────────

func TestSceneService_UndoLastDecision_ReturnsConflictInPhaseC(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageAssemble, Status: domain.StatusRunning},
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, &fakeSceneDecisionStore{}, clock.RealClock{})
	_, err := svc.UndoLastDecision(context.Background(), "r1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict for Phase C run, got %v", err)
	}
}

func TestSceneService_UndoLastDecision_ReturnsConflictWhenStackEmpty(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageBatchReview, Status: domain.StatusWaiting},
	}}
	decisions := &fakeSceneDecisionStore{latestUndoable: nil}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})
	_, err := svc.UndoLastDecision(context.Background(), "r1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict for empty stack, got %v", err)
	}
}

func TestSceneService_UndoLastDecision_UndoesApproveDecision(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageBatchReview, Status: domain.StatusWaiting},
	}}
	decisions := &fakeSceneDecisionStore{
		latestUndoable: &db.UndoableDecision{
			ID:           42,
			RunID:        "r1",
			DecisionType: domain.DecisionTypeApprove,
		},
		undoSceneIndex:   3,
		undoDecisionType: domain.DecisionTypeApprove,
		counts:           db.DecisionCounts{Approved: 1, TotalScenes: 5},
	}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})
	result, err := svc.UndoLastDecision(context.Background(), "r1")
	if err != nil {
		t.Fatalf("UndoLastDecision: %v", err)
	}
	if result.UndoneSceneIndex != 3 {
		t.Fatalf("want UndoneSceneIndex=3, got %d", result.UndoneSceneIndex)
	}
	if result.UndoneKind != domain.CommandKindApprove {
		t.Fatalf("want kind=%q, got %q", domain.CommandKindApprove, result.UndoneKind)
	}
	if result.FocusTarget != "scene-card" {
		t.Fatalf("want focus_target=scene-card, got %q", result.FocusTarget)
	}
}

func TestSceneService_UndoLastDecision_DescriptorEditHasFocusDescriptor(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageCharacterPick, Status: domain.StatusWaiting},
	}}
	decisions := &fakeSceneDecisionStore{
		latestUndoable: &db.UndoableDecision{
			ID:           10,
			RunID:        "r1",
			DecisionType: domain.DecisionTypeDescriptorEdit,
		},
		undoSceneIndex:   -1,
		undoDecisionType: domain.DecisionTypeDescriptorEdit,
	}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})
	result, err := svc.UndoLastDecision(context.Background(), "r1")
	if err != nil {
		t.Fatalf("UndoLastDecision: %v", err)
	}
	if result.FocusTarget != "descriptor" {
		t.Fatalf("want focus_target=descriptor, got %q", result.FocusTarget)
	}
	if result.UndoneKind != domain.CommandKindDescriptorEdit {
		t.Fatalf("want kind=%q, got %q", domain.CommandKindDescriptorEdit, result.UndoneKind)
	}
}

func TestSceneService_UndoLastDecision_UsesAggregateCommandKindWhenProvided(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageBatchReview, Status: domain.StatusWaiting},
	}}
	decisions := &fakeSceneDecisionStore{
		latestUndoable: &db.UndoableDecision{
			ID:           42,
			RunID:        "r1",
			DecisionType: domain.DecisionTypeApprove,
		},
		undoSceneIndex:   2,
		undoDecisionType: domain.DecisionTypeApprove,
		undoCommandKind:  domain.CommandKindApproveAllRemaining,
	}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})

	result, err := svc.UndoLastDecision(context.Background(), "r1")
	if err != nil {
		t.Fatalf("UndoLastDecision: %v", err)
	}
	if result.UndoneKind != domain.CommandKindApproveAllRemaining {
		t.Fatalf("want kind=%q, got %q", domain.CommandKindApproveAllRemaining, result.UndoneKind)
	}
}
