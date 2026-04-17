package pipeline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCheckConsistency_AllPresent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageTTS}
	seg := &domain.Episode{
		RunID:      "scp-049-run-1",
		SceneIndex: 0,
		TTSPath:    strPtr("tts/scene_01.wav"),
		Shots: []domain.Shot{
			{ImagePath: "images/scene_01/shot_01.png"},
		},
	}

	report, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if report == nil {
		t.Fatal("report is nil")
	}
	if len(report.Mismatches) != 0 {
		t.Errorf("expected 0 mismatches; got %d: %+v", len(report.Mismatches), report.Mismatches)
	}
}

func TestCheckConsistency_MissingTTS(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	// tts file NOT written — DB says it should exist.

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageTTS}
	seg := &domain.Episode{
		RunID:      "scp-049-run-1",
		SceneIndex: 0,
		TTSPath:    strPtr("tts/scene_01.wav"),
	}

	report, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if len(report.Mismatches) != 1 {
		t.Fatalf("expected 1 mismatch; got %d", len(report.Mismatches))
	}
	if report.Mismatches[0].Kind != "missing_file" {
		t.Errorf("Kind = %q, want missing_file", report.Mismatches[0].Kind)
	}
	if report.Mismatches[0].Path != "tts/scene_01.wav" {
		t.Errorf("Path = %q, want tts/scene_01.wav", report.Mismatches[0].Path)
	}
}

func TestCheckConsistency_MissingShotImage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageImage}
	seg := &domain.Episode{
		RunID:      "scp-049-run-1",
		SceneIndex: 0,
		Shots: []domain.Shot{
			{ImagePath: "images/scene_01/shot_01.png"},
			{ImagePath: "images/scene_01/shot_02.png"},
		},
	}
	// Only shot_01 on disk.
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")

	report, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if len(report.Mismatches) != 1 {
		t.Fatalf("expected 1 mismatch; got %d: %+v", len(report.Mismatches), report.Mismatches)
	}
	if report.Mismatches[0].Path != "images/scene_01/shot_02.png" {
		t.Errorf("Path = %q, want shot_02 missing", report.Mismatches[0].Path)
	}
}

func TestCheckConsistency_UnexpectedScenarioJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	// Scenario.json present but stage is Phase A (write).
	mustWrite(t, filepath.Join(runDir, "scenario.json"), "{}")

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageWrite}
	report, err := pipeline.CheckConsistency(runDir, run, nil)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	// Expect exactly one unexpected_scenario_json mismatch.
	foundUnexpected := false
	for _, m := range report.Mismatches {
		if m.Kind == "unexpected_scenario_json" {
			foundUnexpected = true
		}
	}
	if !foundUnexpected {
		t.Errorf("expected unexpected_scenario_json; got mismatches: %+v", report.Mismatches)
	}
}

func TestCheckConsistency_MissingScenarioJSONAfterPhaseA(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	// scenario.json missing but stage is post-Phase A.

	run := &domain.Run{
		ID:           "scp-049-run-1",
		Stage:        domain.StageImage,
		ScenarioPath: strPtr("scenario.json"),
	}
	report, err := pipeline.CheckConsistency(runDir, run, nil)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if len(report.Mismatches) != 1 {
		t.Fatalf("expected 1 mismatch; got %+v", report.Mismatches)
	}
	if report.Mismatches[0].Kind != "missing_file" {
		t.Errorf("Kind = %q, want missing_file", report.Mismatches[0].Kind)
	}
}

func TestCheckConsistency_AbsolutePathResolution(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	absTTS := filepath.Join(runDir, "tts", "scene_01.wav")
	mustWrite(t, absTTS, "wav")

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageTTS}
	seg := &domain.Episode{RunID: run.ID, SceneIndex: 0, TTSPath: &absTTS}

	report, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if len(report.Mismatches) != 0 {
		t.Errorf("expected 0 mismatches on absolute path; got %+v", report.Mismatches)
	}
}

