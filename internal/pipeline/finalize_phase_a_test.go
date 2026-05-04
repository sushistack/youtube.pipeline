package pipeline

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestFinalizePhaseA_WritesScenarioJSON_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runDir := t.TempDir()
	state := sampleFinalizablePhaseAState()
	wrote, err := finalizePhaseA(runDir, state)
	if err != nil {
		t.Fatalf("finalizePhaseA: %v", err)
	}
	if !wrote {
		t.Fatal("expected scenario.json to be written")
	}
	if _, err := os.Stat(runDir + "/scenario.json"); err != nil {
		t.Fatalf("scenario.json missing: %v", err)
	}
	if state.Quality == nil || state.Contracts == nil {
		t.Fatalf("expected quality and contracts populated")
	}
}

func TestFinalizePhaseA_NoScenarioJSONOnRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runDir := t.TempDir()
	state := sampleFinalizablePhaseAState()
	state.Critic.PostReviewer.Verdict = domain.CriticVerdictRetry
	wrote, err := finalizePhaseA(runDir, state)
	if err != nil {
		t.Fatalf("finalizePhaseA: %v", err)
	}
	if wrote {
		t.Fatal("expected no scenario.json on retry")
	}
	if _, err := os.Stat(runDir + "/scenario.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no scenario.json, got %v", err)
	}
}

func TestFinalizePhaseA_ScenarioJSONAbsentBeforeFinalization(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runDir := t.TempDir()

	// Before finalizePhaseA is called, scenario.json must not exist.
	if _, err := os.Stat(runDir + "/scenario.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scenario.json must not exist before finalization, got stat err=%v", err)
	}

	// Calling finalizePhaseA with incomplete state (nil PostReviewer) also
	// must not write the file.
	partial := sampleFinalizablePhaseAState()
	partial.Critic.PostReviewer = nil
	wrote, err := finalizePhaseA(runDir, partial)
	if err != nil {
		t.Fatalf("finalizePhaseA with incomplete state: %v", err)
	}
	if wrote {
		t.Fatal("expected no scenario.json on incomplete state")
	}
	if _, err := os.Stat(runDir + "/scenario.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scenario.json must remain absent for incomplete state, got stat err=%v", err)
	}
}

func TestFinalizePhaseA_ContractManifestStable(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	first, err := buildPhaseAContractManifest()
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	second, err := buildPhaseAContractManifest()
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	rawFirst, _ := json.Marshal(first)
	rawSecond, _ := json.Marshal(second)
	testutil.AssertJSONEq(t, string(rawFirst), string(rawSecond))
	for _, path := range []string{
		first.ResearchSchema.Path,
		first.StructureSchema.Path,
		first.WriterSchema.Path,
		first.VisualBreakdownSchema.Path,
		first.ReviewSchema.Path,
		first.CriticPostWriterSchema.Path,
		first.CriticPostReviewerSchema.Path,
		first.PhaseAStateSchema.Path,
	} {
		if len(path) == 0 || path[0] == '/' {
			t.Fatalf("expected repo-relative contract path, got %q", path)
		}
	}
}

func TestFinalizePhaseA_JSONContainsBothCriticReports(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runDir := t.TempDir()
	state := sampleFinalizablePhaseAState()
	if _, err := finalizePhaseA(runDir, state); err != nil {
		t.Fatalf("finalizePhaseA: %v", err)
	}
	raw, err := os.ReadFile(runDir + "/scenario.json")
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	var got agents.PipelineState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal scenario: %v", err)
	}
	if got.Critic == nil || got.Critic.PostWriter == nil || got.Critic.PostReviewer == nil {
		t.Fatalf("expected both critic reports, got %+v", got.Critic)
	}
}

func sampleFinalizablePhaseAState() *agents.PipelineState {
	return &agents.PipelineState{
		RunID:           "run-1",
		SCPID:           "SCP-TEST",
		Research:     samplePhaseAResearch(),
		Structure:    &domain.StructurerOutput{SCPID: "SCP-TEST", TargetSceneCount: 10, SourceVersion: domain.SourceVersionV1},
		Narration:    samplePhaseANarration(),
		VisualScript: samplePhaseAVisualBreakdown(),
		Review: &domain.ReviewReport{
			OverallPass:      true,
			CoveragePct:      100,
			Issues:           []domain.ReviewIssue{},
			Corrections:      []domain.ReviewCorrection{},
			ReviewerModel:    "review-model",
			ReviewerProvider: "anthropic",
			SourceVersion:    domain.ReviewSourceVersionV1,
		},
		Critic: &domain.CriticOutput{
			PostWriter: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostWriter,
				Verdict:      domain.CriticVerdictAcceptWithNotes,
				OverallScore: 81,
				Feedback:     "좋습니다.",
			},
			PostReviewer: &domain.CriticCheckpointReport{
				Checkpoint:   domain.CriticCheckpointPostReviewer,
				Verdict:      domain.CriticVerdictPass,
				OverallScore: 88,
				Feedback:     "최종 검토까지 안정적입니다.",
			},
		},
		StartedAt:  "2026-04-18T12:00:00Z",
		FinishedAt: "2026-04-18T12:00:05Z",
	}
}
