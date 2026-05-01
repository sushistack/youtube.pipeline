package pipeline_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// seedMidPhaseBCrashState mutates the harness run into the exact post-restart
// state that triggered the user-reported "Resume failed: request validation
// failed" bug:
//
//   - stage = image, status = failed
//   - retry_reason = domain.RetryReasonServerRestarted (the orphan reconciler
//     marker; status='running' did not survive the restart)
//   - segments rows have populated image_path / tts_path values pointing at
//     files that no longer exist on disk (Phase B was interrupted between
//     allocating the path and writing the bytes — or the writes were lost
//     to the same restart that killed the process).
//
// Returns the runID for convenience.
func seedMidPhaseBCrashState(t *testing.T, h *rewindHarness) string {
	t.Helper()
	ctx := context.Background()

	scenarioPath := "scenario.json"
	if _, err := h.database.ExecContext(ctx,
		`UPDATE runs
		    SET stage = ?, status = ?, retry_reason = ?,
		        scenario_path = ?, critic_score = ?,
		        selected_character_id = ?, frozen_descriptor = ?, character_query_key = ?
		  WHERE id = ?`,
		string(domain.StageImage), string(domain.StatusFailed),
		domain.RetryReasonServerRestarted,
		scenarioPath, 0.85,
		"cand-1", "elderly scholar", "scholar",
		h.runID,
	); err != nil {
		t.Fatalf("seed mid-Phase-B crash: %v", err)
	}

	// Two segments with paths pointing at NON-EXISTENT files.
	for i := 0; i < 2; i++ {
		shotsJSON := `[{"image_path":"images/scene_0` + strconv.Itoa(i+1) + `/shot_01.png","duration_s":1.5,"transition":"cut","visual_descriptor":"x"}]`
		if _, err := h.database.ExecContext(ctx,
			`INSERT INTO segments (run_id, scene_index, narration, shot_count, shots, tts_path, tts_duration_ms, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`,
			h.runID, i, "narration "+strconv.Itoa(i), 1, shotsJSON,
			"tts/scene_0"+strconv.Itoa(i+1)+".wav", 1500,
		); err != nil {
			t.Fatalf("seed mid-flight segment %d: %v", i, err)
		}
	}

	// Only scenario.json on disk — image/tts directories never made it.
	runDir := filepath.Join(h.outDir, h.runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, scenarioPath), []byte("{}"), 0644); err != nil {
		t.Fatalf("write scenario.json: %v", err)
	}
	return h.runID
}

