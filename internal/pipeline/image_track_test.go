package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// ---------------- helpers ----------------

type fakeImageGen struct {
	mu           sync.Mutex
	generateCalls []domain.ImageRequest
	editCalls     []domain.ImageEditRequest
	writeBytes    []byte
	costGenerate  float64
	costEdit      float64
	generateErr   error
	editErr       error
}

func newFakeImageGen() *fakeImageGen {
	return &fakeImageGen{
		writeBytes:   []byte("png-bytes"),
		costGenerate: 0.05,
		costEdit:     0.08,
	}
}

func (f *fakeImageGen) Generate(ctx context.Context, req domain.ImageRequest) (domain.ImageResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.generateErr != nil {
		return domain.ImageResponse{}, f.generateErr
	}
	f.generateCalls = append(f.generateCalls, req)
	if req.OutputPath != "" {
		if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
			return domain.ImageResponse{}, err
		}
		if err := os.WriteFile(req.OutputPath, f.writeBytes, 0o644); err != nil {
			return domain.ImageResponse{}, err
		}
	}
	return domain.ImageResponse{ImagePath: req.OutputPath, Model: req.Model, Provider: "dashscope", CostUSD: f.costGenerate}, nil
}

func (f *fakeImageGen) Edit(ctx context.Context, req domain.ImageEditRequest) (domain.ImageResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editErr != nil {
		return domain.ImageResponse{}, f.editErr
	}
	f.editCalls = append(f.editCalls, req)
	if req.OutputPath != "" {
		if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
			return domain.ImageResponse{}, err
		}
		if err := os.WriteFile(req.OutputPath, f.writeBytes, 0o644); err != nil {
			return domain.ImageResponse{}, err
		}
	}
	return domain.ImageResponse{ImagePath: req.OutputPath, Model: req.Model, Provider: "dashscope", CostUSD: f.costEdit}, nil
}

type fakeCharacterResolver struct {
	candidate *domain.CharacterCandidate
	err       error
	calls     int
}

func (f *fakeCharacterResolver) GetSelectedCandidate(ctx context.Context, runID string) (*domain.CharacterCandidate, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.candidate, nil
}

type fakeShotStore struct {
	mu      sync.Mutex
	entries map[int][]domain.Shot
	err     error
}

func newFakeShotStore() *fakeShotStore {
	return &fakeShotStore{entries: map[int][]domain.Shot{}}
}

func (f *fakeShotStore) UpsertImageShots(ctx context.Context, runID string, sceneIndex int, shots []domain.Shot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	copied := make([]domain.Shot, len(shots))
	copy(copied, shots)
	f.entries[sceneIndex] = copied
	return nil
}

type passthroughLimiter struct {
	calls int
	mu    sync.Mutex
}

func (p *passthroughLimiter) Do(ctx context.Context, fn func(context.Context) error) error {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return fn(ctx)
}

type imageTrackFixture struct {
	outputDir   string
	runID       string
	images      *fakeImageGen
	resolver    *fakeCharacterResolver
	shots       *fakeShotStore
	limiter     *passthroughLimiter
	track       pipeline.ImageTrack
	req         pipeline.PhaseBRequest
	sceneStyle  string // optional; opt-in via withSceneStyle option
}

// withSceneStyle wires SceneStylePrompt into the fixture's ImageTrackConfig.
// Default is empty (no-op fallback that yields pre-cycle two-layer prompts).
func withSceneStyle(style string) func(*imageTrackFixture) {
	return func(f *imageTrackFixture) { f.sceneStyle = style }
}

func scenarioStateForTest(runID string, scenes []sceneFixture, frozen string) *agents.PipelineState {
	// v2 (D2 migration): collapse the v1 multi-shot-per-scene fixture shape
	// into a v2 NarrationScript + VisualScript pair where each fixture shot
	// is its own beat (1:1 invariant). The image-track loop, which now
	// reads `state.VisualScript.LegacyScenes(state.Narration)`, then sees
	// one bridged scene per shot — total bridged scene count == total shot
	// count, matching every existing test's totalCalls expectation.
	monoBuilder := []rune{}
	anchors := []domain.BeatAnchor{}
	for sIdx, s := range scenes {
		// Distribute the scene's narration runes evenly across its shots so
		// each beat carries a non-empty rune slice. With one shot per scene
		// (the dominant fixture shape), this collapses to "beat covers the
		// whole scene narration" — a faithful preservation of v1 semantics.
		runes := []rune(s.narration)
		nShots := len(s.shots)
		if nShots == 0 {
			continue
		}
		chunk := len(runes) / nShots
		if chunk == 0 {
			chunk = 1
		}
		for shotIdx := 0; shotIdx < nShots; shotIdx++ {
			before := len(monoBuilder)
			beatStart := shotIdx * chunk
			beatEnd := beatStart + chunk
			if shotIdx == nShots-1 || beatEnd > len(runes) {
				beatEnd = len(runes)
			}
			beatRunes := runes[beatStart:beatEnd]
			monoBuilder = append(monoBuilder, beatRunes...)
			anchors = append(anchors, domain.BeatAnchor{
				StartOffset:       before,
				EndOffset:         len(monoBuilder),
				Mood:              "tense",
				Location:          "site-19",
				CharactersPresent: []string{"unknown"},
				EntityVisible:     s.entityVisible,
				ColorPalette:      "neutral",
				Atmosphere:        "subdued",
				FactTags:          []domain.FactTag{},
			})
		}
		if sIdx < len(scenes)-1 {
			monoBuilder = append(monoBuilder, ' ')
		}
	}
	// Build a v2 VisualScript whose Acts[0].Shots line up 1:1 with the
	// flattened anchor sequence above. The bridge LegacyScenes() then
	// reproduces the flattened scene→1-shot view that image_track.go
	// iterates over.
	visualShots := make([]domain.VisualShot, 0, len(anchors))
	anchorIdx := 0
	for _, s := range scenes {
		for _, descriptor := range s.shots {
			visualShots = append(visualShots, domain.VisualShot{
				ShotIndex:          anchorIdx + 1,
				VisualDescriptor:   frozen + "; " + descriptor,
				EstimatedDurationS: 4.0,
				Transition:         domain.TransitionKenBurns,
				NarrationAnchor:    anchors[anchorIdx],
			})
			anchorIdx++
		}
	}
	return &agents.PipelineState{
		RunID: runID,
		SCPID: "049",
		Narration: &domain.NarrationScript{
			SCPID:         "049",
			SourceVersion: domain.NarrationSourceVersionV2,
			Acts: []domain.ActScript{{
				ActID:     domain.ActIncident,
				Monologue: string(monoBuilder),
				Beats:     anchors,
				Mood:      "tense",
			}},
		},
		VisualScript: &domain.VisualScript{
			SCPID:            "049",
			FrozenDescriptor: frozen,
			Acts: []domain.VisualAct{{
				ActID: domain.ActIncident,
				Shots: visualShots,
			}},
			ShotOverrides: map[int]domain.ShotOverride{},
		},
	}
}

