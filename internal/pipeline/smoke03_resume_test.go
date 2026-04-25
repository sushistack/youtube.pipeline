package pipeline_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestE2E_SMOKE03_ResumeIdempotency exercises Engine.Resume against a Phase C
// failure and pins the idempotency contract (test-design §4 SMOKE-03).
//
// The run is seeded at StageAssemble/StatusFailed with fully-populated
// segments — the canonical SCP-049 seed bundled by Step 4.5 — copied into
// the per-run output directory so Resume's CheckConsistency sandbox check
// passes (DB-recorded paths must resolve under runDir).
//
// Resume idempotency: each Resume call re-runs only the failed/waiting
// stage's narrow scope, never replaying earlier work.
//
//  1. Resume #1 from StageAssemble/StatusFailed runs Phase C once and
//     advances to StageMetadataAck/StatusWaiting with all three artifacts
//     (output.mp4 + metadata.json + manifest.json) on disk.
//  2. Phase A meta (CriticScore, ScenarioPath) survives the Phase C
//     transition — the rollback contract pinned by Story 11-1.
//  3. Resume #2 from StageMetadataAck/StatusWaiting re-runs only the
//     metadata-entry step: Phase C (assembly) is NOT replayed, so
//     UpdateOutputPath / UpdateClipPath call counts must stay flat.
//     metadata.json + manifest.json are regenerated (CleanStageArtifacts
//     scope), but output.mp4 is preserved.
func TestE2E_SMOKE03_ResumeIdempotency(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	const runID = "smoke03-resume"
	outDir := t.TempDir()
	runDir := filepath.Join(outDir, runID)
	stageSeedIntoRunDir(t, runDir)

	_, segs := loadSCP049Seed(t)
	rewriteSegmentsToRunDir(t, runDir, segs)
	for _, seg := range segs {
		seg.RunID = runID
	}

	score := 0.92
	scenarioPath := "scenario.json"
	runStore := &e2eRunStore{
		run: &domain.Run{
			ID:           runID,
			SCPID:        "SCP-049",
			Stage:        domain.StageAssemble,
			Status:       domain.StatusFailed,
			CriticScore:  &score,
			ScenarioPath: &scenarioPath,
		},
	}
	segStore := &e2eSegmentStore{segments: segs}

	runUpdater := &fakeRunUpdater{}
	segUpdater := &fakeSegmentUpdater{}

	engine := pipeline.NewEngine(runStore, segStore, nil, clock.RealClock{}, outDir, slog.Default())
	engine.SetPhaseCRunner(pipeline.NewPhaseCRunner(
		segUpdater, runUpdater, nil, clock.RealClock{}, slog.Default(),
	))
	engine.SetPhaseCMetadataBuilder(&e2eMetadataBuilder{outDir: outDir})

	ctx := context.Background()

	// ── First Resume: assemble + metadata, advance to MetadataAck/Waiting. ──
	report, err := engine.Resume(ctx, runID)
	if err != nil {
		t.Fatalf("first Resume: %v", err)
	}
	if report != nil && len(report.Mismatches) > 0 {
		t.Fatalf("first Resume reports mismatches: %v", report.Mismatches)
	}

	if got := runStore.run.Stage; got != domain.StageMetadataAck {
		t.Fatalf("after first Resume: stage = %s, want %s", got, domain.StageMetadataAck)
	}
	if got := runStore.run.Status; got != domain.StatusWaiting {
		t.Fatalf("after first Resume: status = %s, want %s", got, domain.StatusWaiting)
	}

	// Phase A meta survives the Phase C transition (Story 11-1 contract).
	if runStore.run.CriticScore == nil || *runStore.run.CriticScore != score {
		t.Errorf("critic_score lost across resume: got %v, want %v",
			runStore.run.CriticScore, score)
	}
	if runStore.run.ScenarioPath == nil || *runStore.run.ScenarioPath != scenarioPath {
		t.Errorf("scenario_path lost across resume: got %v, want %q",
			runStore.run.ScenarioPath, scenarioPath)
	}

	for _, art := range []string{"output.mp4", "metadata.json", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(runDir, art)); err != nil {
			t.Errorf("artifact missing after first Resume: %s: %v", art, err)
		}
	}

	// Phase C must run exactly once: one UpdateOutputPath, one UpdateClipPath
	// per segment. Re-running would double-count and is the regression this
	// test guards.
	if got := len(runUpdater.updated); got != 1 {
		t.Errorf("UpdateOutputPath calls = %d, want 1", got)
	}
	if got := len(segUpdater.updated); got != len(segs) {
		t.Errorf("UpdateClipPath calls = %d, want %d", got, len(segs))
	}

	// ── Second Resume: metadata_ack/waiting re-runs metadata only. ──────────
	mp4Stat1, err := os.Stat(filepath.Join(runDir, "output.mp4"))
	if err != nil {
		t.Fatalf("stat output.mp4 before second Resume: %v", err)
	}
	report2, err := engine.Resume(ctx, runID)
	if err != nil {
		t.Fatalf("second Resume: %v", err)
	}
	if report2 != nil && len(report2.Mismatches) > 0 {
		t.Fatalf("second Resume reports mismatches: %v", report2.Mismatches)
	}

	// Phase C must NOT have been replayed — its updaters are append-only.
	if got := len(runUpdater.updated); got != 1 {
		t.Errorf("UpdateOutputPath calls after second Resume = %d, want 1 (Phase C must not replay)", got)
	}
	if got := len(segUpdater.updated); got != len(segs) {
		t.Errorf("UpdateClipPath calls after second Resume = %d, want %d (Phase C must not replay)",
			got, len(segs))
	}

	// metadata.json + manifest.json regenerated; output.mp4 preserved
	// (StageMetadataAck's clean scope excludes the assembly artifact).
	for _, art := range []string{"metadata.json", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(runDir, art)); err != nil {
			t.Errorf("artifact missing after second Resume: %s: %v", art, err)
		}
	}
	mp4Stat2, err := os.Stat(filepath.Join(runDir, "output.mp4"))
	if err != nil {
		t.Fatalf("stat output.mp4 after second Resume: %v", err)
	}
	if !mp4Stat1.ModTime().Equal(mp4Stat2.ModTime()) || mp4Stat1.Size() != mp4Stat2.Size() {
		t.Errorf("output.mp4 mutated across second Resume: was (mtime=%v size=%d), now (mtime=%v size=%d)",
			mp4Stat1.ModTime(), mp4Stat1.Size(), mp4Stat2.ModTime(), mp4Stat2.Size())
	}

	if got := runStore.run.Stage; got != domain.StageMetadataAck {
		t.Errorf("after second Resume: stage = %s, want %s", got, domain.StageMetadataAck)
	}
	if got := runStore.run.Status; got != domain.StatusWaiting {
		t.Errorf("after second Resume: status = %s, want %s", got, domain.StatusWaiting)
	}
}

