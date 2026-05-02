package agents

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
				{Index: 0, Description: "Beat 0", EmotionalTone: "mystery"},
				{Index: 1, Description: "Beat 1", EmotionalTone: "horror"},
				{Index: 2, Description: "Beat 2", EmotionalTone: "tension"},
				{Index: 3, Description: "Beat 3", EmotionalTone: "revelation"},
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
	}
	wantBudgets := [4]int{1, 1, 1, 1}
	var sum int
	for i, act := range state.Structure.Acts {
		testutil.AssertEqual(t, act.SceneBudget, wantBudgets[i])
		sum += act.SceneBudget
	}
	testutil.AssertEqual(t, sum, 4)
}

func TestStructurer_Run_Validates_SampleFixture(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	state := &PipelineState{SCPID: "SCP-TEST"}
	researcher := NewResearcher(
		NewFilesystemCorpus(filepath.Join(testutil.ProjectRoot(t), "testdata", "fixtures", "corpus")),
		mustValidator(t, "researcher_output.schema.json"),
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

func TestStructurer_Run_BeatAssignmentModulo(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	beats := make([]domain.DramaticBeat, 8)
	for i := range beats {
		beats[i] = domain.DramaticBeat{Index: i, Description: "Beat"}
	}
	state := &PipelineState{Research: &domain.ResearcherOutput{SCPID: "SCP-TEST", DramaticBeats: beats, SourceVersion: domain.SourceVersionV1}}
	err := NewStructurer(mustValidator(t, "structurer_output.schema.json"))(context.Background(), state)
	if err != nil {
		t.Fatalf("Structurer: %v", err)
	}
	want := [][]int{{0, 4}, {1, 5}, {2, 6}, {3, 7}}
	for i, act := range state.Structure.Acts {
		for j, beatID := range want[i] {
			testutil.AssertEqual(t, act.DramaticBeatIDs[j], beatID)
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

func TestDistributeSceneBudget_Target10(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	wantBudget(t, 10, [4]int{2, 3, 4, 1})
}
func TestDistributeSceneBudget_Target8(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	wantBudget(t, 8, [4]int{1, 3, 3, 1})
}
func TestDistributeSceneBudget_Target9(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	wantBudget(t, 9, [4]int{1, 3, 4, 1})
}
func TestDistributeSceneBudget_Target11(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	wantBudget(t, 11, [4]int{2, 3, 4, 2})
}
func TestDistributeSceneBudget_Target12(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	wantBudget(t, 12, [4]int{2, 3, 5, 2})
}

func TestDistributeSceneBudget_MinimumOne(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	for target := 8; target <= 12; target++ {
		got := distributeSceneBudget(target)
		for _, v := range got {
			if v < 1 {
				t.Fatalf("target %d produced allocation < 1: %v", target, got)
			}
		}
	}
}

func TestDistributeSceneBudget_SumsToTarget(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	for target := 8; target <= 12; target++ {
		got := distributeSceneBudget(target)
		var sum int
		for _, v := range got {
			sum += v
		}
		testutil.AssertEqual(t, sum, target)
	}
}

func TestDistributeSceneBudget_Deterministic(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	want := distributeSceneBudget(10)
	for i := 0; i < 100; i++ {
		if got := distributeSceneBudget(10); got != want {
			t.Fatalf("iteration %d: got %v want %v", i, got, want)
		}
	}
}

func TestDistributeSceneBudget_TieBreaker(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	wantBudget(t, 10, [4]int{2, 3, 4, 1})
}

func wantBudget(t *testing.T, target int, want [4]int) {
	t.Helper()
	got := distributeSceneBudget(target)
	if got != want {
		t.Fatalf("target %d: got %v want %v", target, got, want)
	}
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
		DramaticBeats: []domain.DramaticBeat{
			{Index: 0, Description: "Beat 0", EmotionalTone: "mystery"},
			{Index: 1, Description: "Beat 1", EmotionalTone: "horror"},
			{Index: 2, Description: "Beat 2", EmotionalTone: "tension"},
			{Index: 3, Description: "Beat 3", EmotionalTone: "revelation"},
			{Index: 4, Description: "Beat 4", EmotionalTone: "mystery"},
			{Index: 5, Description: "Beat 5", EmotionalTone: "horror"},
		},
		SourceVersion: domain.SourceVersionV1,
	}
}