type sceneFixture struct {
	sceneNum      int
	narration     string
	entityVisible bool
	shots         []string
}

func writeScenario(t *testing.T, outputDir, runID string, state *agents.PipelineState) string {
	t.Helper()
	runDir := filepath.Join(outputDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}
	scenarioPath := filepath.Join(runDir, "scenario.json")
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	if err := os.WriteFile(scenarioPath, raw, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return scenarioPath
}

func newImageTrackFixture(t *testing.T, scenes []sceneFixture, opts ...func(*imageTrackFixture)) imageTrackFixture {
	t.Helper()
	outputDir := t.TempDir()
	runID := "scp-049-run-1"
	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	state := scenarioStateForTest(runID, scenes, frozen)
	scenarioPath := writeScenario(t, outputDir, runID, state)

	f := imageTrackFixture{
		outputDir: outputDir,
		runID:     runID,
		images:    newFakeImageGen(),
		resolver: &fakeCharacterResolver{candidate: &domain.CharacterCandidate{
			ID:       "scp-049#1",
			PageURL:  "https://example.com/049",
			ImageURL: "https://example.com/049/reference.jpg",
		}},
		shots:   newFakeShotStore(),
		limiter: &passthroughLimiter{},
	}
	for _, opt := range opts {
		opt(&f)
	}
	track, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:         outputDir,
		Provider:          "dashscope",
		GenerateModel:     "qwen-image",
		EditModel:         "qwen-image-edit",
		Width:             1024,
		Height:            1024,
		Images:            f.images,
		CharacterResolver: f.resolver,
		Shots:             f.shots,
		Limiter:           f.limiter,
		Clock:             clock.RealClock{},
		Logger:            nil,
		SceneStylePrompt:  f.sceneStyle,
	})
	if err != nil {
		t.Fatalf("NewImageTrack: %v", err)
	}
	f.track = track
	f.req = pipeline.PhaseBRequest{
		RunID:        runID,
		Stage:        domain.StageImage,
		ScenarioPath: scenarioPath,
	}
	return f
}

// ---------------- prompt composer ----------------

func TestImagePromptComposer_PrefixesFrozenDescriptorVerbatim(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	frozen := "Appearance: slender humanoid; Distinguishing features: glossy jet-black skin; Environment: dim concrete chamber"
	shot := "camera: low wide shot revealing SCP-049 emerging from shadow"

	// Empty style preserves pre-cycle two-layer behavior — frozen prefix
	// remains the load-bearing byte-stable contract from Story 5.4.
	got := pipeline.ComposeImagePrompt("", frozen, shot)

	if !strings.HasPrefix(got, frozen) {
		t.Fatalf("prompt does not begin with frozen descriptor verbatim:\n  prompt: %q\n  frozen: %q", got, frozen)
	}
	if !strings.Contains(got, shot) {
		t.Fatalf("prompt does not contain shot descriptor: %q", got)
	}

	// Idempotent when frozen is already prefixed.
	already := frozen + "; cinematic wide establishing shot"
	if got2 := pipeline.ComposeImagePrompt("", frozen, already); got2 != already {
		t.Fatalf("prompt composer mutated already-prefixed input: got %q, want %q", got2, already)
	}
}

// TestImagePromptComposer_LayersStyleFrontmost confirms that a non-empty
// scene style is layered in front of the frozen descriptor without mutating
// frozen's bytes — Story 5.4's byte-stability contract is preserved
// because style is a new outer layer, not a splice into frozen.
func TestImagePromptComposer_LayersStyleFrontmost(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	style := "Style: kid-friendly cartoon, vibrant"
	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	visual := "low wide shot revealing the entity"

	got := pipeline.ComposeImagePrompt(style, frozen, visual)
	want := style + "; " + frozen + "; " + visual
	if got != want {
		t.Fatalf("layered prompt mismatch:\n  got:  %q\n  want: %q", got, want)
	}
	// Frozen substring appears verbatim exactly once (no double prefix).
	if c := strings.Count(got, frozen); c != 1 {
		t.Fatalf("frozen substring appears %d times, want exactly 1: %q", c, got)
	}
}

// TestImagePromptComposer_StyleEmptyMatchesPreCycle locks in the no-op
// fallback contract: an empty style yields a prompt byte-equal to the
// pre-cycle two-layer composition, so operators can clear scene_style_prompt
// in config.yaml without disabling Phase B image generation.
func TestImagePromptComposer_StyleEmptyMatchesPreCycle(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	frozen := "F"
	visual := "V"

	got := pipeline.ComposeImagePrompt("", frozen, visual)
	if got != "F; V" {
		t.Fatalf("empty-style fallback drifted from pre-cycle: got %q, want %q", got, "F; V")
	}
}

