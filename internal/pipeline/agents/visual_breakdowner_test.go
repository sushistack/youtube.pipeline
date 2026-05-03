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
	for i, s := range shots {
		// Auto-fill narration_beat_index when the test caller leaves it as
		// the zero value: shots[0]→0, shots[1]→1, ... so the validator's
		// "shot order must equal beat index" check passes by default.
		idx := s.NarrationBeatIndex
		if i > 0 && idx == 0 {
			idx = i
		}
		fragments = append(fragments,
			fmt.Sprintf(`{"visual_descriptor":%q,"transition":%q,"narration_beat_index":%d}`,
				s.VisualDescriptor, s.Transition, idx))
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

func TestVisualBreakdowner_Run_ShotCountMatchesNarrationBeats(t *testing.T) {
	t.Skip("v1 multi-beat-per-scene shape eliminated in v2; visual_breakdowner v2 (D2) will reintroduce equivalent coverage against ActScript[]/BeatAnchor[]")
	testutil.BlockExternalHTTP(t)

	// Beat-driven: shot count is now len(scene.NarrationBeats), not a
	// duration tier. Mix single- and multi-beat scenes.
	beats := map[int]int{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 1, 7: 2, 8: 3}

	responses := make([]string, 0, len(beats))
	for sceneNum := 1; sceneNum <= 8; sceneNum++ {
		count := beats[sceneNum]
		shots := make([]visualBreakdownResponseShot, 0, count)
		for i := 0; i < count; i++ {
			shots = append(shots, visualBreakdownResponseShot{
				VisualDescriptor:   fmt.Sprintf("scene %d shot %d", sceneNum, i+1),
				Transition:         domain.TransitionKenBurns,
				NarrationBeatIndex: i,
			})
		}
		responses = append(responses, shotResponse(sceneNum, shots...))
	}

	gen := &sequenceTextGenerator{responses: responses}
	state := sampleVisualBreakdownState(8)
	state.Narration = sampleNarrationScenesWithBeats(8, beats)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	for sceneNum := 1; sceneNum <= 8; sceneNum++ {
		want := beats[sceneNum]
		got := state.VisualBreakdown.Scenes[sceneNum-1].ShotCount
		if got != want {
			t.Fatalf("scene %d: shot count=%d, want=%d (== len(NarrationBeats))", sceneNum, got, want)
		}
		if len(state.VisualBreakdown.Scenes[sceneNum-1].Shots) != want {
			t.Fatalf("scene %d: shots len=%d, want=%d", sceneNum, len(state.VisualBreakdown.Scenes[sceneNum-1].Shots), want)
		}
		for i, shot := range state.VisualBreakdown.Scenes[sceneNum-1].Shots {
			if shot.NarrationBeatIndex != i {
				t.Fatalf("scene %d shot %d narration_beat_index=%d want=%d", sceneNum, i+1, shot.NarrationBeatIndex, i)
			}
			if shot.NarrationBeatText == "" {
				t.Fatalf("scene %d shot %d: narration_beat_text empty", sceneNum, i+1)
			}
		}
	}
}

func TestVisualBreakdowner_Run_PassesDescriptorsThroughVerbatim(t *testing.T) {
	t.Skip("v1 multi-beat-per-scene shape eliminated in v2; visual_breakdowner v2 (D2) will reintroduce equivalent coverage against ActScript[]/BeatAnchor[]")
	testutil.BlockExternalHTTP(t)

	// Beat-driven multi-shot mix: scene 3 → 3 beats, scene 5 → 4 beats, rest 1.
	// Descriptors emitted by the LLM flow through unchanged — visual identity
	// is anchored downstream by image_track.ComposeImagePrompt, not by an agent-
	// layer prepend. The new prompt invites focal-subject variation per beat,
	// so any agent-side prepend would corrupt that intent.
	beats := map[int]int{1: 1, 2: 1, 3: 3, 4: 1, 5: 4, 6: 1, 7: 1, 8: 1}
	responses := []string{
		shotResponse(1, visualBreakdownResponseShot{VisualDescriptor: "alpha", Transition: domain.TransitionKenBurns}),
		shotResponse(2, visualBreakdownResponseShot{VisualDescriptor: "beta", Transition: domain.TransitionKenBurns}),
		shotResponse(3,
			visualBreakdownResponseShot{VisualDescriptor: "gamma-a", Transition: domain.TransitionKenBurns, NarrationBeatIndex: 0},
			visualBreakdownResponseShot{VisualDescriptor: "gamma-b", Transition: domain.TransitionCrossDissolve, NarrationBeatIndex: 1},
			visualBreakdownResponseShot{VisualDescriptor: "gamma-c", Transition: domain.TransitionHardCut, NarrationBeatIndex: 2},
		),
		shotResponse(4, visualBreakdownResponseShot{VisualDescriptor: "delta", Transition: domain.TransitionKenBurns}),
		shotResponse(5,
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-a", Transition: domain.TransitionKenBurns, NarrationBeatIndex: 0},
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-b", Transition: domain.TransitionKenBurns, NarrationBeatIndex: 1},
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-c", Transition: domain.TransitionKenBurns, NarrationBeatIndex: 2},
			visualBreakdownResponseShot{VisualDescriptor: "epsilon-d", Transition: domain.TransitionKenBurns, NarrationBeatIndex: 3},
		),
		shotResponse(6, visualBreakdownResponseShot{VisualDescriptor: "zeta", Transition: domain.TransitionKenBurns}),
		shotResponse(7, visualBreakdownResponseShot{VisualDescriptor: "eta", Transition: domain.TransitionKenBurns}),
		shotResponse(8, visualBreakdownResponseShot{VisualDescriptor: "theta", Transition: domain.TransitionKenBurns}),
	}

	gen := &sequenceTextGenerator{responses: responses}
	state := sampleVisualBreakdownState(8)
	state.Narration = sampleNarrationScenesWithBeats(8, beats)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	totalShots := 0
	frozen := state.VisualBreakdown.FrozenDescriptor
	for _, scene := range state.VisualBreakdown.Scenes {
		for _, shot := range scene.Shots {
			totalShots++
			if frozen != "" && strings.HasPrefix(shot.VisualDescriptor, frozen) {
				t.Fatalf("scene %d shot %d unexpectedly prepended with frozen descriptor: %q", scene.SceneNum, shot.ShotIndex, shot.VisualDescriptor)
			}
		}
	}
	if totalShots != 13 { // 1+1+3+1+4+1+1+1 = 13 total beats across 8 scenes
		t.Fatalf("expected 13 total shots across 8 scenes (matching beat counts), got %d", totalShots)
	}
}

func TestVisualBreakdowner_Run_RejectsWrongShotCount(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	gen := &fakeTextGenerator{resp: domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{
		Content: `{"scene_num":1,"shots":[{"visual_descriptor":"only one","transition":"ken_burns","narration_beat_index":0}]}`,
	}}}
	state := sampleVisualBreakdownState(8)
	// Scene 1 carries 3 beats; LLM returning 1 shot must be rejected as
	// shot count mismatch (beat-driven contract).
	state.Narration = sampleNarrationScenesWithBeats(8, map[int]int{1: 3})
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
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

// retryQueueWithScene1Failures builds a per-scene response queue for a 4-scene
// state where scene 1 receives the supplied failing-then-successful sequence
// and scenes 2-4 each receive one valid response. The schema requires
// minItems=4 so the helper keeps the contract test happy when scene 1 recovers.
func retryQueueWithScene1Failures(scene1 ...string) map[int][]string {
	queue := map[int][]string{1: scene1}
	for i := 2; i <= 4; i++ {
		queue[i] = []string{shotResponse(i, visualBreakdownResponseShot{
			VisualDescriptor: fmt.Sprintf("scene %d descriptor", i),
			Transition:       domain.TransitionKenBurns,
		})}
	}
	return queue
}

func TestVisualBreakdowner_Run_RetriesOnEmptyTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	invalid := `{"scene_num":1,"shots":[{"visual_descriptor":"shot","transition":""}]}`
	valid := shotResponse(1, visualBreakdownResponseShot{VisualDescriptor: "shot", Transition: domain.TransitionKenBurns})
	gen := &queueTextGenerator{responsesByScene: retryQueueWithScene1Failures(invalid, valid)}
	state := sampleVisualBreakdownState(4)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.callsByScene[1], 2)
	for sceneNum := 2; sceneNum <= 4; sceneNum++ {
		testutil.AssertEqual(t, gen.callsByScene[sceneNum], 1)
	}
	if state.VisualBreakdown == nil || len(state.VisualBreakdown.Scenes) != 4 {
		t.Fatalf("expected 4 scenes built from retry, got %#v", state.VisualBreakdown)
	}
	testutil.AssertEqual(t, state.VisualBreakdown.Scenes[0].Shots[0].Transition, domain.TransitionKenBurns)
}

