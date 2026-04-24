package pipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

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

// ArchiveRunArtifacts removes every known artifact file and subtree inside a
// per-run output directory as part of Story 10.3 Soft Archive. runDir is the
// absolute path to {cfg.OutputDir}/{run_id}. Known artifacts covered:
//
//   - scenario.json       (Phase A output)
//   - images/             (Phase B image subtree)
//   - tts/                (Phase B TTS subtree)
//   - clips/              (Phase C per-scene clips)
//   - output.mp4          (Phase C final video)
//   - metadata.json       (Phase C metadata bundle)
//   - manifest.json       (Phase C license manifest)
//
// After artifacts are removed, the runDir itself is removed if it is empty.
// A non-empty runDir is kept intact — unknown files authored outside the
// pipeline are never deleted. Missing entries are tolerated as idempotent
// archive state and do not fail the call. Returns the count of artifacts
// that existed at the time of removal (so "files deleted" is a meaningful
// number for the summary, not just "attempts made").
func ArchiveRunArtifacts(runDir string) (int, error) {
	if runDir == "" {
		return 0, fmt.Errorf("archive run artifacts: runDir is empty")
	}

	subtrees := []string{"images", "tts", "clips"}
	files := []string{"output.mp4", "metadata.json", "manifest.json", "scenario.json"}

	deleted := 0

	for _, sub := range subtrees {
		p := filepath.Join(runDir, sub)
		existed, err := pathExists(p)
		if err != nil {
			return deleted, err
		}
		if err := removeAll(p); err != nil {
			return deleted, err
		}
		if existed {
			deleted++
		}
	}
	for _, f := range files {
		p := filepath.Join(runDir, f)
		existed, err := pathExists(p)
		if err != nil {
			return deleted, err
		}
		if err := removeFile(p); err != nil {
			return deleted, err
		}
		if existed {
			deleted++
		}
	}

	// Best-effort: remove the now-hopefully-empty run directory so disk
	// usage drops to zero for this archived run.
	if err := os.Remove(runDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Dir already gone — fine.
			return deleted, nil
		}
		// ENOTEMPTY means unknown operator files remain — preserve them.
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.ENOTEMPTY {
			return deleted, nil
		}
		// Real error (permission denied, I/O error, etc.) — propagate.
		return deleted, fmt.Errorf("remove run dir %s: %w", runDir, err)
	}

	return deleted, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat %s: %w", path, err)
}