// TestImagePromptComposer_VisualAlreadyPrefixedSkipsDoubleFrozen preserves
// the existing idempotency guard: if the per-shot visual descriptor already
// begins with the frozen bytes, the composer must not double-prefix even
// when a style layer is added in front.
func TestImagePromptComposer_VisualAlreadyPrefixedSkipsDoubleFrozen(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	style := "S"
	frozen := "F"
	visual := frozen + "; extra"

	got := pipeline.ComposeImagePrompt(style, frozen, visual)
	want := "S; F; extra"
	if got != want {
		t.Fatalf("composer mismatch on already-prefixed visual:\n  got:  %q\n  want: %q", got, want)
	}
	if c := strings.Count(got, frozen); c != 1 {
		t.Fatalf("frozen substring appears %d times, want exactly 1: %q", c, got)
	}
}

// TestImagePromptComposer_FrozenEndsWithSeparator preserves the existing
// separator-collapse guard. The new style layer must not introduce a
// duplicate separator on the right edge of frozen.
func TestImagePromptComposer_FrozenEndsWithSeparator(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	style := "S"
	frozen := "F; "
	visual := "V"

	got := pipeline.ComposeImagePrompt(style, frozen, visual)
	want := "S; F; V"
	if got != want {
		t.Fatalf("separator-collapse mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestImagePromptComposer_StyleEndsWithSeparator covers the symmetric case:
// when the operator-supplied style ends with "; ", the composer must not
// emit a doubled separator between style and the rest of the prompt.
func TestImagePromptComposer_StyleEndsWithSeparator(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	style := "S; "
	frozen := "F"
	visual := "V"

	got := pipeline.ComposeImagePrompt(style, frozen, visual)
	want := "S; F; V"
	if got != want {
		t.Fatalf("style-trailing-sep mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestImagePromptComposer_VisualAlreadyPrefixedWithStyleSkipsDoublePrefix
// guards against a worst-case LLM output where the visual_descriptor
// already begins with the configured Style Directive prefix verbatim. The
// composer must short-circuit the style layer in that case so the final
// prompt contains the style and frozen segments exactly once each —
// symmetric to the existing HasPrefix(visual, frozen) idempotency guard
// in composeFrozenAndVisual.
func TestImagePromptComposer_VisualAlreadyPrefixedWithStyleSkipsDoublePrefix(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	style := "Style: cartoon"
	frozen := "Appearance: humanoid"
	composed := style + "; " + frozen + "; cinematic wide"

	got := pipeline.ComposeImagePrompt(style, frozen, composed)
	if got != composed {
		t.Fatalf("composer added duplicate style prefix:\n  got:  %q\n  want: %q", got, composed)
	}
	if c := strings.Count(got, style); c != 1 {
		t.Fatalf("style substring appears %d times, want 1: %q", c, got)
	}
	if c := strings.Count(got, frozen); c != 1 {
		t.Fatalf("frozen substring appears %d times, want 1: %q", c, got)
	}
}

// TestImageTrack_SceneStyleAppearsAsPrefixOnEveryShot exercises the
// end-to-end propagation: when SceneStylePrompt is set on ImageTrackConfig,
// every per-shot prompt sent to the image provider begins with the style
// string, then the frozen descriptor verbatim, then the shot's visual
// descriptor. The fixture covers both Generate and Edit paths in one run.
func TestImageTrack_SceneStyleAppearsAsPrefixOnEveryShot(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	style := "Style: kid-friendly cartoon, vibrant"
	scenes := []sceneFixture{
		{sceneNum: 1, narration: "n1", entityVisible: true, shots: []string{"close-up"}},
		{sceneNum: 2, narration: "n2", entityVisible: false, shots: []string{"wide shot"}},
	}
	f := newImageTrackFixture(t, scenes, withSceneStyle(style))
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	var prompts []string
	for _, c := range f.images.generateCalls {
		prompts = append(prompts, c.Prompt)
	}
	for _, c := range f.images.editCalls {
		prompts = append(prompts, c.Prompt)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(prompts))
	}
	for i, p := range prompts {
		if !strings.HasPrefix(p, style) {
			t.Fatalf("prompt %d missing scene style prefix: %q", i, p)
		}
		if strings.Count(p, frozen) != 1 {
			t.Fatalf("prompt %d frozen substring count != 1: %q", i, p)
		}
		if !strings.Contains(p, frozen) {
			t.Fatalf("prompt %d missing frozen descriptor: %q", i, p)
		}
	}
}

// ---------------- scenario loading ----------------

func TestImageTrack_LoadsShotBreakdownFromScenarioJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene1", entityVisible: true, shots: []string{"wide shot", "close-up"}},
		{sceneNum: 2, narration: "scene2", entityVisible: false, shots: []string{"empty corridor"}},
	}
	f := newImageTrackFixture(t, scenes)

	res, err := f.track(context.Background(), f.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	// Two shots scene 1 + one shot scene 2 = 3 calls total.
	totalCalls := len(f.images.generateCalls) + len(f.images.editCalls)
	if totalCalls != 3 {
		t.Fatalf("expected 3 provider calls derived from scenario.json, got %d", totalCalls)
	}
	if len(res.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(res.Artifacts))
	}
}

func TestImageTrack_UsesOperatorOverrideShotsWithoutRecomputing(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Operator-override: heavy-narration scene kept at 1 shot instead of
	// recomputing from duration. Image track must respect the persisted count.
	scenes := []sceneFixture{
		{sceneNum: 1, narration: strings.Repeat("word ", 200), entityVisible: true, shots: []string{"single hero shot"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	total := len(f.images.generateCalls) + len(f.images.editCalls)
	if total != 1 {
		t.Fatalf("operator-override shot count violated: %d calls (want 1)", total)
	}
}

func TestImageTrack_MissingScenarioJSONFailsValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	track, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:         outputDir,
		Provider:          "dashscope",
		GenerateModel:     "qwen-image",
		EditModel:         "qwen-image-edit",
		Images:            newFakeImageGen(),
		CharacterResolver: &fakeCharacterResolver{},
		Shots:             newFakeShotStore(),
		Limiter:           &passthroughLimiter{},
	})
	if err != nil {
		t.Fatalf("NewImageTrack: %v", err)
	}
	missingPath := filepath.Join(outputDir, "scp-049-run-1", "scenario.json")
	_, err = track(context.Background(), pipeline.PhaseBRequest{
		RunID:        "scp-049-run-1",
		Stage:        domain.StageImage,
		ScenarioPath: missingPath,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation on missing scenario.json, got %v", err)
	}
}

// ---------------- frozen descriptor propagation ----------------

func TestImageTrack_AllShotsShareIdenticalFrozenDescriptorPrefix(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "n1", entityVisible: true, shots: []string{"shot a", "shot b"}},
		{sceneNum: 2, narration: "n2", entityVisible: false, shots: []string{"shot c"}},
		{sceneNum: 3, narration: "n3", entityVisible: true, shots: []string{"shot d", "shot e"}},
	}
	f := newImageTrackFixture(t, scenes)
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	var prompts []string
	for _, c := range f.images.generateCalls {
		prompts = append(prompts, c.Prompt)
	}
	for _, c := range f.images.editCalls {
		prompts = append(prompts, c.Prompt)
	}
	if len(prompts) != 5 {
		t.Fatalf("expected 5 prompts, got %d", len(prompts))
	}
	// With no scene style configured (default fixture), the prompt is the
	// pre-cycle two-layer shape: frozen prefix verbatim. The frozen substring
	// still appears exactly once even when a style layer is later prepended,
	// so we assert via Contains+count rather than HasPrefix to keep the
	// invariant test orthogonal to the optional style layer.
	for i, p := range prompts {
		if !strings.HasPrefix(p, frozen) {
			t.Fatalf("prompt %d missing frozen prefix (no style configured): %q", i, p)
		}
		if strings.Count(p, frozen) != 1 {
			t.Fatalf("prompt %d does not include frozen substring exactly once: %q", i, p)
		}
	}
}

func TestImageTrack_MissingFrozenDescriptorFailsLoudly(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "n1", entityVisible: false, shots: []string{"x"}},
	}
	outputDir := t.TempDir()
	runID := "scp-049-run-1"
	state := scenarioStateForTest(runID, scenes, "")
	state.VisualScript.FrozenDescriptor = ""
	scenarioPath := writeScenario(t, outputDir, runID, state)

	track, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:         outputDir,
		Provider:          "dashscope",
		GenerateModel:     "qwen-image",
		EditModel:         "qwen-image-edit",
		Images:            newFakeImageGen(),
		CharacterResolver: &fakeCharacterResolver{},
		Shots:             newFakeShotStore(),
		Limiter:           &passthroughLimiter{},
	})
	if err != nil {
		t.Fatalf("NewImageTrack: %v", err)
	}
	_, err = track(context.Background(), pipeline.PhaseBRequest{
		RunID:        runID,
		Stage:        domain.StageImage,
		ScenarioPath: scenarioPath,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation on missing frozen descriptor, got %v", err)
	}
}