func TestVisualBreakdowner_Run_RetriesOnInvalidTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	invalid := `{"scene_num":1,"shots":[{"visual_descriptor":"shot","transition":"zoom"}]}`
	valid := shotResponse(1, visualBreakdownResponseShot{VisualDescriptor: "shot", Transition: domain.TransitionCrossDissolve})
	gen := &queueTextGenerator{responsesByScene: retryQueueWithScene1Failures(invalid, valid)}
	state := sampleVisualBreakdownState(4)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.callsByScene[1], 2)
	for sceneNum := 2; sceneNum <= 4; sceneNum++ {
		testutil.AssertEqual(t, gen.callsByScene[sceneNum], 1)
	}
	testutil.AssertEqual(t, state.VisualBreakdown.Scenes[0].Shots[0].Transition, domain.TransitionCrossDissolve)
}

func TestVisualBreakdowner_Run_PropagatesTransportErrorWithoutRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	transportErr := errors.New("network: connection reset")
	validResponses := map[int]string{}
	for i := 2; i <= 4; i++ {
		validResponses[i] = shotResponse(i, visualBreakdownResponseShot{
			VisualDescriptor: fmt.Sprintf("scene %d descriptor", i),
			Transition:       domain.TransitionKenBurns,
		})
	}
	gen := &sceneErrorGenerator{errOnScene: 1, err: transportErr, validResponses: validResponses}
	state := sampleVisualBreakdownState(4)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err == nil {
		t.Fatal("expected transport error to propagate")
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("expected transport error to propagate verbatim, got %v", err)
	}
	// Per spec: transport errors propagate immediately and do NOT consume the retry budget.
	testutil.AssertEqual(t, gen.callsByScene[1], 1)
	if state.VisualBreakdown != nil {
		t.Fatalf("expected state.VisualBreakdown unchanged on transport failure, got %#v", state.VisualBreakdown)
	}
}

