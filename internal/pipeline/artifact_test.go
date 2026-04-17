package pipeline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCleanStageArtifacts_Image(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	// Seed files across multiple stage dirs; only images/ should be cleared.
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")
	mustWrite(t, filepath.Join(runDir, "images", "scene_02", "shot_01.png"), "img")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "scenario.json"), "{}")

	if err := pipeline.CleanStageArtifacts(runDir, domain.StageImage); err != nil {
		t.Fatalf("CleanStageArtifacts: %v", err)
	}
	assertMissing(t, filepath.Join(runDir, "images"))
	assertPresent(t, filepath.Join(runDir, "tts", "scene_01.wav"))
	assertPresent(t, filepath.Join(runDir, "scenario.json"))
}

func TestCleanStageArtifacts_TTS(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_02.wav"), "wav")
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")

	if err := pipeline.CleanStageArtifacts(runDir, domain.StageTTS); err != nil {
		t.Fatalf("CleanStageArtifacts: %v", err)
	}
	assertMissing(t, filepath.Join(runDir, "tts"))
	assertPresent(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"))
}

func TestCleanStageArtifacts_Assemble(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "clips", "scene_01.mp4"), "clip")
	mustWrite(t, filepath.Join(runDir, "output.mp4"), "final")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")

	if err := pipeline.CleanStageArtifacts(runDir, domain.StageAssemble); err != nil {
		t.Fatalf("CleanStageArtifacts: %v", err)
	}
	assertMissing(t, filepath.Join(runDir, "clips"))
	assertMissing(t, filepath.Join(runDir, "output.mp4"))
	// Phase B artifacts preserved.
	assertPresent(t, filepath.Join(runDir, "tts", "scene_01.wav"))
}

func TestCleanStageArtifacts_MetadataAck(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "metadata.json"), "{}")
	mustWrite(t, filepath.Join(runDir, "manifest.json"), "{}")
	mustWrite(t, filepath.Join(runDir, "output.mp4"), "final")

	if err := pipeline.CleanStageArtifacts(runDir, domain.StageMetadataAck); err != nil {
		t.Fatalf("CleanStageArtifacts: %v", err)
	}
	assertMissing(t, filepath.Join(runDir, "metadata.json"))
	assertMissing(t, filepath.Join(runDir, "manifest.json"))
	// Final video preserved (it is not a metadata_ack artifact).
	assertPresent(t, filepath.Join(runDir, "output.mp4"))
}

func TestCleanStageArtifacts_PhaseA_NoOp(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")

	for _, s := range []domain.Stage{
		domain.StageResearch, domain.StageStructure, domain.StageWrite,
		domain.StageVisualBreak, domain.StageReview, domain.StageCritic,
	} {
		if err := pipeline.CleanStageArtifacts(runDir, s); err != nil {
			t.Fatalf("CleanStageArtifacts(%s): %v", s, err)
		}
	}
	// Nothing touched — Phase A has no on-disk artifacts.
	assertPresent(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"))
	assertPresent(t, filepath.Join(runDir, "tts", "scene_01.wav"))
}

func TestCleanStageArtifacts_HITL_NoOp(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img")
	mustWrite(t, filepath.Join(runDir, "characters", "canonical.png"), "char")

	for _, s := range []domain.Stage{
		domain.StageScenarioReview, domain.StageCharacterPick,
		domain.StageBatchReview, domain.StageMetadataAck,
	} {
		if s == domain.StageMetadataAck {
			// MetadataAck has cleanup logic; skip here (covered by its own test).
			continue
		}
		if err := pipeline.CleanStageArtifacts(runDir, s); err != nil {
			t.Fatalf("CleanStageArtifacts(%s): %v", s, err)
		}
	}
	assertPresent(t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"))
	assertPresent(t, filepath.Join(runDir, "characters", "canonical.png"))
}

func TestCleanStageArtifacts_Idempotent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runDir := t.TempDir()
	mustWrite(t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav")

	// First call removes the directory.
	if err := pipeline.CleanStageArtifacts(runDir, domain.StageTTS); err != nil {
		t.Fatalf("first CleanStageArtifacts: %v", err)
	}
	// Second call on already-clean state must not error.
	if err := pipeline.CleanStageArtifacts(runDir, domain.StageTTS); err != nil {
		t.Fatalf("second CleanStageArtifacts: %v", err)
	}
	// Also idempotent for file-based cleanup.
	if err := pipeline.CleanStageArtifacts(runDir, domain.StageMetadataAck); err != nil {
		t.Fatalf("metadata_ack on clean runDir: %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s missing; stat err = %v", path, err)
	}
}

func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s present; stat err = %v", path, err)
	}
}
