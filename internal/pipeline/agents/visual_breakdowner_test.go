package agents

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeSceneDurationEstimator struct {
	values  map[int]float64
	fallback float64
}

func (f fakeSceneDurationEstimator) Estimate(scene domain.NarrationScene) float64 {
	if v, ok := f.values[scene.SceneNum]; ok {
		return v
	}
	if f.fallback > 0 {
		return f.fallback
	}
	return 7.0
}

func uniformEstimator(duration float64) fakeSceneDurationEstimator {
	return fakeSceneDurationEstimator{fallback: duration}
}

func shotResponse(sceneNum int, shots ...visualBreakdownResponseShot) string {
	fragments := make([]string, 0, len(shots))
	for _, s := range shots {
		fragments = append(fragments, fmt.Sprintf(`{"visual_descriptor":%q,"transition":%q}`, s.VisualDescriptor, s.Transition))
	}
	return fmt.Sprintf(`{"scene_num":%d,"shots":[%s]}`, sceneNum, strings.Join(fragments, ","))
}

func eightSceneSingleShotResponses() []string {
	responses := make([]string, 0, 8)
	for i := 1; i <= 8; i++ {
		responses = append(responses, shotResponse(i, visualBreakdownResponseShot{
			VisualDescriptor: fmt.Sprintf("scene %d descriptor", i),
			Transition:       domain.TransitionKenBurns,
		}))
	}
	return responses
}

func TestVisualBreakdowner_Run_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &sequenceTextGenerator{responses: eightSceneSingleShotResponses()}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	if state.VisualBreakdown == nil {
		t.Fatal("expected visual breakdown output")
	}
	testutil.AssertEqual(t, state.VisualBreakdown.SourceVersion, domain.VisualBreakdownSourceVersionV1)
	testutil.AssertEqual(t, state.VisualBreakdown.Metadata.PromptTemplate, "03_5_visual_breakdown.md")
	testutil.AssertEqual(t, state.VisualBreakdown.Metadata.ShotFormulaVersion, domain.ShotFormulaVersionV1)
	testutil.AssertEqual(t, state.VisualBreakdown.Metadata.VisualBreakdownModel, sampleWriterConfig().Model)
	testutil.AssertEqual(t, state.VisualBreakdown.Metadata.VisualBreakdownProvider, sampleWriterConfig().Provider)
	testutil.AssertEqual(t, len(state.VisualBreakdown.Scenes), 8)
}

func TestVisualBreakdowner_Run_MetadataFromConfigNotLastResponse(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Generator drifts model/provider per scene — metadata must still reflect the config.
	drifting := &driftingTextGenerator{responses: eightSceneSingleShotResponses()}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(drifting, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, state.VisualBreakdown.Metadata.VisualBreakdownModel, sampleWriterConfig().Model)
	testutil.AssertEqual(t, state.VisualBreakdown.Metadata.VisualBreakdownProvider, sampleWriterConfig().Provider)
}

func TestVisualBreakdowner_Run_CallsGeneratorPerScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &sequenceTextGenerator{responses: eightSceneSingleShotResponses()}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 8)
}

func TestVisualBreakdowner_Run_UsesShotCountFormula(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Each boundary tier: scene N has duration dᴺ which maps to shot count cᴺ.
	tiers := []struct {
		sceneNum int
		duration float64
		shots    int
	}{
		{1, 7.5, 1},
		{2, 12.0, 2},
		{3, 20.0, 3},
		{4, 35.0, 4},
		{5, 50.0, 5},
		{6, 7.5, 1},
		{7, 12.0, 2},
		{8, 20.0, 3},
	}

	responses := make([]string, 0, len(tiers))
	for _, tier := range tiers {
		shots := make([]visualBreakdownResponseShot, 0, tier.shots)
		for i := 0; i < tier.shots; i++ {
			shots = append(shots, visualBreakdownResponseShot{
				VisualDescriptor: fmt.Sprintf("scene %d shot %d", tier.sceneNum, i+1),
				Transition:       domain.TransitionKenBurns,
			})
		}
		responses = append(responses, shotResponse(tier.sceneNum, shots...))
	}

	estimator := fakeSceneDurationEstimator{values: map[int]float64{}}
	for _, tier := range tiers {
		estimator.values[tier.sceneNum] = tier.duration
	}

	gen := &sequenceTextGenerator{responses: responses}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), estimator)(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	for i, tier := range tiers {
		got := state.VisualBreakdown.Scenes[i].ShotCount
		if got != tier.shots {
			t.Fatalf("scene %d duration=%v want shot count=%d got=%d", tier.sceneNum, tier.duration, tier.shots, got)
		}
		if len(state.VisualBreakdown.Scenes[i].Shots) != tier.shots {
			t.Fatalf("scene %d shots len=%d want=%d", tier.sceneNum, len(state.VisualBreakdown.Scenes[i].Shots), tier.shots)
		}
	}
}