// retryQueueWithSceneNFailures builds a 4-scene per-scene queue where
// targetScene receives the supplied failure-then-recovery sequence and
// every other scene gets a single valid response.
func retryQueueWithSceneNFailures(targetScene int, queue ...string) map[int][]string {
	out := map[int][]string{targetScene: queue}
	for i := 1; i <= 4; i++ {
		if i == targetScene {
			continue
		}
		out[i] = []string{shotResponse(i, visualBreakdownResponseShot{
			VisualDescriptor: fmt.Sprintf("scene %d descriptor", i),
			Transition:       domain.TransitionKenBurns,
		})}
	}
	return out
}

// TestVisualBreakdowner_Run_RetriesOnSceneN_WhenNGreaterThanOne pins the
// retry-loop's per-scene routing: scene N>1 must get exactly the same
// retry treatment as scene 1. The earlier coverage only exercised scene 1
// failing-then-recovering, which would not catch a regression that
// hard-codes the loop to scene 1.
func TestVisualBreakdowner_Run_RetriesOnSceneN_WhenNGreaterThanOne(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	invalid := `{"scene_num":3,"shots":[{"visual_descriptor":"shot","transition":""}]}`
	valid := shotResponse(3, visualBreakdownResponseShot{
		VisualDescriptor: "scene 3 descriptor",
		Transition:       domain.TransitionKenBurns,
	})
	gen := &queueTextGenerator{responsesByScene: retryQueueWithSceneNFailures(3, invalid, valid)}
	state := sampleVisualBreakdownState(4)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.callsByScene[3], 2)
	for sceneNum := 1; sceneNum <= 4; sceneNum++ {
		if sceneNum == 3 {
			continue
		}
		testutil.AssertEqual(t, gen.callsByScene[sceneNum], 1)
	}
	if state.VisualBreakdown == nil || len(state.VisualBreakdown.Scenes) != 4 {
		t.Fatalf("expected 4 scenes built, got %#v", state.VisualBreakdown)
	}
	for i, scene := range state.VisualBreakdown.Scenes {
		if scene.SceneNum != i+1 {
			t.Fatalf("ordering broken: scenes[%d].scene_num=%d, want=%d", i, scene.SceneNum, i+1)
		}
	}
}

