package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// Compile-time assertion that the AgentFunc signature does not drift.
// If this ever fails to compile, every concrete agent in Stories 3.2–3.5
// will fail too — which is the right failure mode.
func noopAgent(ctx context.Context, state *PipelineState) error { return nil }

var _ AgentFunc = noopAgent

func TestAgentFunc_SignatureStable(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var f AgentFunc = func(ctx context.Context, state *PipelineState) error { return nil }
	if f == nil {
		t.Fatalf("AgentFunc assignment failed")
	}
}

func TestPipelineState_JSONShape_ZeroValue(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var s PipelineState
	got, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("marshal zero state: %v", err)
	}
	want := `{"run_id":"","scp_id":"","started_at":"","finished_at":""}`
	testutil.AssertEqual(t, string(got), want)
}

func TestPipelineState_JSONShape_RoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	orig := PipelineState{
		RunID:           "run-abc",
		SCPID:           "scp-001",
		Research:        &domain.ResearcherOutput{SCPID: "SCP-TEST", Title: "x"},
		Structure:       &domain.StructurerOutput{SCPID: "SCP-TEST", TargetSceneCount: 10},
		Narration:       &domain.NarrationScript{SCPID: "SCP-TEST", Title: "x"},
		VisualBreakdown: &domain.VisualBreakdownOutput{SCPID: "SCP-TEST", Title: "x", ShotOverrides: map[int]domain.ShotOverride{}, SourceVersion: domain.VisualBreakdownSourceVersionV1},
		Review:          &domain.ReviewReport{OverallPass: true, SourceVersion: domain.ReviewSourceVersionV1},
		Critic:          &domain.CriticOutput{PostWriter: &domain.CriticCheckpointReport{Verdict: domain.CriticVerdictPass}},
		StartedAt:       "2026-04-18T10:00:00Z",
		FinishedAt:      "2026-04-18T10:05:00Z",
	}

	first, err := json.Marshal(&orig)
	if err != nil {
		t.Fatalf("marshal #1: %v", err)
	}

	var round PipelineState
	if err := json.Unmarshal(first, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	second, err := json.Marshal(&round)
	if err != nil {
		t.Fatalf("marshal #2: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip not byte-identical:\nfirst:  %s\nsecond: %s", first, second)
	}
	if round.Research == nil || round.Research.SCPID != "SCP-TEST" {
		t.Fatalf("typed research did not round-trip: %+v", round.Research)
	}
}

func TestPipelineState_JSONTags_SnakeCase(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Populate every field so each appears in the serialized output.
	s := PipelineState{
		RunID:           "r",
		SCPID:           "s",
		Research:        &domain.ResearcherOutput{},
		Structure:       &domain.StructurerOutput{},
		Narration:       &domain.NarrationScript{},
		VisualBreakdown: &domain.VisualBreakdownOutput{ShotOverrides: map[int]domain.ShotOverride{}},
		Review:          &domain.ReviewReport{},
		Critic:          &domain.CriticOutput{},
		StartedAt:       "t",
		FinishedAt:      "t",
	}
	raw, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := string(raw)

	// Required snake_case keys.
	for _, key := range []string{
		`"run_id"`, `"scp_id"`, `"research"`, `"structure"`,
		`"narration"`, `"visual_breakdown"`, `"review"`, `"critic"`,
		`"started_at"`, `"finished_at"`,
	} {
		if !strings.Contains(payload, key) {
			t.Errorf("missing required JSON key %s in %s", key, payload)
		}
	}

	// Forbidden camelCase variants.
	for _, bad := range []string{
		`"runId"`, `"scpId"`, `"visualBreakdown"`, `"startedAt"`, `"finishedAt"`,
	} {
		if strings.Contains(payload, bad) {
			t.Errorf("forbidden camelCase JSON key %s found in %s", bad, payload)
		}
	}
}