func TestVisualBreakdowner_Run_PrefixesFrozenDescriptor_EveryShot(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Mix of single-shot and multi-shot scenes to prove every shot is prefixed.
	durations := map[int]float64{1: 7, 2: 7, 3: 20, 4: 7, 5: 35, 6: 7, 7: 7, 8: 7}
	responses := []string{
		shotResponse(1, visualBreakdownResponseShot{VisualDescriptor: "alpha", Transition: domain.TransitionKenBurns}),
		shotResponse(2, visualBreakdownResponseShot{VisualDescriptor: "beta", Transition: domain.TransitionKenBurns}),
		shotResponse(3,
			visualBreakdownResponseShot{VisualDescriptor: "gamma-a", Transition: domain.TransitionKenBurns},
			visualBreakdownResponseShot{VisualDescriptor: "gamma-b", Transition: domain.TransitionCrossDissolve},
			visualBreakdownResponseShot{VisualDescriptor: "gamma-c", Transition: domain.TransitionHardCut},
		),
		shotResponse(4, visualBreakdownResponseShot{VisualDescriptor: "delta", Transition: domain.TransitionKenBurns}),
		shotResponse(5,
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-a", Transition: domain.TransitionKenBurns},
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-b", Transition: domain.TransitionKenBurns},
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-c", Transition: domain.TransitionKenBurns},
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-d", Transition: domain.TransitionKenBurns},
		),
		shotResponse(6, visualBreakdownResponseShot{VisualDescriptor: "zeta", Transition: domain.TransitionKenBurns}),
		shotResponse(7, visualBreakdownResponseShot{VisualDescriptor: "eta", Transition: domain.TransitionKenBurns}),
		shotResponse(8, visualBreakdownResponseShot{VisualDescriptor: "theta", Transition: domain.TransitionKenBurns}),
	}

	gen := &sequenceTextGenerator{responses: responses}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), fakeSceneDurationEstimator{values: durations})(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	totalShots := 0
	for _, scene := range state.VisualBreakdown.Scenes {
		for _, shot := range scene.Shots {
			totalShots++
			if !strings.HasPrefix(shot.VisualDescriptor, state.VisualBreakdown.FrozenDescriptor) {
				t.Fatalf("scene %d shot %d missing frozen prefix: %s", scene.SceneNum, shot.ShotIndex, shot.VisualDescriptor)
			}
		}
	}
	if totalShots != 13 { // 1+1+3+1+4+1+1+1 under the current duration→shot formula
		t.Fatalf("expected 13 total shots across 8 scenes, got %d", totalShots)
	}
}

func TestVisualBreakdowner_Run_RejectsWrongShotCount(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"scene_num":1,"shots":[{"visual_descriptor":"only one","transition":"ken_burns"}]}`,
	}}}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), fakeSceneDurationEstimator{values: map[int]float64{1: 20}, fallback: 7})(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestVisualBreakdowner_Run_RejectsInvalidTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"scene_num":1,"shots":[{"visual_descriptor":"shot","transition":"zoom"}]}`,
	}}}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestVisualBreakdowner_Run_RejectsEmptyScenes(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := sampleVisualBreakdownState(0)
	state.Narration = &domain.NarrationScript{SCPID: "SCP-TEST", Title: "SCP-TEST", SourceVersion: domain.NarrationSourceVersionV1}
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	testutil.AssertEqual(t, gen.calls, 0)
}

func TestVisualBreakdowner_Run_RejectsDuplicateSceneNum(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{}
	state := sampleVisualBreakdownState(8)
	state.Narration.Scenes[1].SceneNum = 1 // collide with scene[0]
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestVisualBreakdowner_Run_InitializesEmptyShotOverrides(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &sequenceTextGenerator{responses: eightSceneSingleShotResponses()}
	state := sampleVisualBreakdownState(8)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	if state.VisualBreakdown.ShotOverrides == nil || len(state.VisualBreakdown.ShotOverrides) != 0 {
		t.Fatalf("expected empty shot overrides map, got %#v", state.VisualBreakdown.ShotOverrides)
	}
}

func TestVisualBreakdowner_Run_DoesNotMutateStateOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: "not-json"}}}
	state := sampleVisualBreakdownState(8)
	// Pre-populate with a distinctive sentinel so a full overwrite would be detectable.
	sentinel := &domain.VisualBreakdownOutput{SCPID: "SENTINEL"}
	state.VisualBreakdown = sentinel
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err == nil {
		t.Fatal("expected error")
	}
	if state.VisualBreakdown != sentinel {
		t.Fatal("visual breakdowner mutated state on failure")
	}
	if state.VisualBreakdown.SCPID != "SENTINEL" {
		t.Fatalf("sentinel mutated in place: %#v", state.VisualBreakdown)
	}
}