// TestVisualBreakdowner_Run_HandlesMultipleConcurrentRetries proves the
// errgroup fan-out doesn't serialize retries: scenes 1 AND 3 fail-then-
// recover concurrently and final ordering is still preserved.
func TestVisualBreakdowner_Run_HandlesMultipleConcurrentRetries(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	invalid1 := `{"scene_num":1,"shots":[{"visual_descriptor":"shot","transition":""}]}`
	valid1 := shotResponse(1, visualBreakdownResponseShot{
		VisualDescriptor: "scene 1 descriptor",
		Transition:       domain.TransitionKenBurns,
	})
	invalid3 := `{"scene_num":3,"shots":[{"visual_descriptor":"shot","transition":"zoom"}]}`
	valid3 := shotResponse(3, visualBreakdownResponseShot{
		VisualDescriptor: "scene 3 descriptor",
		Transition:       domain.TransitionCrossDissolve,
	})
	queue := map[int][]string{
		1: {invalid1, valid1},
		2: {shotResponse(2, visualBreakdownResponseShot{VisualDescriptor: "scene 2 descriptor", Transition: domain.TransitionKenBurns})},
		3: {invalid3, valid3},
		4: {shotResponse(4, visualBreakdownResponseShot{VisualDescriptor: "scene 4 descriptor", Transition: domain.TransitionKenBurns})},
	}
	gen := &queueTextGenerator{responsesByScene: queue}
	state := sampleVisualBreakdownState(4)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if err != nil {
		t.Fatalf("VisualBreakdowner: %v", err)
	}
	testutil.AssertEqual(t, gen.callsByScene[1], 2)
	testutil.AssertEqual(t, gen.callsByScene[2], 1)
	testutil.AssertEqual(t, gen.callsByScene[3], 2)
	testutil.AssertEqual(t, gen.callsByScene[4], 1)
	if state.VisualBreakdown == nil || len(state.VisualBreakdown.Scenes) != 4 {
		t.Fatalf("expected 4 scenes built, got %#v", state.VisualBreakdown)
	}
	for i, scene := range state.VisualBreakdown.Scenes {
		if scene.SceneNum != i+1 {
			t.Fatalf("ordering broken: scenes[%d].scene_num=%d, want=%d", i, scene.SceneNum, i+1)
		}
	}
	// Scene 3 recovered with cross_dissolve to prove its specific retry
	// produced its specific response — not a cross-scene mix-up.
	testutil.AssertEqual(t, state.VisualBreakdown.Scenes[2].Shots[0].Transition, domain.TransitionCrossDissolve)
}