// stageSeedIntoRunDir creates runDir with the per-stage subdirs Engine.Resume
// expects to find when CheckConsistency runs at StageAssemble (image+tts
// artifacts already on disk; clips/+output.mp4 absent and to be produced).
// scenario.json is written as an empty JSON object — its content is not
// asserted; only its presence under runDir is required by the consistency
// check at post-Phase-A stages.
func stageSeedIntoRunDir(t testing.TB, runDir string) {
	t.Helper()
	for _, sub := range []string{"images", "tts"} {
		if err := os.MkdirAll(filepath.Join(runDir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(runDir, "scenario.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write scenario.json stub: %v", err)
	}
}

// rewriteSegmentsToRunDir copies the canonical seed's image and tts files
// into runDir/images and runDir/tts and rewrites each segment's path to the
// in-runDir copy. CheckConsistency rejects DB-recorded paths that resolve
// outside runDir, so the segments returned by loadSCP049Seed (which point
// at the immutable testdata/e2e/scp-049-seed/ tree) cannot be used as-is
// once the engine seam is involved.
func rewriteSegmentsToRunDir(t testing.TB, runDir string, segments []*domain.Episode) {
	t.Helper()
	imgDir := filepath.Join(runDir, "images")
	ttsDir := filepath.Join(runDir, "tts")
	for i, seg := range segments {
		for j := range seg.Shots {
			src := seg.Shots[j].ImagePath
			dst := filepath.Join(imgDir, scp049SceneFilename("scene", i, ".png"))
			copySeedFile(t, src, dst)
			seg.Shots[j].ImagePath = dst
		}
		if seg.TTSPath != nil {
			src := *seg.TTSPath
			dst := filepath.Join(ttsDir, scp049SceneFilename("scene", i, ".wav"))
			copySeedFile(t, src, dst)
			seg.TTSPath = &dst
		}
	}
}

func copySeedFile(t testing.TB, src, dst string) {
	t.Helper()
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read seed %s: %v", src, err)
	}
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		t.Fatalf("write seed copy %s: %v", dst, err)
	}
}
