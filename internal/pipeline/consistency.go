package pipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// CheckConsistency verifies that filesystem state matches the DB-recorded
// artifact paths for a run. A non-nil InconsistencyReport is returned even
// when no mismatches are found — callers check len(report.Mismatches).
// A non-nil error indicates an I/O failure (not a mismatch).
//
// Mismatch categories:
//   - missing_file: a DB-recorded path is not present on disk
//   - unexpected_scenario_json: scenario.json exists but the run hasn't
//     reached the post-Phase-A stages yet
//   - run_directory_missing: runDir itself is absent (short-circuit signal)
//   - suspicious_path: a recorded path escapes runDir after resolution
//     (defense-in-depth; stat probing only, not write)
//
// Relative paths in segment records are resolved against runDir. Absolute
// paths must still resolve to a location under runDir; otherwise the
// mismatch is recorded as `suspicious_path` and the path is NOT stat'd
// (we refuse to probe arbitrary filesystem locations via DB values).
func CheckConsistency(runDir string, run *domain.Run, segments []*domain.Episode) (*domain.InconsistencyReport, error) {
	report := &domain.InconsistencyReport{
		RunID: run.ID,
		Stage: run.Stage,
	}

	// If runDir is missing AND we'd otherwise check paths inside it,
	// short-circuit with a single diagnostic (avoids per-file noise).
	// Pre-Phase-A runs have nothing on disk yet, so their missing runDir
	// is not an inconsistency.
	runDirExists, err := exists(runDir)
	if err != nil {
		return nil, err
	}
	if !runDirExists && expectsArtifacts(run.Stage, segments) {
		report.Mismatches = append(report.Mismatches, domain.Mismatch{
			Kind:     "run_directory_missing",
			Path:     runDir,
			Expected: "per-run output directory",
		})
		return report, nil
	}

	for _, ep := range segments {
		if ep.TTSPath != nil && *ep.TTSPath != "" {
			if err := checkSegmentPath(runDir, *ep.TTSPath,
				fmt.Sprintf("tts file for scene %d", ep.SceneIndex),
				report); err != nil {
				return nil, err
			}
		}
		if ep.ClipPath != nil && *ep.ClipPath != "" {
			if err := checkSegmentPath(runDir, *ep.ClipPath,
				fmt.Sprintf("clip file for scene %d", ep.SceneIndex),
				report); err != nil {
				return nil, err
			}
		}
		for i, shot := range ep.Shots {
			if shot.ImagePath == "" {
				continue
			}
			if err := checkSegmentPath(runDir, shot.ImagePath,
				fmt.Sprintf("image for scene %d shot %d", ep.SceneIndex, i),
				report); err != nil {
				return nil, err
			}
		}
	}

	// scenario.json existence expectations vary by stage.
	scenarioPath := filepath.Join(runDir, "scenario.json")
	scenarioExists, err := exists(scenarioPath)
	if err != nil {
		return nil, err
	}
	// An empty-string ScenarioPath is equivalent to nil — do not join it with
	// runDir (that resolves to runDir itself, which always exists, hiding a
	// real "scenario.json missing" bug).
	hasRecordedScenario := run.ScenarioPath != nil && *run.ScenarioPath != ""
	if isPostPhaseA(run.Stage) && hasRecordedScenario {
		// Use the sandbox-safe resolver; an out-of-runDir recorded scenario
		// path is flagged instead of stat'd.
		if err := checkSegmentPath(runDir, *run.ScenarioPath,
			"scenario.json for post-Phase-A run", report); err != nil {
			return nil, err
		}
	} else if isPrePhaseA(run.Stage) && scenarioExists {
		report.Mismatches = append(report.Mismatches, domain.Mismatch{
			Kind:     "unexpected_scenario_json",
			Path:     scenarioPath,
			Detail:   fmt.Sprintf("run stage %s precedes scenario.json creation", run.Stage),
		})
	}

	return report, nil
}

// checkSegmentPath resolves a DB-recorded path against runDir, verifies it
// stays inside the sandbox, and then stat-probes it. Mismatches are
// appended to report. A returned error signals an I/O failure (not a
// mismatch).
func checkSegmentPath(runDir, p, expected string, report *domain.InconsistencyReport) error {
	resolved := resolvePath(runDir, p)
	if !withinSandbox(runDir, resolved) {
		report.Mismatches = append(report.Mismatches, domain.Mismatch{
			Kind:     "suspicious_path",
			Path:     p,
			Expected: expected,
			Detail:   "recorded path resolves outside per-run directory",
		})
		return nil
	}
	ok, err := exists(resolved)
	if err != nil {
		return err
	}
	if !ok {
		report.Mismatches = append(report.Mismatches, domain.Mismatch{
			Kind:     "missing_file",
			Path:     p,
			Expected: expected,
		})
	}
	return nil
}

// withinSandbox reports whether cleanResolved is inside cleanRoot.
// Both inputs should already be absolute-style paths; this function
// re-cleans them defensively.
func withinSandbox(root, resolved string) bool {
	cleanRoot := filepath.Clean(root) + string(filepath.Separator)
	cleanResolved := filepath.Clean(resolved)
	return cleanResolved == filepath.Clean(root) ||
		strings.HasPrefix(cleanResolved, cleanRoot)
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat %s: %w", path, err)
}

func resolvePath(runDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(runDir, p)
}

// expectsArtifacts reports whether the given stage + segments would cause
// CheckConsistency to probe any on-disk paths. Used to decide whether a
// missing runDir is a real inconsistency or a benign pre-artifact state.
func expectsArtifacts(stage domain.Stage, segments []*domain.Episode) bool {
	if isPrePhaseA(stage) {
		// Phase A has no on-disk artifacts until scenario.json is written
		// at the critic → scenario_review boundary.
		return false
	}
	for _, ep := range segments {
		if ep.TTSPath != nil && *ep.TTSPath != "" {
			return true
		}
		if ep.ClipPath != nil && *ep.ClipPath != "" {
			return true
		}
		for _, shot := range ep.Shots {
			if shot.ImagePath != "" {
				return true
			}
		}
	}
	return false
}

// isPrePhaseA reports whether the stage is strictly before Phase A writes
// scenario.json to disk. scenario.json is written at the scenario_review
// boundary (after critic passes), so pre-Phase-A = everything up through critic.
func isPrePhaseA(s domain.Stage) bool {
	switch s {
	case domain.StagePending, domain.StageResearch, domain.StageStructure,
		domain.StageWrite, domain.StageVisualBreak, domain.StageReview,
		domain.StageCritic:
		return true
	}
	return false
}

// isPostPhaseA reports whether the stage expects scenario.json to exist.
func isPostPhaseA(s domain.Stage) bool {
	return !isPrePhaseA(s) && s != domain.StageComplete
}