// ---------------- character vs non-character routing ----------------

func TestImageTrack_CharacterShotUsesEditWithSelectedCharacterReference(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"character reveal"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	if len(f.images.editCalls) != 1 {
		t.Fatalf("expected 1 edit call for character shot, got %d", len(f.images.editCalls))
	}
	if len(f.images.generateCalls) != 0 {
		t.Fatalf("expected no generate calls for character-only scene, got %d", len(f.images.generateCalls))
	}
	testutil.AssertEqual(t, f.images.editCalls[0].ReferenceImageURL, "https://example.com/049/reference.jpg")
	testutil.AssertEqual(t, f.images.editCalls[0].Model, "qwen-image-edit")
}

func TestImageTrack_SelectedCharacterResolutionFailureAbortsCharacterShot(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"character reveal"}},
	}
	resolverErr := errors.New("cache miss")
	f := newImageTrackFixture(t, scenes, func(f *imageTrackFixture) {
		f.resolver = &fakeCharacterResolver{err: resolverErr}
	})
	_, err := f.track(context.Background(), f.req)
	if err == nil {
		t.Fatal("expected error when character resolver fails")
	}
	if !errors.Is(err, resolverErr) {
		t.Fatalf("expected wrapped resolver error, got %v", err)
	}
	if len(f.images.editCalls)+len(f.images.generateCalls) != 0 {
		t.Fatalf("image provider must not be called when character resolution fails")
	}
}

