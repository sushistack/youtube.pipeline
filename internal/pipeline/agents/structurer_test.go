package agents

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestStructurer_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{
		Research: &domain.ResearcherOutput{
			SCPID: "SCP-TEST",
			DramaticBeats: []domain.DramaticBeat{
				{Index: 0, Description: "Beat 0", EmotionalTone: "mystery", RoleSuggestion: domain.RoleHook},
				{Index: 1, Description: "Beat 1", EmotionalTone: "horror", RoleSuggestion: domain.RoleTension},
				{Index: 2, Description: "Beat 2", EmotionalTone: "tension", RoleSuggestion: domain.RoleReveal},
				{Index: 3, Description: "Beat 3", EmotionalTone: "revelation", RoleSuggestion: domain.RoleBridge},
			},
			SourceVersion: domain.SourceVersionV1,
		},
	}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	if state.Structure == nil {
		t.Fatal("expected structure output")
	}
	for i, act := range state.Structure.Acts {
		testutil.AssertEqual(t, act.ID, domain.ActOrder[i])
		testutil.AssertEqual(t, act.Role, domain.RoleForAct[act.ID])
	}
	// One beat per role × per-act multipliers {1, 2, 3, 1} → budgets [1, 2, 3, 1].
	wantBudgets := [4]int{1, 2, 3, 1}
	var sum int
	for i, act := range state.Structure.Acts {
		testutil.AssertEqual(t, act.SceneBudget, wantBudgets[i])
		sum += act.SceneBudget
	}
	testutil.AssertEqual(t, sum, 7)
	testutil.AssertEqual(t, state.Structure.TargetSceneCount, 7)
}

func TestStructurer_Run_Validates_SampleFixture(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	gen := newQueuedGen(queuedResponse{content: happyClassifierResponse(scpTestRoles...)})
	researcher := NewResearcher(
		NewFilesystemCorpus(filepath.Join(testutil.ProjectRoot(t), "testdata", "fixtures", "corpus")),
		mustValidator(t, "researcher_output.schema.json"),
		gen,
		sampleClassifierConfig(),
		sampleClassifierAssets(),
	)
	structurer := NewStructurer(mustValidator(t, "structurer_output.schema.json"))
	if err := researcher(context.Background(), state); err != nil {
		t.Fatalf("Researcher: %v", err)
	}
	if err := structurer(context.Background(), state); err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	raw, err := json.Marshal(state.Structure)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	testutil.AssertJSONEq(t, string(raw), string(testutil.LoadFixture(t, filepath.Join("contracts", "structurer_output.sample.json"))))
}

func TestStructurer_Run_NilResearch(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if state.Structure != nil {
		t.Fatalf("unexpected structure: %+v", state.Structure)
	}
}

func TestStructurer_Run_InsufficientBeats(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{
		Research: &domain.ResearcherOutput{
			SCPID: "SCP-TEST",
			DramaticBeats: []domain.DramaticBeat{
				{Index: 0}, {Index: 1}, {Index: 2},
			},
			SourceVersion: domain.SourceVersionV1,
		},
	}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// TestStructurer_Run_BeatAssignmentByRole asserts the role-based assignment:
// every beat lands in the act whose role matches its RoleSuggestion, in
// original beat-index order. Multiple beats per role all flow to the same act.
func TestStructurer_Run_BeatAssignmentByRole(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	beats := []domain.DramaticBeat{
		{Index: 0, Description: "Beat 0", RoleSuggestion: domain.RoleHook},
		{Index: 1, Description: "Beat 1", RoleSuggestion: domain.RoleTension},
		{Index: 2, Description: "Beat 2", RoleSuggestion: domain.RoleReveal},
		{Index: 3, Description: "Beat 3", RoleSuggestion: domain.RoleBridge},
		{Index: 4, Description: "Beat 4", RoleSuggestion: domain.RoleHook},
		{Index: 5, Description: "Beat 5", RoleSuggestion: domain.RoleTension},
		{Index: 6, Description: "Beat 6", RoleSuggestion: domain.RoleReveal},
		{Index: 7, Description: "Beat 7", RoleSuggestion: domain.RoleBridge},
	}
	state := &PipelineState{Research: &domain.ResearcherOutput{SCPID: "SCP-TEST", DramaticBeats: beats, SourceVersion: domain.SourceVersionV1}}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	want := [][]int{{0, 4}, {1, 5}, {2, 6}, {3, 7}}
	for i, act := range state.Structure.Acts {
		testutil.AssertEqual(t, len(act.DramaticBeatIDs), len(want[i]))
		for j, beatID := range want[i] {
			testutil.AssertEqual(t, act.DramaticBeatIDs[j], beatID)
		}
	}
	// Per-act multipliers {1, 2, 3, 1} × beat counts {2, 2, 2, 2} = {2, 4, 6, 2}.
	wantBudgets := [4]int{2, 4, 6, 2}
	for i, act := range state.Structure.Acts {
		testutil.AssertEqual(t, act.SceneBudget, wantBudgets[i])
	}
}

// TestStructurer_Run_MissingRoleSuggestion rejects beats whose RoleSuggestion
// is empty (researcher contract violation — should never happen post-classifier).
func TestStructurer_Run_MissingRoleSuggestion(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{
		Research: &domain.ResearcherOutput{
			SCPID: "SCP-TEST",
			DramaticBeats: []domain.DramaticBeat{
				{Index: 0, RoleSuggestion: domain.RoleHook},
				{Index: 1, RoleSuggestion: domain.RoleTension},
				{Index: 2, RoleSuggestion: ""}, // missing
				{Index: 3, RoleSuggestion: domain.RoleBridge},
			},
			SourceVersion: domain.SourceVersionV1,
		},
	}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "empty role_suggestion") {
		t.Fatalf("expected empty role_suggestion error, got %v", err)
	}
	if state.Structure != nil {
		t.Fatalf("state mutated on validation failure: %+v", state.Structure)
	}
}

