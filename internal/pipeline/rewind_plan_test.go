package pipeline_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

func TestStageNodeKey_IsRewindable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  pipeline.StageNodeKey
		want bool
	}{
		{pipeline.StageNodeScenario, true},
		{pipeline.StageNodeCharacter, true},
		{pipeline.StageNodeAssets, true},
		{pipeline.StageNodeAssemble, true},
		{pipeline.StageNodeKey("pending"), false},
		{pipeline.StageNodeKey("complete"), false},
		{pipeline.StageNodeKey(""), false},
		{pipeline.StageNodeKey("nonsense"), false},
	}
	for _, tc := range cases {
		if got := tc.key.IsRewindable(); got != tc.want {
			t.Errorf("IsRewindable(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestRewindTarget_Mapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key      pipeline.StageNodeKey
		want     domain.Stage
		wantErr  bool
		errIsVal bool
	}{
		{pipeline.StageNodeScenario, domain.StageResearch, false, false},
		{pipeline.StageNodeCharacter, domain.StageCharacterPick, false, false},
		{pipeline.StageNodeAssets, domain.StageImage, false, false},
		{pipeline.StageNodeAssemble, domain.StageAssemble, false, false},
		{pipeline.StageNodeKey("pending"), "", true, true},
		{pipeline.StageNodeKey("complete"), "", true, true},
		{pipeline.StageNodeKey(""), "", true, true},
	}
	for _, tc := range cases {
		got, err := pipeline.RewindTarget(tc.key)
		if tc.wantErr {
			if err == nil {
				t.Errorf("RewindTarget(%q): expected error, got nil", tc.key)
				continue
			}
			if tc.errIsVal && !errors.Is(err, domain.ErrValidation) {
				t.Errorf("RewindTarget(%q): want ErrValidation, got %v", tc.key, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("RewindTarget(%q): unexpected error %v", tc.key, err)
			continue
		}
		if got != tc.want {
			t.Errorf("RewindTarget(%q) = %s, want %s", tc.key, got, tc.want)
		}
	}
}

// TestCanRewind_StageOrdering pins the rewind authorization gate. The server
// must allow rewind only when the target's entry stage strictly precedes the
// current run stage in pipeline order. Equal or later targets are rejected
// with ErrConflict so a manipulated frontend cannot bypass the rule.
func TestCanRewind_StageOrdering(t *testing.T) {
	t.Parallel()
	type tc struct {
		current domain.Stage
		node    pipeline.StageNodeKey
		want    domain.Stage
		wantErr bool
	}
	cases := []tc{
		// At metadata_ack, every node before assemble is allowed; assemble itself is allowed too.
		{domain.StageMetadataAck, pipeline.StageNodeScenario, domain.StageResearch, false},
		{domain.StageMetadataAck, pipeline.StageNodeCharacter, domain.StageCharacterPick, false},
		{domain.StageMetadataAck, pipeline.StageNodeAssets, domain.StageImage, false},
		{domain.StageMetadataAck, pipeline.StageNodeAssemble, domain.StageAssemble, false},
		// At image, only scenario+character are valid (assets entry == image, not strictly before).
		{domain.StageImage, pipeline.StageNodeScenario, domain.StageResearch, false},
		{domain.StageImage, pipeline.StageNodeCharacter, domain.StageCharacterPick, false},
		{domain.StageImage, pipeline.StageNodeAssets, "", true},
		{domain.StageImage, pipeline.StageNodeAssemble, "", true},
		// At character_pick, only scenario is valid.
		{domain.StageCharacterPick, pipeline.StageNodeScenario, domain.StageResearch, false},
		{domain.StageCharacterPick, pipeline.StageNodeCharacter, "", true},
		{domain.StageCharacterPick, pipeline.StageNodeAssets, "", true},
		// At pending nothing is rewindable (the run hasn't progressed past anything).
		{domain.StagePending, pipeline.StageNodeScenario, "", true},
		// At complete every work-phase node is rewindable.
		{domain.StageComplete, pipeline.StageNodeAssemble, domain.StageAssemble, false},
	}
	for _, c := range cases {
		got, err := pipeline.CanRewind(c.current, c.node)
		if c.wantErr {
			if err == nil {
				t.Errorf("CanRewind(%s, %s): expected error, got %s", c.current, c.node, got)
				continue
			}
			if !errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrValidation) {
				t.Errorf("CanRewind(%s, %s): want ErrConflict or ErrValidation, got %v", c.current, c.node, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("CanRewind(%s, %s): unexpected error %v", c.current, c.node, err)
			continue
		}
		if got != c.want {
			t.Errorf("CanRewind(%s, %s) = %s, want %s", c.current, c.node, got, c.want)
		}
	}
}

func TestPlanRewind_Scenario(t *testing.T) {
	t.Parallel()
	plan, err := pipeline.PlanRewind(pipeline.StageNodeScenario)
	if err != nil {
		t.Fatalf("PlanRewind(scenario): %v", err)
	}
	if plan.Target != domain.StageResearch {
		t.Errorf("Target = %s, want research", plan.Target)
	}
	if plan.FinalStage != domain.StagePending || plan.FinalStatus != domain.StatusPending {
		t.Errorf("Final = %s/%s, want pending/pending", plan.FinalStage, plan.FinalStatus)
	}
	if !plan.DeleteSegments {
		t.Errorf("DeleteSegments must be true for scenario rewind")
	}
	if !plan.ClearScenarioPath || !plan.ClearCharacterPick || !plan.ClearOutputPath || !plan.ClearCriticScore {
		t.Errorf("scenario rewind must clear every per-run pointer: got %+v", plan)
	}
	for _, fs := range []bool{plan.FSRemoveScenario, plan.FSRemoveImages, plan.FSRemoveTTS, plan.FSRemoveClips, plan.FSRemoveOutputMP4, plan.FSRemoveMetadata, plan.FSRemoveManifest} {
		if !fs {
			t.Errorf("scenario rewind must remove every artifact subtree: %+v", plan)
			break
		}
	}
	if !plan.FSRemoveCacheDir {
		t.Errorf("scenario rewind must set FSRemoveCacheDir (wipes _cache/ before Phase A re-runs): %+v", plan)
	}
	if !plan.FSRemoveTracesDir {
		t.Errorf("scenario rewind must set FSRemoveTracesDir (stale traces irrelevant after full restart): %+v", plan)
	}
	// All known decision types must be deleted (rewind to research wipes
	// everything from research onward).
	if len(plan.DecisionTypesToDelete) == 0 {
		t.Errorf("scenario rewind must delete every decision type, got empty")
	}
}

func TestPlanRewind_Character(t *testing.T) {
	t.Parallel()
	plan, err := pipeline.PlanRewind(pipeline.StageNodeCharacter)
	if err != nil {
		t.Fatalf("PlanRewind(character): %v", err)
	}
	if plan.FinalStage != domain.StageCharacterPick || plan.FinalStatus != domain.StatusWaiting {
		t.Errorf("Final = %s/%s, want character_pick/waiting", plan.FinalStage, plan.FinalStatus)
	}
	if plan.DeleteSegments {
		t.Errorf("character rewind must NOT delete segments (Phase A narration is reused)")
	}
	if !plan.ClearImageArtifacts || !plan.ClearTTSArtifacts || !plan.ClearClipPaths {
		t.Errorf("character rewind must clear all phase B/C artifact fields: %+v", plan)
	}
	if plan.ClearScenarioPath {
		t.Errorf("character rewind must preserve scenario_path (Phase A succeeded)")
	}
	if !plan.ClearCharacterPick {
		t.Errorf("character rewind must clear the character pick: %+v", plan)
	}
	if plan.FSRemoveScenario {
		t.Errorf("character rewind must NOT remove scenario.json")
	}
	// descriptor_edit (bucket = character_pick) and approve/reject etc.
	// (bucket = batch_review) all qualify for deletion.
	expected := map[string]bool{
		domain.DecisionTypeDescriptorEdit:     true,
		domain.DecisionTypeApprove:            true,
		domain.DecisionTypeReject:             true,
		domain.DecisionTypeSkipAndRemember:    true,
		domain.DecisionTypeSystemAutoApproved: true,
		domain.DecisionTypeOverride:           true,
		domain.DecisionTypeUndo:               true,
	}
	got := map[string]bool{}
	for _, t := range plan.DecisionTypesToDelete {
		got[t] = true
	}
	for k := range expected {
		if !got[k] {
			t.Errorf("character rewind decision deletion missing %q (got %+v)", k, plan.DecisionTypesToDelete)
		}
	}
}

func TestPlanRewind_Assets(t *testing.T) {
	t.Parallel()
	plan, err := pipeline.PlanRewind(pipeline.StageNodeAssets)
	if err != nil {
		t.Fatalf("PlanRewind(assets): %v", err)
	}
	if plan.FinalStage != domain.StageImage || plan.FinalStatus != domain.StatusWaiting {
		t.Errorf("Final = %s/%s, want image/waiting", plan.FinalStage, plan.FinalStatus)
	}
	if plan.DeleteSegments {
		t.Errorf("assets rewind must keep segment rows (only artifact fields cleared)")
	}
	if plan.ClearScenarioPath {
		t.Errorf("assets rewind must preserve scenario_path")
	}
	if plan.ClearCharacterPick {
		t.Errorf("assets rewind must preserve character pick")
	}
	if !plan.ClearImageArtifacts || !plan.ClearTTSArtifacts || !plan.ClearClipPaths {
		t.Errorf("assets rewind must clear image/tts/clip artifact fields: %+v", plan)
	}
	// descriptor_edit (bucket = character_pick, BEFORE image) must be PRESERVED.
	for _, ty := range plan.DecisionTypesToDelete {
		if ty == domain.DecisionTypeDescriptorEdit {
			t.Errorf("assets rewind must NOT delete descriptor_edit: %+v", plan.DecisionTypesToDelete)
		}
	}
	// Review-bucket types must be DELETED.
	hasApprove := false
	for _, ty := range plan.DecisionTypesToDelete {
		if ty == domain.DecisionTypeApprove {
			hasApprove = true
		}
	}
	if !hasApprove {
		t.Errorf("assets rewind must delete batch-review approve decisions: %+v", plan.DecisionTypesToDelete)
	}
}

func TestPlanRewind_Assemble(t *testing.T) {
	t.Parallel()
	plan, err := pipeline.PlanRewind(pipeline.StageNodeAssemble)
	if err != nil {
		t.Fatalf("PlanRewind(assemble): %v", err)
	}
	if plan.FinalStage != domain.StageAssemble || plan.FinalStatus != domain.StatusWaiting {
		t.Errorf("Final = %s/%s, want assemble/waiting", plan.FinalStage, plan.FinalStatus)
	}
	if plan.ClearImageArtifacts || plan.ClearTTSArtifacts {
		t.Errorf("assemble rewind must preserve image and TTS (Phase C inputs): %+v", plan)
	}
	if !plan.ClearClipPaths {
		t.Errorf("assemble rewind must clear clip paths: %+v", plan)
	}
	if plan.FSRemoveImages || plan.FSRemoveTTS || plan.FSRemoveScenario {
		t.Errorf("assemble rewind must keep scenario/images/tts on disk: %+v", plan)
	}
	if !plan.FSRemoveClips || !plan.FSRemoveOutputMP4 || !plan.FSRemoveMetadata || !plan.FSRemoveManifest {
		t.Errorf("assemble rewind must remove clips/output/metadata/manifest: %+v", plan)
	}
	// Nothing should be deleted from decisions (descriptor_edit is at
	// character_pick=8, batch-review approve is at batch_review=11; both
	// strictly before assemble=12).
	if len(plan.DecisionTypesToDelete) != 0 {
		t.Errorf("assemble rewind must keep all existing decisions, got delete-list %v", plan.DecisionTypesToDelete)
	}
}

func TestPlanRewind_InvalidNode(t *testing.T) {
	t.Parallel()
	_, err := pipeline.PlanRewind(pipeline.StageNodeKey("complete"))
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("PlanRewind(complete): want ErrValidation, got %v", err)
	}
}

// TestRewind_StageOrderMatchesAllStages pins the assumption that AllStages()
// is the single source of truth for pipeline ordering. If the array gets
// reordered, this test fails loudly instead of allowing rewind to silently
// permit invalid transitions.
func TestRewind_StageOrderMatchesAllStages(t *testing.T) {
	t.Parallel()
	all := domain.AllStages()
	// Verify a known-correct ordering: research(1) < character_pick(8) <
	// image(9) < batch_review(11) < assemble(12).
	idx := func(s domain.Stage) int {
		for i, x := range all {
			if x == s {
				return i
			}
		}
		t.Fatalf("AllStages missing %s", s)
		return -1
	}
	if !(idx(domain.StagePending) < idx(domain.StageResearch) &&
		idx(domain.StageResearch) < idx(domain.StageCharacterPick) &&
		idx(domain.StageCharacterPick) < idx(domain.StageImage) &&
		idx(domain.StageImage) < idx(domain.StageBatchReview) &&
		idx(domain.StageBatchReview) < idx(domain.StageAssemble) &&
		idx(domain.StageAssemble) < idx(domain.StageComplete)) {
		t.Errorf("AllStages ordering violates pipeline expectations: %v", all)
	}
}

// --- CancelRegistry tests ---

func TestCancelRegistry_BeginRelease(t *testing.T) {
	t.Parallel()
	reg := pipeline.NewCancelRegistry()
	ctx, _, release := reg.Begin(context.Background(), "run-1")
	if reg.ActiveCount("run-1") != 1 {
		t.Errorf("ActiveCount after Begin = %d, want 1", reg.ActiveCount("run-1"))
	}
	if ctx.Err() != nil {
		t.Errorf("ctx should be live, got %v", ctx.Err())
	}
	release()
	if reg.ActiveCount("run-1") != 0 {
		t.Errorf("ActiveCount after release = %d, want 0", reg.ActiveCount("run-1"))
	}
	// release is idempotent (sync.Once)
	release()
}

func TestCancelRegistry_CancelAndWait_Drains(t *testing.T) {
	t.Parallel()
	reg := pipeline.NewCancelRegistry()
	var wg sync.WaitGroup
	const workers = 3
	var observedCancels int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		ctx, _, release := reg.Begin(context.Background(), "run-X")
		go func() {
			defer wg.Done()
			defer release()
			<-ctx.Done()
			atomic.AddInt32(&observedCancels, 1)
			// Simulate a tiny tail (DB write before exiting).
			time.Sleep(5 * time.Millisecond)
		}()
	}
	if err := reg.CancelAndWait("run-X", 2*time.Second); err != nil {
		t.Fatalf("CancelAndWait: %v", err)
	}
	wg.Wait()
	if got := atomic.LoadInt32(&observedCancels); got != workers {
		t.Errorf("observed cancels = %d, want %d", got, workers)
	}
	if reg.ActiveCount("run-X") != 0 {
		t.Errorf("registry not drained: %d active", reg.ActiveCount("run-X"))
	}
}

func TestCancelRegistry_CancelAndWait_Timeout(t *testing.T) {
	t.Parallel()
	reg := pipeline.NewCancelRegistry()
	// Worker that never releases.
	_, _, release := reg.Begin(context.Background(), "stuck")
	defer release()

	err := reg.CancelAndWait("stuck", 50*time.Millisecond)
	if !errors.Is(err, pipeline.ErrCancelTimeout) {
		t.Errorf("want ErrCancelTimeout, got %v", err)
	}
}

func TestCancelRegistry_NoWorkers_NoError(t *testing.T) {
	t.Parallel()
	reg := pipeline.NewCancelRegistry()
	if err := reg.CancelAndWait("ghost", time.Second); err != nil {
		t.Errorf("CancelAndWait on empty registry: %v", err)
	}
}

func TestCancelRegistry_NilReceiver_NoOps(t *testing.T) {
	t.Parallel()
	var reg *pipeline.CancelRegistry
	ctx, _, release := reg.Begin(context.Background(), "x")
	if ctx == nil {
		t.Errorf("nil registry must still return a valid ctx")
	}
	release() // no panic
	if err := reg.CancelAndWait("x", time.Second); err != nil {
		t.Errorf("nil registry CancelAndWait must be no-op: %v", err)
	}
}