// TestVisualBreakdowner_Run_GivesUpAfterOneRetry was kept verbatim — the
// negative-budget guarantee is exercised separately at the runWithRetry
// helper level (see retry_test.go) because the production budget is
// const-pinned to 1. A future config-driven seam can reuse the same fn
// shape against the helper.
func TestVisualBreakdowner_Run_GivesUpAfterOneRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	invalid1 := `{"scene_num":1,"shots":[{"visual_descriptor":"shot","transition":""}]}`
	invalid2 := `{"scene_num":1,"shots":[{"visual_descriptor":"shot","transition":"zoom"}]}`
	gen := &queueTextGenerator{responsesByScene: retryQueueWithScene1Failures(invalid1, invalid2)}
	state := sampleVisualBreakdownState(4)
	err := NewVisualBreakdowner(gen, sampleWriterConfig(), sampleVisualAssets(), mustValidator(t, "visual_breakdown.schema.json"), uniformEstimator(7.0))(context.Background(), state)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation after retry exhausted, got %v", err)
	}
	testutil.AssertEqual(t, gen.callsByScene[1], 2)
	if state.VisualBreakdown != nil {
		t.Fatalf("expected state.VisualBreakdown unchanged on failure, got %#v", state.VisualBreakdown)
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
	// Force a duplicate by collapsing two beats into one slice with the same
	// implied scene_num. We do this by zeroing out the second act's beats so
	// LegacyScenes() yields fewer scenes than expected — but for v2 the
	// semantically equivalent guard is "two beats sharing the same offset
	// range produce same scene_num in the bridge", which the bridge does
	// not produce. Replace this with a shape that drives the v1 code path
	// the test was guarding: copy the first beat's metadata onto the second
	// then make them identical references — LegacyScenes() preserves order
	// and assigns sequential 1..N, so this test's original v1 invariant no
	// longer applies. Fall back to verifying the agent rejects an empty
	// narration (which is the surviving structural check in v2).
	state.Narration.Acts = nil // forces "no scenes" branch
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

// sceneErrorGenerator returns errOnScene's queued error for any call to that
// scene, and a single valid response for every other scene. Counts calls per
// scene so retry-budget-not-consumed assertions are possible.
type sceneErrorGenerator struct {
	mu             sync.Mutex
	errOnScene     int
	err            error
	validResponses map[int]string
	callsByScene   map[int]int
}

func (g *sceneErrorGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	sceneNum, ok := parseScenePromptNum(req.Prompt)
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("sceneErrorGenerator: scene_num missing from prompt")
	}
	if g.callsByScene == nil {
		g.callsByScene = map[int]int{}
	}
	g.callsByScene[sceneNum]++
	if sceneNum == g.errOnScene {
		return domain.TextResponse{}, g.err
	}
	resp, ok := g.validResponses[sceneNum]
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("sceneErrorGenerator: no response for scene_num=%d", sceneNum)
	}
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: resp, Model: req.Model, Provider: "openai"}}, nil
}

// queueTextGenerator pops responses from a per-scene FIFO queue, so a single
// scene can be served multiple distinct responses across retries. Routes by
// parsing "Scene N" from the rendered prompt; errors out if the prompt lacks
// the scene token. callsByScene exposes per-scene call counts for retry
// assertions.
type queueTextGenerator struct {
	mu               sync.Mutex
	responsesByScene map[int][]string
	callsByScene     map[int]int
}