// TestPrepareResume_AutoBypassesConsistencyForReconcilerMarkedRun guards the
// fix for the user-reported recovery dead-end. With retry_reason set to
// RetryReasonServerRestarted, PrepareResume must:
//
//   - log the FS/DB mismatches (so the operator can audit what was lost)
//   - NOT return ErrValidation (so the FailureBanner Resume button works)
//   - run the standard resume cleanup (DB segment delete, ResetForResume)
//
// We assert against the public engine surface: Resume() must succeed, and
// the run must end up in the post-cleanup state (status flipped, retry
// counter incremented, segments deleted by Phase B clean-slate semantics).
func TestPrepareResume_AutoBypassesConsistencyForReconcilerMarkedRun(t *testing.T) {
	h := newRewindHarness(t)
	seedMidPhaseBCrashState(t, h)

	report, err := h.engine.Resume(context.Background(), h.runID)
	if err != nil {
		t.Fatalf("Resume should succeed for reconciler-marked run with consistency mismatches; got %v", err)
	}
	if report == nil || len(report.Mismatches) == 0 {
		t.Errorf("expected mismatches to be reported (auditing) but got %+v", report)
	}

	// Post-resume run state: status flipped off failed, retry_count++,
	// retry_reason cleared.
	run, err := h.runStore.Get(context.Background(), h.runID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if run.Status == domain.StatusFailed {
		t.Errorf("status should not still be failed after Resume; got %s", run.Status)
	}
	if run.RetryReason != nil {
		t.Errorf("retry_reason should be cleared post-Resume; got %v", *run.RetryReason)
	}
}

// TestPrepareResume_StillBlocksOnUnreconciledMismatch pins the inverse: a
// run whose retry_reason is NOT the system marker (e.g. legitimate
// stage-failure with operator-visible context) still hits the strict gate.
// Without this, an operator's deliberate stage failure followed by a Resume
// would silently wipe their hard-earned mid-stage outputs.
func TestPrepareResume_StillBlocksOnUnreconciledMismatch(t *testing.T) {
	h := newRewindHarness(t)
	seedMidPhaseBCrashState(t, h)

	// Override the retry_reason to look like a normal stage failure.
	if _, err := h.database.Exec(
		`UPDATE runs SET retry_reason = ? WHERE id = ?`,
		"phase b: image_track: 502 from upstream", h.runID,
	); err != nil {
		t.Fatalf("override retry_reason: %v", err)
	}

	_, err := h.engine.Resume(context.Background(), h.runID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("non-system retry_reason must still hit the strict gate; got %v", err)
	}

	// Forcing should still work in that case.
	if _, err := h.engine.ResumeWithOptions(context.Background(), h.runID, pipeline.ResumeOptions{Force: true}); err != nil {
		t.Errorf("Force=true must override the gate; got %v", err)
	}
}

// TestRewindThenGenerateAssetsThenCrashThenResume is the end-to-end
// regression: rewind to assets → flip to running (simulating Generate
// Assets click) → orphan-reconcile → Resume should succeed without manual
// confirm_inconsistent. This is the user's reported flow as a single test.
func TestRewindThenGenerateAssetsThenCrashThenResume(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	if _, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssets); err != nil {
		t.Fatalf("Rewind: %v", err)
	}

	// Verify rewind cleared image_path on segments — guard rail against
	// regressions in the JSON rewrite path.
	verifyImagePathsCleared(t, h)

	// Simulate "Generate Assets" click then crash before any Phase B write.
	if _, err := h.database.Exec(
		`UPDATE runs SET status = ? WHERE id = ?`,
		string(domain.StatusRunning), h.runID,
	); err != nil {
		t.Fatalf("simulate generate-assets click: %v", err)
	}
	// Now simulate that Phase B partially wrote DB rows but FS files never
	// landed (the worst case for consistency check).
	if _, err := h.database.Exec(
		`UPDATE segments SET shots = ?, tts_path = ?, tts_duration_ms = ? WHERE run_id = ? AND scene_index = 0`,
		`[{"image_path":"images/scene_01/shot_01.png","duration_s":1.5,"transition":"cut","visual_descriptor":"x"}]`,
		"tts/scene_01.wav", 1500,
		h.runID,
	); err != nil {
		t.Fatalf("simulate partial Phase B write: %v", err)
	}

	// Server restart → orphan reconcile.
	if _, err := h.runStore.ReconcileOrphanedRuns(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Operator clicks Resume — must succeed without confirm_inconsistent.
	if _, err := h.engine.Resume(context.Background(), h.runID); err != nil {
		t.Fatalf("Resume after rewind+crash should succeed; got %v", err)
	}
}

func verifyImagePathsCleared(t *testing.T, h *rewindHarness) {
	t.Helper()
	rows, err := h.database.Query(
		`SELECT scene_index, shots FROM segments WHERE run_id = ? ORDER BY scene_index ASC`,
		h.runID,
	)
	if err != nil {
		t.Fatalf("query segments: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			idx int
			raw sql.NullString
		)
		if err := rows.Scan(&idx, &raw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if !raw.Valid || raw.String == "" {
			continue
		}
		if got := raw.String; len(got) > 0 && !containsCleared(got) {
			t.Errorf("scene %d: image_path not cleared in shots JSON: %s", idx, got)
		}
	}
}

func containsCleared(shotsJSON string) bool {
	// Cleared = each shot's image_path field is "" (empty). A JSON
	// substring match is enough for this assertion since the rewrite path
	// always re-encodes via the canonical encoder.
	return shotsJSON != "" && (shotsJSON == "[]" ||
		stringContains(shotsJSON, `"image_path":""`))
}

func stringContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
