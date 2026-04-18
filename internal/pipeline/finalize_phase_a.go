package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

func finalizePhaseA(runDir string, state *agents.PipelineState) (bool, error) {
	if state == nil {
		return false, fmt.Errorf("finalize phase a: %w: state is nil", domain.ErrValidation)
	}
	if state.Research == nil || state.Structure == nil || state.Narration == nil || state.VisualBreakdown == nil || state.Review == nil {
		return false, nil
	}
	if state.Critic == nil || state.Critic.PostWriter == nil || state.Critic.PostReviewer == nil {
		return false, nil
	}
	if state.Critic.PostReviewer.Verdict == domain.CriticVerdictRetry {
		return false, nil
	}
	if state.Critic.PostReviewer.Verdict != domain.CriticVerdictPass &&
		state.Critic.PostReviewer.Verdict != domain.CriticVerdictAcceptWithNotes {
		return false, fmt.Errorf("finalize phase a: %w: unsupported final verdict %q", domain.ErrValidation, state.Critic.PostReviewer.Verdict)
	}

	quality, err := ComputePhaseAQuality(state.Critic.PostWriter, state.Critic.PostReviewer)
	if err != nil {
		return false, err
	}
	manifest, err := buildPhaseAContractManifest()
	if err != nil {
		return false, err
	}
	state.Quality = &quality
	state.Contracts = &manifest
	if err := writeScenarioJSON(runDir, state); err != nil {
		return false, err
	}
	return true, nil
}

func writeScenarioJSON(runDir string, state *agents.PipelineState) error {
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal scenario: %w", err)
	}

	tmp, err := os.CreateTemp(runDir, "scenario-*.json")
	if err != nil {
		return fmt.Errorf("create temp scenario: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write scenario: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync scenario: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close scenario: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod scenario: %w", err)
	}

	finalPath := filepath.Join(runDir, "scenario.json")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		cleanup()
		return fmt.Errorf("rename scenario: %w", err)
	}
	return nil
}

func buildPhaseAContractManifest() (agents.PhaseAContractManifest, error) {
	root, err := findProjectRoot()
	if err != nil {
		return agents.PhaseAContractManifest{}, err
	}

	build := func(rel string) (agents.ContractRef, error) {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return agents.ContractRef{}, fmt.Errorf("build phase a contract manifest: read %s: %w", rel, err)
		}
		sum := sha256.Sum256(raw)
		return agents.ContractRef{
			Path:   filepath.ToSlash(rel),
			SHA256: hex.EncodeToString(sum[:]),
		}, nil
	}

	paths := []string{
		"testdata/contracts/researcher_output.schema.json",
		"testdata/contracts/structurer_output.schema.json",
		"testdata/contracts/writer_output.schema.json",
		"testdata/contracts/visual_breakdown.schema.json",
		"testdata/contracts/reviewer_report.schema.json",
		"testdata/contracts/critic_post_writer.schema.json",
		"testdata/contracts/critic_post_reviewer.schema.json",
		"testdata/contracts/phase_a_state.schema.json",
	}
	refs := make([]agents.ContractRef, 0, len(paths))
	for _, rel := range paths {
		ref, err := build(rel)
		if err != nil {
			return agents.PhaseAContractManifest{}, err
		}
		refs = append(refs, ref)
	}

	return agents.PhaseAContractManifest{
		ResearchSchema:           refs[0],
		StructureSchema:          refs[1],
		WriterSchema:             refs[2],
		VisualBreakdownSchema:    refs[3],
		ReviewSchema:             refs[4],
		CriticPostWriterSchema:   refs[5],
		CriticPostReviewerSchema: refs[6],
		PhaseAStateSchema:        refs[7],
	}, nil
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find project root: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("find project root: %w", domain.ErrValidation)
		}
		dir = parent
	}
}
