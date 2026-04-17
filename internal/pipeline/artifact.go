package pipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// CleanStageArtifacts removes on-disk artifacts scoped to a single failed stage.
// runDir is the absolute path to the per-run output directory
// (typically {cfg.OutputDir}/{run_id}/). The function is idempotent —
// missing files are not errors — and its scope is deliberately narrow:
// it only ever removes files that belong to the given stage, never
// anything from a different stage or another run.
//
// HITL stages and Phase A stages are no-ops because they produce no
// on-disk artifacts (Phase A state is in-memory until scenario_review).
func CleanStageArtifacts(runDir string, stage domain.Stage) error {
	switch stage {
	case domain.StageImage:
		return removeAll(filepath.Join(runDir, "images"))
	case domain.StageTTS:
		return removeAll(filepath.Join(runDir, "tts"))
	case domain.StageAssemble:
		if err := removeAll(filepath.Join(runDir, "clips")); err != nil {
			return err
		}
		return removeFile(filepath.Join(runDir, "output.mp4"))
	case domain.StageMetadataAck:
		if err := removeFile(filepath.Join(runDir, "metadata.json")); err != nil {
			return err
		}
		return removeFile(filepath.Join(runDir, "manifest.json"))
	}
	// Phase A, HITL, pending, complete — no artifacts of their own.
	return nil
}

func removeAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