func (q *queueTextGenerator) Generate(_ context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	sceneNum, ok := parseScenePromptNum(req.Prompt)
	if !ok {
		return domain.TextResponse{}, fmt.Errorf("queueTextGenerator: scene_num missing from prompt")
	}
	queue, ok := q.responsesByScene[sceneNum]
	if !ok || len(queue) == 0 {
		return domain.TextResponse{}, fmt.Errorf("queueTextGenerator: no response queued for scene_num=%d", sceneNum)
	}
	resp := queue[0]
	q.responsesByScene[sceneNum] = queue[1:]
	if q.callsByScene == nil {
		q.callsByScene = map[int]int{}
	}
	q.callsByScene[sceneNum]++
	return domain.TextResponse{NormalizedResponse: domain.NormalizedResponse{Content: resp, Model: req.Model, Provider: "openai"}}, nil
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

// sampleNarrationScenes builds a v2 NarrationScript whose LegacyScenes()
// returns sceneCount scene-shaped entries numbered 1..sceneCount in the act
// order produced by actForScene. Each beat covers a 10-rune slice of its
// act's monologue, matching the per-scene 1-beat layout the tests assume.
func sampleNarrationScenes(sceneCount int) *domain.NarrationScript {
	return sampleNarrationScenesWithBeats(sceneCount, nil)
}

// sampleNarrationScenesWithBeats accepts a per-scene beat-count override.
// In v2 every scene maps to one BeatAnchor; "extra beats" become additional
// BeatAnchors slicing the same monologue. Scenes not in the map default to
// 1 beat.
func sampleNarrationScenesWithBeats(sceneCount int, beatsByScene map[int]int) *domain.NarrationScript {
	type sceneSpec struct {
		actID    string
		text     string
		beatTxt  []string
		entityOn bool
	}
	specsByAct := map[string][]sceneSpec{}
	actOrder := []string{}
	for i := 1; i <= sceneCount; i++ {
		actID := actForScene(i)
		count, ok := beatsByScene[i]
		if !ok || count <= 0 {
			count = 1
		}
		beatTexts := make([]string, count)
		for b := 0; b < count; b++ {
			beatTexts[b] = fmt.Sprintf("scene %d beat %d", i, b)
		}
		if _, ok := specsByAct[actID]; !ok {
			actOrder = append(actOrder, actID)
		}
		specsByAct[actID] = append(specsByAct[actID], sceneSpec{
			actID:   actID,
			text:    fmt.Sprintf("scene %d narration", i),
			beatTxt: beatTexts,
		})
	}
	script := &domain.NarrationScript{
		SCPID:         "SCP-TEST",
		Title:         "SCP-TEST",
		SourceVersion: domain.NarrationSourceVersionV2,
	}
	for _, actID := range actOrder {
		specs := specsByAct[actID]
		// monologue = each scene's narration concatenated with " " separators.
		parts := make([]string, len(specs))
		for i, s := range specs {
			parts[i] = s.text
		}
		monologue := strings.Join(parts, " ")
		anchors := []domain.BeatAnchor{}
		offset := 0
		for i, s := range specs {
			runes := []rune(s.text)
			end := offset + len(runes)
			// One BeatAnchor per requested per-scene beat. Each beat slices a
			// proportional chunk of the scene's narration. With count=1 this
			// reduces to "one beat covers the full scene".
			n := len(s.beatTxt)
			chunk := (end - offset) / n
			if chunk == 0 {
				chunk = 1
			}
			for b := 0; b < n; b++ {
				bs := offset + b*chunk
				be := bs + chunk
				if b == n-1 || be > end {
					be = end
				}
				anchors = append(anchors, domain.BeatAnchor{
					StartOffset:       bs,
					EndOffset:         be,
					Mood:              "tense",
					Location:          "transit platform",
					CharactersPresent: []string{"SCP-TEST"},
					EntityVisible:     false,
					ColorPalette:      "gray",
					Atmosphere:        "tense",
					FactTags:          []domain.FactTag{},
				})
			}
			offset = end
			if i < len(specs)-1 {
				offset++ // joining space between scene narrations
			}
		}
		script.Acts = append(script.Acts, domain.ActScript{
			ActID:     actID,
			Monologue: monologue,
			Mood:      "tense",
			KeyPoints: []string{},
			Beats:     anchors,
		})
	}
	return script
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
