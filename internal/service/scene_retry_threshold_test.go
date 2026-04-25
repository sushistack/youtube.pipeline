package service_test

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestSceneService_RecordSceneDecision_RetryExhaustedAtCapBoundary is the
// dedicated regression for the AI-3 unification: before the fix
// RecordSceneDecision used `attempts > MaxSceneRegenAttempts`, so a reject
// that pushed the counter to the cap reported retry_exhausted=false even
// though ListReviewItems and DispatchSceneRegeneration already considered
// the scene exhausted. The boundary value (attempts == cap) is the only
// value that distinguishes `>` from `>=`, so this test pins the corrected
// `>=` semantics into place.
func TestSceneService_RecordSceneDecision_RetryExhaustedAtCapBoundary(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": batchReviewRun("run-1", ""),
	}}
	decisions := &fakeSceneDecisionStore{
		counts:        db.DecisionCounts{TotalScenes: 5},
		regenAttempts: map[int]int{2: service.MaxSceneRegenAttempts},
	}
	svc := service.NewSceneService(runs, &fakeSegmentStore{}, decisions, clock.RealClock{})

	note := "still off-tone after retries"
	result, err := svc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
		RunID:        "run-1",
		SceneIndex:   2,
		DecisionType: domain.DecisionTypeReject,
		Note:         &note,
	})
	if err != nil {
		t.Fatalf("RecordSceneDecision: %v", err)
	}
	testutil.AssertEqual(t, result.RegenAttempts, service.MaxSceneRegenAttempts)
	// Cap-inclusive: at attempts == MaxSceneRegenAttempts the read-model
	// must already report exhausted, so the UI can swap to manual-edit /
	// skip-and-flag CTAs without waiting for the next overflow attempt.
	testutil.AssertEqual(t, result.RetryExhausted, true)
}