func TestImageTrack_NonCharacterShotSkipsReferenceEdit(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: false, shots: []string{"empty corridor"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	if len(f.images.editCalls) != 0 {
		t.Fatalf("non-character scene must not use edit path; got %d edit calls", len(f.images.editCalls))
	}
	// Resolver must not be invoked at all when no scenes contain the character.
	if f.resolver.calls != 0 {
		t.Fatalf("character resolver invoked for non-character-only run: %d calls", f.resolver.calls)
	}
}

func TestImageTrack_NonCharacterShotUsesStandardGenerate(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: false, shots: []string{"empty corridor"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	if len(f.images.generateCalls) != 1 {
		t.Fatalf("non-character shot must use generate; got %d", len(f.images.generateCalls))
	}
	testutil.AssertEqual(t, f.images.generateCalls[0].Model, "qwen-image")
}

func TestImageTrack_EditAndGenerateShareOutputPersistenceContract(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"character"}},
		{sceneNum: 2, narration: "scene 2", entityVisible: false, shots: []string{"scenery"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	// Both paths must write to images/scene_{idx}/shot_{idx}.png and persist a
	// shot entry through the same store surface.
	checkFile := func(rel string) {
		full := filepath.Join(f.outputDir, f.runID, rel)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("missing artifact %s: %v", rel, err)
		}
		if info.Size() == 0 {
			t.Fatalf("artifact %s is empty", rel)
		}
	}
	checkFile("images/scene_01/shot_01.png")
	checkFile("images/scene_02/shot_01.png")

	if len(f.shots.entries) != 2 {
		t.Fatalf("expected 2 persisted segments, got %d", len(f.shots.entries))
	}
	for _, shots := range f.shots.entries {
		if len(shots) != 1 {
			t.Fatalf("expected 1 shot per scene, got %d", len(shots))
		}
		if shots[0].ImagePath == "" {
			t.Fatalf("image path missing from persisted shot: %+v", shots[0])
		}
	}
}

// ---------------- canonical path + rerun ----------------

func TestImageTrack_WritesImagesToSceneShotDirectories(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// v2 (D2): the visual_breakdowner emits one shot per BeatAnchor; the
	// bridge LegacyScenes() flattens that to one bridged scene per shot,
	// preserving the 1-shot-per-scene canonical-path pattern. The test
	// fixture below collapses to 3 fixture scenes × 1 shot each (= 3
	// bridged scenes), matching v2's 1:1 invariant.
	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"a"}},
		{sceneNum: 2, narration: "scene 2", entityVisible: true, shots: []string{"b"}},
		{sceneNum: 3, narration: "scene 3", entityVisible: false, shots: []string{"c"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	expected := []string{
		"images/scene_01/shot_01.png",
		"images/scene_02/shot_01.png",
		"images/scene_03/shot_01.png",
	}
	for _, rel := range expected {
		if _, err := os.Stat(filepath.Join(f.outputDir, f.runID, rel)); err != nil {
			t.Fatalf("missing expected artifact %s: %v", rel, err)
		}
	}
}

func TestImageTrack_TypicalRunProducesExpectedImageCount(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// 10 scenes with alternating 2/3/4 shots averaging ~3 shots/scene = 30 shots.
	shotPlan := []int{3, 2, 4, 3, 3, 2, 4, 3, 3, 3}
	total := 0
	scenes := make([]sceneFixture, 0, len(shotPlan))
	for i, count := range shotPlan {
		desc := make([]string, 0, count)
		for k := 0; k < count; k++ {
			desc = append(desc, fmt.Sprintf("shot-%d-%d", i+1, k+1))
		}
		scenes = append(scenes, sceneFixture{
			sceneNum:      i + 1,
			narration:     fmt.Sprintf("scene-%d", i+1),
			entityVisible: i%2 == 0,
			shots:         desc,
		})
		total += count
	}
	if total != 30 {
		t.Fatalf("plan invariant: expected 30 shots, got %d", total)
	}

	f := newImageTrackFixture(t, scenes)
	res, err := f.track(context.Background(), f.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	if len(res.Artifacts) != total {
		t.Fatalf("expected %d artifacts for 10-scene/3-avg run, got %d", total, len(res.Artifacts))
	}
}

func TestImageTrack_RerunPreservesCanonicalPathPattern(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"one"}},
		{sceneNum: 2, narration: "scene 2", entityVisible: false, shots: []string{"two"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Simulate clean-slate resume: delete the images dir, then re-run.
	imagesDir := filepath.Join(f.outputDir, f.runID, "images")
	if err := os.RemoveAll(imagesDir); err != nil {
		t.Fatalf("remove images dir: %v", err)
	}
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, rel := range []string{
		"images/scene_01/shot_01.png",
		"images/scene_02/shot_01.png",
	} {
		if _, err := os.Stat(filepath.Join(f.outputDir, f.runID, rel)); err != nil {
			t.Fatalf("rerun missing artifact %s: %v", rel, err)
		}
	}
}

// ---------------- persistence contract ----------------

func TestImageTrack_SegmentsShotsJSONPreservesVisualDescriptor(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"hero wide"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	persisted, ok := f.shots.entries[0]
	if !ok || len(persisted) != 1 {
		t.Fatalf("expected 1 persisted shot for scene 1, got %+v", f.shots.entries)
	}
	shot := persisted[0]
	if shot.VisualDescriptor == "" {
		t.Fatalf("visual descriptor dropped from persisted shot: %+v", shot)
	}
	if shot.Transition != domain.TransitionKenBurns {
		t.Fatalf("transition not carried forward: %+v", shot)
	}
	if shot.DurationSeconds <= 0 {
		t.Fatalf("duration not carried forward: %+v", shot)
	}
}

func TestImageTrack_SegmentsShotsRemainAlignedWithScenarioShotOrder(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// v2 (D2): ordering invariant operates on the bridged scene sequence —
	// one scene per BeatAnchor in narration→visual ordering. Three fixture
	// scenes here exercise the same "shots emit in declared order" contract
	// the v1 multi-shot-per-scene case did, but against v2's 1:1 layout.
	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"alpha"}},
		{sceneNum: 2, narration: "scene 2", entityVisible: true, shots: []string{"beta"}},
		{sceneNum: 3, narration: "scene 3", entityVisible: true, shots: []string{"gamma"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	wantSuffixes := []string{"alpha", "beta", "gamma"}
	for i := range scenes {
		persisted, ok := f.shots.entries[i]
		if !ok || len(persisted) != 1 {
			t.Fatalf("expected 1 shot at sceneIndex=%d, got %#v", i, persisted)
		}
		s := persisted[0]
		if !strings.HasSuffix(s.VisualDescriptor, wantSuffixes[i]) {
			t.Fatalf("scene %d descriptor out of order: %q (want suffix %q)", i+1, s.VisualDescriptor, wantSuffixes[i])
		}
		if s.ImagePath != filepath.Join("images", fmt.Sprintf("scene_%02d", i+1), "shot_01.png") {
			t.Fatalf("scene %d image path out of order: %q", i+1, s.ImagePath)
		}
	}
}

// ---------------- Phase B integration ----------------

func TestPhaseBRunner_ImageTrackParticipatesWithoutCancellingSiblingTrack(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"a"}},
		{sceneNum: 2, narration: "scene 2", entityVisible: false, shots: []string{"b"}},
	}
	f := newImageTrackFixture(t, scenes)
	f.images.editErr = errors.New("dashscope 500")

	ttsStarted := make(chan struct{})
	ttsReleased := make(chan struct{})
	ttsFinished := make(chan struct{})

	runner := pipeline.NewPhaseBRunner(
		f.track,
		func(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
			close(ttsStarted)
			<-ttsReleased
			close(ttsFinished)
			return pipeline.TTSTrackResult{
				Observation: domain.StageObservation{Stage: domain.StageTTS, DurationMs: 1},
			}, nil
		},
		nil,
		clock.RealClock{},
		nil,
		nil,
		nil,
	)

	done := make(chan struct{})
	var runErr error
	var runRes pipeline.PhaseBResult
	go func() {
		defer close(done)
		runRes, runErr = runner.Run(context.Background(), f.req)
	}()

	<-ttsStarted
	// The image track has failed — TTS must still be able to complete.
	close(ttsReleased)
	<-ttsFinished
	<-done

	if runErr == nil {
		t.Fatal("expected image failure to propagate through phase b runner")
	}
	if runRes.TTS.Err != nil {
		t.Fatalf("tts sibling was cancelled: %v", runRes.TTS.Err)
	}
}