type sequenceTextGenerator struct {
	mu        sync.Mutex
	responses []string
	calls     int
}

// Generate routes by parsing the "Scene N" header from the rendered prompt
// when it matches the visual_breakdowner template, so parallel fan-out gets
// the correct scene response. Falls back to call-order indexing for prompts
// that don't carry a scene_num token (preserved for any non-visual tests).
func (s *sequenceTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sceneNum, ok := parseScenePromptNum(req.Prompt); ok {
		idx := sceneNum - 1
		if idx < 0 || idx >= len(s.responses) {
			return domain.TextResponse{}, fmt.Errorf("no response for scene_num=%d", sceneNum)
		}
		s.calls++
		return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: s.responses[idx], Model: req.Model, Provider: "openai"}}, nil
	}
	if s.calls >= len(s.responses) {
		return domain.TextResponse{}, fmt.Errorf("unexpected extra call")
	}
	resp := s.responses[s.calls]
	s.calls++
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: resp, Model: req.Model, Provider: "openai"}}, nil
}

// parseScenePromptNum extracts the scene number from the "Scene N\n..."
// header rendered by the visual_breakdowner prompt template.
func parseScenePromptNum(prompt string) (int, bool) {
	const prefix = "Scene "
	if !strings.HasPrefix(prompt, prefix) {
		return 0, false
	}
	rest := prompt[len(prefix):]
	end := strings.IndexAny(rest, "\n\r ")
	if end <= 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

type driftingTextGenerator struct {
	mu        sync.Mutex
	responses []string
	calls     int
}

func (d *driftingTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	driftN := d.calls
	if sceneNum, ok := parseScenePromptNum(req.Prompt); ok {
		idx := sceneNum - 1
		if idx < 0 || idx >= len(d.responses) {
			return domain.TextResponse{}, fmt.Errorf("no response for scene_num=%d", sceneNum)
		}
		return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
			Content:  d.responses[idx],
			Model:    fmt.Sprintf("drift-model-%d", driftN),
			Provider: fmt.Sprintf("drift-provider-%d", driftN),
		}}, nil
	}
	if driftN > len(d.responses) {
		return domain.TextResponse{}, fmt.Errorf("unexpected extra call")
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content:  d.responses[driftN-1],
		Model:    fmt.Sprintf("drift-model-%d", driftN),
		Provider: fmt.Sprintf("drift-provider-%d", driftN),
	}}, nil
}

func sampleVisualBreakdownState(sceneCount int) *PipelineState {
	state := sampleWriterState()
	state.Narration = sampleNarrationScenes(sceneCount)
	return state
}

func sampleNarrationScenes(sceneCount int) *domain.NarrationScript {
	var script domain.NarrationScript
	script.SCPID = "SCP-TEST"
	script.Title = "SCP-TEST"
	script.SourceVersion = domain.NarrationSourceVersionV1
	for i := 1; i <= sceneCount; i++ {
		script.Scenes = append(script.Scenes, domain.NarrationScene{
			SceneNum:          i,
			ActID:             actForScene(i),
			Narration:         fmt.Sprintf("scene %d narration", i),
			Location:          "transit platform",
			CharactersPresent: []string{"SCP-TEST"},
			ColorPalette:      "gray",
			Atmosphere:        "tense",
		})
	}
	return &script
}

func sampleVisualAssets() PromptAssets {
	return PromptAssets{
		VisualBreakdownTemplate: "Scene {scene_num}\n{location}\n{characters_present}\n{color_palette}\n{atmosphere}\n{scp_visual_reference}\n{narration}\n{frozen_descriptor}\n{estimated_tts_duration_s}\n{shot_count}\n{format_guide}",
		ReviewerTemplate:        "unused",
		WriterTemplate:          "unused",
		CriticTemplate:          "unused",
		FormatGuide:             "guide",
	}
}

func actForScene(sceneNum int) string {
	switch {
	case sceneNum <= 2:
		return domain.ActIncident
	case sceneNum <= 5:
		return domain.ActMystery
	case sceneNum <= 8:
		return domain.ActRevelation
	default:
		return domain.ActUnresolved
	}
}