// TestSceneService_RetryExhausted_AgreesAcross3Sites is the property test
// requested by Step 3 §12 / R-11 / AI-3: for 100 iterations of `attempts`
// values (covering the cap boundary plus randomized counts on either side),
// ListReviewItems, RecordSceneDecision (reject path), and
// DispatchSceneRegeneration must agree on retry_exhausted. The dispatch
// path additionally enforces the mutation gate (attempts > cap → ErrConflict
// on overflow); the property test validates that the read-model state at
// the cap boundary is already exhausted while the third-attempt mutation
// is the only one rejected.
func TestSceneService_RetryExhausted_AgreesAcross3Sites(t *testing.T) {
	const iterations = 100
	rng := rand.New(rand.NewSource(20260425))

	// Cycle through deterministic boundary-relevant values so cap-1 / cap /
	// cap+1 are always exercised, then top up with randomized counts so the
	// 100-iteration budget actually hits a wide range of attempts values.
	deterministic := []int{
		0,
		1,
		service.MaxSceneRegenAttempts - 1,
		service.MaxSceneRegenAttempts,
		service.MaxSceneRegenAttempts + 1,
		service.MaxSceneRegenAttempts + 5,
	}

	for i := 0; i < iterations; i++ {
		var attempts int
		if i < len(deterministic) {
			attempts = deterministic[i]
		} else {
			attempts = rng.Intn(2*service.MaxSceneRegenAttempts + 6)
		}
		expected := attempts >= service.MaxSceneRegenAttempts

		// ── Site 1: ListReviewItems ─────────────────────────────────────
		const sceneIdx = 0
		listScenarioPath := writeReviewScenarioFixture(t, []domain.NarrationScene{
			{SceneNum: 1, ActID: "act_2", EntityVisible: false, CharactersPresent: []string{"연구원"}},
		})
		listRuns := &fakeRunStore{runs: map[string]*domain.Run{
			"run-list": batchReviewRun("run-list", listScenarioPath),
		}}
		listSegments := &fakeSegmentStore{scenes: []*domain.Episode{
			{SceneIndex: sceneIdx, Narration: narrationPtr("scene"), ReviewStatus: domain.ReviewStatusRejected},
		}}
		listDecisions := &fakeSceneDecisionStore{
			regenAttempts: map[int]int{sceneIdx: attempts},
		}
		listSvc := service.NewSceneService(listRuns, listSegments, listDecisions, clock.RealClock{})

		items, err := listSvc.ListReviewItems(context.Background(), "run-list")
		if err != nil {
			t.Fatalf("iter %d attempts=%d ListReviewItems: %v", i, attempts, err)
		}
		if len(items) != 1 {
			t.Fatalf("iter %d attempts=%d expected 1 review item, got %d", i, attempts, len(items))
		}
		if items[0].RetryExhausted != expected {
			t.Fatalf("iter %d attempts=%d ListReviewItems.RetryExhausted=%v want=%v",
				i, attempts, items[0].RetryExhausted, expected)
		}

		// ── Site 2: RecordSceneDecision (reject path) ────────────────────
		recordRuns := &fakeRunStore{runs: map[string]*domain.Run{
			"run-record": batchReviewRun("run-record", ""),
		}}
		recordDecisions := &fakeSceneDecisionStore{
			counts:        db.DecisionCounts{TotalScenes: 1},
			regenAttempts: map[int]int{sceneIdx: attempts},
		}
		recordSvc := service.NewSceneService(recordRuns, &fakeSegmentStore{}, recordDecisions, clock.RealClock{})

		note := "property iter reject"
		result, err := recordSvc.RecordSceneDecision(context.Background(), service.SceneDecisionInput{
			RunID:        "run-record",
			SceneIndex:   sceneIdx,
			DecisionType: domain.DecisionTypeReject,
			Note:         &note,
		})
		if err != nil {
			t.Fatalf("iter %d attempts=%d RecordSceneDecision: %v", i, attempts, err)
		}
		if result.RetryExhausted != expected {
			t.Fatalf("iter %d attempts=%d RecordSceneDecision.RetryExhausted=%v want=%v",
				i, attempts, result.RetryExhausted, expected)
		}

		// ── Site 3: DispatchSceneRegeneration ────────────────────────────
		dispatchRuns := &fakeRunStore{runs: map[string]*domain.Run{
			"run-dispatch": batchReviewRun("run-dispatch", ""),
		}}
		dispatchDecisions := &fakeSceneDecisionStore{
			counts:        db.DecisionCounts{TotalScenes: 1},
			regenAttempts: map[int]int{sceneIdx: attempts},
		}
		regen := &fakeSceneRegenerator{}
		dispatchSvc := service.NewSceneService(dispatchRuns, &fakeSegmentStore{}, dispatchDecisions, clock.RealClock{})
		dispatchSvc.SetSceneRegenerator(regen)

		regenResult, err := dispatchSvc.DispatchSceneRegeneration(context.Background(), "run-dispatch", sceneIdx)
		if attempts > service.MaxSceneRegenAttempts {
			// AC-2: the regeneration dispatch path still blocks only the
			// overflow attempt. The read-model state is already exhausted,
			// but the mutation gate rejects this with ErrConflict.
			if !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("iter %d attempts=%d expected ErrConflict for overflow dispatch, got err=%v result=%+v",
					i, attempts, err, regenResult)
			}
			if len(regen.dispatches) != 0 {
				t.Fatalf("iter %d attempts=%d regenerator should not run on overflow, got %v",
					i, attempts, regen.dispatches)
			}
			continue
		}
		if err != nil {
			t.Fatalf("iter %d attempts=%d DispatchSceneRegeneration: %v", i, attempts, err)
		}
		if regenResult.RetryExhausted != expected {
			t.Fatalf("iter %d attempts=%d DispatchSceneRegeneration.RetryExhausted=%v want=%v",
				i, attempts, regenResult.RetryExhausted, expected)
		}
	}
}