func TestCheckConsistency_StatFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()

	// Make a file path that's actually a file in place of a directory segment —
	// os.Stat succeeds on both files and dirs, so we instead pass a path
	// segment that is provably unusable: a file used as a parent directory.
	parent := filepath.Join(runDir, "tts")
	mustWrite(t, parent, "oops-this-is-a-file")
	// "tts/scene_01.wav" would now require tts to be a directory; stat returns
	// a "not a directory" error (ENOTDIR), which is neither nil nor IsNotExist.

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageTTS}
	seg := &domain.Episode{
		RunID:      "scp-049-run-1",
		SceneIndex: 0,
		TTSPath:    strPtr("tts/scene_01.wav"),
	}
	_, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err == nil {
		t.Fatal("expected I/O error on stat through non-directory")
	}
}

func TestCheckConsistency_RunDirMissing_ShortCircuit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// runDir was never created — simulates user deleting the output tree.
	runDir := filepath.Join(t.TempDir(), "does-not-exist")

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageTTS}
	seg := &domain.Episode{
		RunID: run.ID, SceneIndex: 0,
		TTSPath: strPtr("tts/scene_01.wav"),
		Shots: []domain.Shot{
			{ImagePath: "images/scene_01/shot_01.png"},
		},
	}

	report, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if len(report.Mismatches) != 1 {
		t.Fatalf("expected 1 short-circuit mismatch; got %d: %+v",
			len(report.Mismatches), report.Mismatches)
	}
	if report.Mismatches[0].Kind != "run_directory_missing" {
		t.Errorf("Kind = %q, want run_directory_missing", report.Mismatches[0].Kind)
	}
}

func TestCheckConsistency_EmptyScenarioPathTreatedAsNil(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	// Materialize scenario.json so a separate post-Phase-A sanity check
	// doesn't accidentally flag it.
	mustWrite(t, filepath.Join(runDir, "scenario.json"), "{}")

	empty := ""
	run := &domain.Run{
		ID:           "scp-049-run-1",
		Stage:        domain.StageImage,
		ScenarioPath: &empty,
	}
	report, err := pipeline.CheckConsistency(runDir, run, nil)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	// Empty ScenarioPath is treated as nil — it must NOT resolve to runDir
	// (which would always "exist") nor be recorded as a missing_file with
	// path "" (meaningless). Report should be clean.
	for _, m := range report.Mismatches {
		if m.Path == "" || m.Path == runDir {
			t.Errorf("empty ScenarioPath leaked into mismatch: %+v", m)
		}
	}
}

func TestCheckConsistency_PathTraversalFlaggedAsSuspicious(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageTTS}
	// Relative path that escapes runDir via `..`.
	bad := "../../etc/passwd"
	seg := &domain.Episode{RunID: run.ID, SceneIndex: 0, TTSPath: &bad}

	report, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if len(report.Mismatches) != 1 {
		t.Fatalf("expected 1 mismatch; got %+v", report.Mismatches)
	}
	if report.Mismatches[0].Kind != "suspicious_path" {
		t.Errorf("Kind = %q, want suspicious_path", report.Mismatches[0].Kind)
	}
}

func TestCheckConsistency_AbsolutePathOutsideRunDirFlagged(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()

	run := &domain.Run{ID: "scp-049-run-1", Stage: domain.StageTTS}
	// Absolute path pointing elsewhere.
	seg := &domain.Episode{
		RunID: run.ID, SceneIndex: 0,
		TTSPath: strPtr("/tmp/leaked-tts.wav"),
	}

	report, err := pipeline.CheckConsistency(runDir, run, []*domain.Episode{seg})
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	// Phase B artifacts must live under runDir; any absolute escape is
	// suspicious, not a benign "missing_file".
	foundSuspicious := false
	for _, m := range report.Mismatches {
		if m.Kind == "suspicious_path" && m.Path == "/tmp/leaked-tts.wav" {
			foundSuspicious = true
		}
	}
	if !foundSuspicious {
		t.Errorf("expected suspicious_path mismatch; got %+v", report.Mismatches)
	}
}

func strPtr(s string) *string { return &s }

func init() {
	// ensure os is referenced in dependent paths below if future expansions need it.
	_ = os.PathSeparator
}