// ---------------- resume + consistency ----------------

func TestResume_PhaseBRegenerationRebuildsSegmentsShotsAfterFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"one"}},
		{sceneNum: 2, narration: "scene 2", entityVisible: false, shots: []string{"two"}},
	}
	f := newImageTrackFixture(t, scenes)

	// First pass succeeds on scene 1, fails midway into scene 2 by setting a
	// provider error only *after* scene 1 persists. Easiest simulation: first
	// force generate error, observe partial persistence, then clear error and
	// rerun to rebuild.
	f.images.editErr = nil
	f.images.generateErr = errors.New("boom on scene 2")
	_, err := f.track(context.Background(), f.req)
	if err == nil {
		t.Fatal("expected failure during first run")
	}
	// scene 1 should have persisted; scene 2 should not.
	if _, ok := f.shots.entries[0]; !ok {
		t.Fatalf("scene 1 must persist before scene 2 failure")
	}
	if _, ok := f.shots.entries[1]; ok {
		t.Fatalf("scene 2 must NOT persist when provider errors: %+v", f.shots.entries[1])
	}

	// Simulate clean-slate Phase B resume: drop fake persisted rows + remove images.
	f.shots.entries = map[int][]domain.Shot{}
	if err := os.RemoveAll(filepath.Join(f.outputDir, f.runID, "images")); err != nil {
		t.Fatalf("remove images: %v", err)
	}
	f.images.generateErr = nil
	f.images.editErr = nil

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("rerun failed: %v", err)
	}
	if len(f.shots.entries) != 2 {
		t.Fatalf("rerun must rebuild every scene: got %d entries", len(f.shots.entries))
	}
}

func TestImageTrack_OutputIsConsumableByConsistencyCheck(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "scene 1", entityVisible: true, shots: []string{"one"}},
		{sceneNum: 2, narration: "scene 2", entityVisible: false, shots: []string{"two"}},
	}
	f := newImageTrackFixture(t, scenes)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	runDir := filepath.Join(f.outputDir, f.runID)
	// Build Episodes consistent with the image-track output, including the
	// required scenario_path for post-phase-A stages.
	episodes := make([]*domain.Episode, 0, len(f.shots.entries))
	for sceneIndex, shots := range f.shots.entries {
		episodes = append(episodes, &domain.Episode{
			RunID:      f.runID,
			SceneIndex: sceneIndex,
			Shots:      shots,
		})
	}
	scenarioRel := "scenario.json"
	run := &domain.Run{ID: f.runID, SCPID: "049", Stage: domain.StageImage, Status: domain.StatusRunning, ScenarioPath: &scenarioRel}

	report, err := pipeline.CheckConsistency(runDir, run, episodes)
	if err != nil {
		t.Fatalf("CheckConsistency: %v", err)
	}
	if len(report.Mismatches) != 0 {
		t.Fatalf("expected zero mismatches, got %+v", report.Mismatches)
	}
}

// TestImageTrack_FrozenDescriptorOverridePrecedesArtifactValue verifies that
// when the operator has edited the descriptor and saved it to
// runs.frozen_descriptor, the pipeline caller passes it via
// PhaseBRequest.FrozenDescriptorOverride and every image prompt begins with
// the override bytes (ComposeImagePrompt uses the override as the frozen
// prefix). The artifact shot strings may still include the original artifact
// frozen text because they were composed pre-override — that is expected;
// the contract here is about what bytes go into the frozen prefix position.
func TestImageTrack_FrozenDescriptorOverridePrecedesArtifactValue(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Use shot descriptors that do NOT embed the artifact frozen prefix so
	// the override's precedence is observable on the final prompt.
	outputDir := t.TempDir()
	runID := "scp-049-run-1"
	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	state := &agents.PipelineState{
		RunID: runID,
		SCPID: "049",
		Narration: &domain.NarrationScript{
			SCPID:         "049",
			SourceVersion: domain.NarrationSourceVersionV2,
			Acts: []domain.ActScript{{
				ActID:     domain.ActIncident,
				Monologue: "n1",
				Beats: []domain.BeatAnchor{{
					StartOffset: 0, EndOffset: 2,
					Mood: "calm", Location: "site-19", CharactersPresent: []string{"unknown"},
					EntityVisible: false, ColorPalette: "neutral", Atmosphere: "subdued",
				}},
			}},
		},
		VisualScript: &domain.VisualScript{
			SCPID:            "049",
			FrozenDescriptor: frozen,
			Acts: []domain.VisualAct{{
				ActID: domain.ActIncident,
				Shots: []domain.VisualShot{{
					ShotIndex:          1,
					VisualDescriptor:   "camera: low wide establishing",
					EstimatedDurationS: 4.0,
					Transition:         domain.TransitionKenBurns,
					NarrationAnchor: domain.BeatAnchor{
						StartOffset: 0, EndOffset: 2,
						Mood: "calm", Location: "site-19", CharactersPresent: []string{"unknown"},
						EntityVisible: false, ColorPalette: "neutral", Atmosphere: "subdued",
					},
				}},
			}},
			ShotOverrides: map[int]domain.ShotOverride{},
		},
	}
	scenarioPath := writeScenario(t, outputDir, runID, state)

	images := newFakeImageGen()
	track, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:         outputDir,
		Provider:          "dashscope",
		GenerateModel:     "qwen-image",
		EditModel:         "qwen-image-edit",
		Width:             1024,
		Height:            1024,
		Images:            images,
		CharacterResolver: &fakeCharacterResolver{},
		Shots:             newFakeShotStore(),
		Limiter:           &passthroughLimiter{},
		Clock:             clock.RealClock{},
	})
	if err != nil {
		t.Fatalf("NewImageTrack: %v", err)
	}
	override := "OPERATOR EDIT: porcelain mask; dim teal uplight"
	req := pipeline.PhaseBRequest{
		RunID:                    runID,
		Stage:                    domain.StageImage,
		ScenarioPath:             scenarioPath,
		FrozenDescriptorOverride: &override,
	}
	if _, err := track(context.Background(), req); err != nil {
		t.Fatalf("track: %v", err)
	}

	if len(images.generateCalls) != 1 {
		t.Fatalf("expected 1 generate call, got %d", len(images.generateCalls))
	}
	prompt := images.generateCalls[0].Prompt
	if !strings.HasPrefix(prompt, override) {
		t.Fatalf("prompt missing override prefix: %q", prompt)
	}
	if strings.Contains(prompt, frozen) {
		t.Fatalf("override must replace (not supplement) artifact frozen: %q", prompt)
	}
}