// TestStructurer_Run_RoleAbsentFromBeats rejects when the researcher's
// classification left an entire act with no beats (which the classifier
// validator should already reject — defense in depth).
func TestStructurer_Run_RoleAbsentFromBeats(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// All beats classified as hook/tension/reveal — bridge has no beats.
	beats := []domain.DramaticBeat{
		{Index: 0, RoleSuggestion: domain.RoleHook},
		{Index: 1, RoleSuggestion: domain.RoleTension},
		{Index: 2, RoleSuggestion: domain.RoleReveal},
		{Index: 3, RoleSuggestion: domain.RoleHook},
	}
	state := &PipelineState{Research: &domain.ResearcherOutput{SCPID: "SCP-TEST", DramaticBeats: beats, SourceVersion: domain.SourceVersionV1}}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "act unresolved has no beats") {
		t.Fatalf("expected unresolved-empty error, got %v", err)
	}
}

func TestStructurer_Run_SynopsisCarriesRolePrefix(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{Research: sampleResearchForStructurer()}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	for _, act := range state.Structure.Acts {
		role := domain.RoleForAct[act.ID]
		label := domain.RoleKoreanLabel[role]
		want := "[ROLE: " + label + "]"
		if !strings.HasPrefix(act.Synopsis, want) {
			t.Fatalf("act %s synopsis missing prefix %q: %q", act.ID, want, act.Synopsis)
		}
		for _, kp := range act.KeyPoints {
			if !strings.HasPrefix(kp, want) {
				t.Fatalf("act %s key_point missing prefix %q: %q", act.ID, want, kp)
			}
		}
	}
}

func TestStructurer_Run_SynopsisDeterministic(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{Research: sampleResearchForStructurer()}
	structurer := NewStructurer(mustValidator(t, "structurer_output.schema.json"))
	if err := structurer(context.Background(), state); err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	first, err := json.Marshal(state.Structure)
	if err != nil {
		t.Fatalf("marshal #1: %v", err)
	}
	state.Structure = nil
	if err := structurer(context.Background(), state); err != nil {
		t.Fatalf("Structurer #2: %v", err)
	}
	second, err := json.Marshal(state.Structure)
	if err != nil {
		t.Fatalf("marshal #2: %v", err)
	}
	testutil.AssertEqual(t, string(first), string(second))
}

func TestStructurer_Run_ActDurationRatioSum(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{Research: sampleResearchForStructurer()}
	if err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state); err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	var sum float64
	for _, act := range state.Structure.Acts {
		sum += act.DurationRatio
	}
	testutil.AssertFloatNear(t, sum, 1.0, 1e-9)
}

func TestStructurer_Run_SourceVersionPropagates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{Research: sampleResearchForStructurer()}
	if err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state); err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	testutil.AssertEqual(t, state.Structure.SourceVersion, state.Research.SourceVersion)
}

func sampleResearchForStructurer() *domain.ResearcherOutput {
	return &domain.ResearcherOutput{
		SCPID: "SCP-TEST",
		VisualIdentity: domain.VisualIdentity{
			Appearance:             "Concrete sentinel",
			DistinguishingFeatures: []string{"Obsidian eyes"},
			EnvironmentSetting:     "Transit vault",
			KeyVisualMoments:       []string{"Blink"},
		},
		// Six beats balanced so every role appears: 2 hook, 2 tension, 1 reveal,
		// 1 bridge. Per-act multipliers {1, 2, 3, 1} × beat counts {2, 2, 1, 1}
		// produce scene budgets {2, 4, 3, 1} = 10 total — close to today's
		// 8-scene baseline for a 6-beat corpus.
		DramaticBeats: []domain.DramaticBeat{
			{Index: 0, Description: "Beat 0", EmotionalTone: "mystery", RoleSuggestion: domain.RoleHook},
			{Index: 1, Description: "Beat 1", EmotionalTone: "horror", RoleSuggestion: domain.RoleTension},
			{Index: 2, Description: "Beat 2", EmotionalTone: "tension", RoleSuggestion: domain.RoleReveal},
			{Index: 3, Description: "Beat 3", EmotionalTone: "revelation", RoleSuggestion: domain.RoleBridge},
			{Index: 4, Description: "Beat 4", EmotionalTone: "mystery", RoleSuggestion: domain.RoleHook},
			{Index: 5, Description: "Beat 5", EmotionalTone: "horror", RoleSuggestion: domain.RoleTension},
		},
		SourceVersion: domain.SourceVersionV1,
	}
}