func TestPipelineState_VisualBreakdownReviewTyped(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	orig := PipelineState{
		VisualBreakdown: &domain.VisualBreakdownOutput{
			SCPID:            "SCP-TEST",
			Title:            "SCP-TEST",
			FrozenDescriptor: "Appearance: test",
			Scenes: []domain.VisualBreakdownScene{{
				SceneNum:              1,
				ActID:                 domain.ActIncident,
				Narration:             "scene",
				EstimatedTTSDurationS: 7.1,
				ShotCount:             1,
				Shots: []domain.VisualShot{{
					ShotIndex:          1,
					VisualDescriptor:   "Appearance: test; shot",
					EstimatedDurationS: 7.1,
					Transition:         domain.TransitionKenBurns,
				}},
			}},
			ShotOverrides: map[int]domain.ShotOverride{},
			Metadata: domain.VisualBreakdownMetadata{
				VisualBreakdownModel:    "model",
				VisualBreakdownProvider: "provider",
				PromptTemplate:          "03_5_visual_breakdown.md",
				ShotFormulaVersion:      domain.ShotFormulaVersionV1,
			},
			SourceVersion: domain.VisualBreakdownSourceVersionV1,
		},
		Review: &domain.ReviewReport{
			OverallPass:      true,
			CoveragePct:      100,
			Issues:           []domain.ReviewIssue{},
			Corrections:      []domain.ReviewCorrection{},
			ReviewerModel:    "reviewer",
			ReviewerProvider: "anthropic",
			SourceVersion:    domain.ReviewSourceVersionV1,
		},
	}
	raw, err := json.Marshal(&orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round PipelineState
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.VisualBreakdown == nil || round.Review == nil {
		t.Fatalf("typed fields did not unmarshal: %+v", round)
	}
	testutil.AssertEqual(t, round.VisualBreakdown.SourceVersion, domain.VisualBreakdownSourceVersionV1)
	testutil.AssertEqual(t, round.Review.SourceVersion, domain.ReviewSourceVersionV1)
}

func TestPipelineState_ResearchStructureTyped(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	orig := PipelineState{
		Research:  &domain.ResearcherOutput{SCPID: "SCP-TEST"},
		Structure: &domain.StructurerOutput{SCPID: "SCP-TEST"},
	}
	raw, err := json.Marshal(&orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round PipelineState
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Research == nil || round.Structure == nil {
		t.Fatalf("typed fields did not unmarshal: %+v", round)
	}
	testutil.AssertEqual(t, round.Research.SCPID, "SCP-TEST")
	testutil.AssertEqual(t, round.Structure.SCPID, "SCP-TEST")
}

func TestPipelineStage_String(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		stage PipelineStage
		want  string
	}{
		{StageResearcher, "researcher"},
		{StageStructurer, "structurer"},
		{StageWriter, "writer"},
		{StagePolisher, "polisher"},
		{StagePostWriterCritic, "post_writer_critic"},
		{StageVisualBreakdowner, "visual_breakdowner"},
		{StageReviewer, "reviewer"},
		{StageCritic, "critic"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			testutil.AssertEqual(t, tc.stage.String(), tc.want)
		})
	}
}

func TestPipelineStage_Count(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertEqual(t, int(phaseAStageCount), 8)
}

func TestPipelineStage_DomainStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		ps   PipelineStage
		want domain.Stage
	}{
		{StageResearcher, domain.StageResearch},
		{StageStructurer, domain.StageStructure},
		{StageWriter, domain.StageWrite},
		{StagePolisher, domain.StageWrite},
		{StagePostWriterCritic, domain.StageCritic},
		{StageVisualBreakdowner, domain.StageVisualBreak},
		{StageReviewer, domain.StageReview},
		{StageCritic, domain.StageCritic},
	}
	for _, tc := range tests {
		t.Run(tc.ps.String(), func(t *testing.T) {
			testutil.AssertEqual(t, tc.ps.DomainStage(), tc.want)
		})
	}
}

func TestPipelineStage_DomainStage_OutOfRangePanics(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for out-of-range PipelineStage, got nil")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value is not string: %T %v", r, r)
		}
		if !strings.Contains(msg, "out-of-range") {
			t.Errorf("panic message missing 'out-of-range': %s", msg)
		}
	}()

	bad := PipelineStage(99)
	_ = bad.DomainStage()
}