func TestImageTrack_NilFrozenDescriptorOverrideFallsThroughToArtifact(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "n1", entityVisible: false, shots: []string{"shot a"}},
	}
	f := newImageTrackFixture(t, scenes)
	// No override set.
	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}

	artifactFrozen := "Appearance: slender humanoid; Environment: concrete chamber"
	if len(f.images.generateCalls) != 1 {
		t.Fatalf("expected 1 generate call, got %d", len(f.images.generateCalls))
	}
	if !strings.HasPrefix(f.images.generateCalls[0].Prompt, artifactFrozen) {
		t.Fatalf("prompt should retain artifact frozen descriptor when no override: %q", f.images.generateCalls[0].Prompt)
	}
}

func TestImageTrack_EmptyFrozenDescriptorOverrideFallsThroughToArtifact(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scenes := []sceneFixture{
		{sceneNum: 1, narration: "n1", entityVisible: false, shots: []string{"shot a"}},
	}
	f := newImageTrackFixture(t, scenes)
	empty := "   "
	f.req.FrozenDescriptorOverride = &empty

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	artifactFrozen := "Appearance: slender humanoid; Environment: concrete chamber"
	if !strings.HasPrefix(f.images.generateCalls[0].Prompt, artifactFrozen) {
		t.Fatalf("blank override must fall through to artifact: %q", f.images.generateCalls[0].Prompt)
	}
}

// ── Audit logging ──────────────────────────────────────────────────────────────

func TestImageTrack_WritesAuditLogOnSuccess(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	outputDir := t.TempDir()
	runID := "scp-049-img-audit"
	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	scenes := []sceneFixture{
		{sceneNum: 1, narration: "Scene one.", shots: []string{"wide shot of containment cell"}},
		{sceneNum: 2, narration: "Scene two.", shots: []string{"close-up of SCP-049 hands"}},
	}
	state := scenarioStateForTest(runID, scenes, frozen)
	scenarioPath := writeScenario(t, outputDir, runID, state)

	images := newFakeImageGen()
	resolver := &fakeCharacterResolver{}
	shots := newFakeShotStore()
	limiter := &passthroughLimiter{}
	auditLogger := pipeline.NewFileAuditLogger(outputDir)

	track, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:         outputDir,
		Provider:          "dashscope",
		GenerateModel:     "qwen-image",
		EditModel:         "qwen-image-edit",
		Width:             1024,
		Height:            1024,
		Images:            images,
		CharacterResolver: resolver,
		Shots:             shots,
		Limiter:           limiter,
		Clock:             clock.RealClock{},
		Logger:            nil,
		AuditLogger:       auditLogger,
	})
	if err != nil {
		t.Fatalf("NewImageTrack: %v", err)
	}

	req := pipeline.PhaseBRequest{
		RunID:        runID,
		Stage:        domain.StageImage,
		ScenarioPath: scenarioPath,
	}

	_, err = track(context.Background(), req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	auditPath := filepath.Join(outputDir, runID, "audit.log")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 audit lines for 2 shots, got %d", len(lines))
	}
	for i, line := range lines {
		var entry domain.AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		if entry.EventType != domain.AuditEventImageGeneration {
			t.Errorf("line %d: event_type=%q, want %q", i, entry.EventType, domain.AuditEventImageGeneration)
		}
		if entry.RunID != runID {
			t.Errorf("line %d: run_id=%q, want %q", i, entry.RunID, runID)
		}
		if entry.Stage != string(domain.StageImage) {
			t.Errorf("line %d: stage=%q, want %q", i, entry.Stage, domain.StageImage)
		}
	}
}

func TestImageTrack_NilAuditLoggerDoesNotPanic(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// newImageTrackFixture does not set AuditLogger, so it's nil by default.
	fx := newImageTrackFixture(t, []sceneFixture{
		{sceneNum: 1, narration: "Scene one.", shots: []string{"wide shot"}},
	})

	_, err := fx.track(context.Background(), fx.req)
	if err != nil {
		t.Fatalf("track: %v", err)
	}
}

// ---------------- canonical image resolver ----------------

type fakeCanonicalResolver struct {
	rec   *domain.ScpImageRecord
	err   error
	calls int
}

func (f *fakeCanonicalResolver) GetByRun(ctx context.Context, runID string) (*domain.ScpImageRecord, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.rec == nil {
		return nil, domain.ErrNotFound
	}
	return f.rec, nil
}

// minimalPNG is a 1x1 PNG used by canonical-resolver tests so the loader's
// http.DetectContentType correctly identifies the file as image/png.
var minimalPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func newImageTrackWithCanonical(t *testing.T, canonicalResolver pipeline.CanonicalImageResolver, scpImageDir string) imageTrackFixture {
	t.Helper()
	outputDir := t.TempDir()
	runID := "scp-049-run-1"
	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	scenes := []sceneFixture{
		{sceneNum: 1, narration: "Scene one.", entityVisible: true, shots: []string{"wide shot"}},
	}
	state := scenarioStateForTest(runID, scenes, frozen)
	scenarioPath := writeScenario(t, outputDir, runID, state)

	f := imageTrackFixture{
		outputDir: outputDir,
		runID:     runID,
		images:    newFakeImageGen(),
		resolver: &fakeCharacterResolver{candidate: &domain.CharacterCandidate{
			ID:       "scp-049#1",
			PageURL:  "https://example.com/049",
			ImageURL: "https://example.com/049/reference.jpg",
		}},
		shots:   newFakeShotStore(),
		limiter: &passthroughLimiter{},
	}
	track, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:         outputDir,
		Provider:          "comfyui",
		GenerateModel:     "qwen-image",
		EditModel:         "qwen-image-edit",
		Width:             1024,
		Height:            1024,
		Images:            f.images,
		CharacterResolver: f.resolver,
		CanonicalResolver: canonicalResolver,
		ScpImageDir:       scpImageDir,
		Shots:             f.shots,
		Limiter:           f.limiter,
		Clock:             clock.RealClock{},
		Logger:            nil,
		// Recognize the canonical-priority test by passing a refImageFetcher
		// that errors out — we want to assert the canonical branch never
		// touches it.
		RefImageFetcher: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("ref fetcher must not be called when canonical exists")
		},
	})
	if err != nil {
		t.Fatalf("NewImageTrack: %v", err)
	}
	f.track = track
	f.req = pipeline.PhaseBRequest{
		RunID:        runID,
		Stage:        domain.StageImage,
		ScenarioPath: scenarioPath,
	}
	return f
}

func TestImageTrack_CanonicalHit_UsesLocalDataURL(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scpImageDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scpImageDir, "049"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	canonicalPath := filepath.Join(scpImageDir, "049", "canonical.png")
	if err := os.WriteFile(canonicalPath, minimalPNG, 0o644); err != nil {
		t.Fatalf("write canonical: %v", err)
	}

	res := &fakeCanonicalResolver{rec: &domain.ScpImageRecord{
		ScpID:    "049",
		FilePath: filepath.Join("049", "canonical.png"),
		Version:  1,
	}}
	f := newImageTrackWithCanonical(t, res, scpImageDir)

	if _, err := f.track(context.Background(), f.req); err != nil {
		t.Fatalf("track: %v", err)
	}
	if res.calls == 0 {
		t.Fatalf("expected canonical resolver to be queried, got 0 calls")
	}
	if len(f.images.editCalls) != 1 {
		t.Fatalf("expected 1 Edit call (character shot), got %d", len(f.images.editCalls))
	}
	got := f.images.editCalls[0].ReferenceImageURL
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("Edit ReferenceImageURL = %q, want data:image/png;base64,...", got)
	}
	// Ensure the DDG candidate URL was NOT used as the reference.
	if strings.Contains(got, "example.com") {
		t.Fatalf("canonical hit should bypass DDG URL; got %q", got)
	}
}

func TestImageTrack_CanonicalMiss_FallsBackToDDG(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	scpImageDir := t.TempDir() // empty — no canonical file
	res := &fakeCanonicalResolver{}    // returns ErrNotFound

	outputDir := t.TempDir()
	runID := "scp-049-run-1"
	frozen := "Appearance: slender humanoid; Environment: concrete chamber"
	scenes := []sceneFixture{
		{sceneNum: 1, narration: "Scene one.", entityVisible: true, shots: []string{"wide shot"}},
	}
	state := scenarioStateForTest(runID, scenes, frozen)
	scenarioPath := writeScenario(t, outputDir, runID, state)

	images := newFakeImageGen()
	resolver := &fakeCharacterResolver{candidate: &domain.CharacterCandidate{
		ID: "scp-049#1", PageURL: "https://example.com/049",
		ImageURL: "https://example.com/049/reference.jpg",
	}}
	fetchCalls := 0
	track, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:         outputDir,
		Provider:          "comfyui",
		GenerateModel:     "qwen-image",
		EditModel:         "qwen-image-edit",
		Width:             1024, Height: 1024,
		Images:            images,
		CharacterResolver: resolver,
		CanonicalResolver: res,
		ScpImageDir:       scpImageDir,
		Shots:             newFakeShotStore(),
		Limiter:           &passthroughLimiter{},
		Clock:             clock.RealClock{},
		RefImageFetcher: func(_ context.Context, url string) (string, error) {
			fetchCalls++
			return "data:image/jpeg;base64,FAKE-" + url, nil
		},
	})
	if err != nil {
		t.Fatalf("NewImageTrack: %v", err)
	}
	if _, err := track(context.Background(), pipeline.PhaseBRequest{
		RunID: runID, Stage: domain.StageImage, ScenarioPath: scenarioPath,
	}); err != nil {
		t.Fatalf("track: %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("expected DDG fetcher to be called once on canonical miss, got %d", fetchCalls)
	}
	if len(images.editCalls) != 1 {
		t.Fatalf("expected 1 Edit call, got %d", len(images.editCalls))
	}
	if !strings.HasPrefix(images.editCalls[0].ReferenceImageURL, "data:image/jpeg;base64,FAKE-") {
		t.Fatalf("expected fallback fetcher URL, got %q", images.editCalls[0].ReferenceImageURL)
	}
}
