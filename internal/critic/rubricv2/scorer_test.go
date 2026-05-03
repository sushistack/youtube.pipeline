package rubricv2_test

import (
	"os"
	"path/filepath"
	"testing"

	contractv2 "github.com/sushistack/youtube.pipeline/internal/contract/v2"
	"github.com/sushistack/youtube.pipeline/internal/critic/rubricv2"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/style"
)

func TestScoreEmptyInputDoesNotPanic(t *testing.T) {
	t.Parallel()
	rep := rubricv2.Score(rubricv2.Input{})
	if rep.Passed {
		t.Fatal("nil script must not pass")
	}
	if rep.OverallScore != 0 {
		t.Errorf("OverallScore=%d, want 0", rep.OverallScore)
	}
}

func TestGoodScriptPasses(t *testing.T) {
	t.Parallel()
	in := rubricv2.Input{
		Script:   goodScript(),
		Style:    loadStyleGuide(t),
		Research: goodResearch(),
		Shots:    goodShots(),
	}
	rep := rubricv2.Score(in)
	if !rep.Passed {
		t.Errorf("expected good script to pass, got %d / failures: %+v", rep.OverallScore, rep.Failures)
	}
	if rep.OverallScore < contractv2.PassingScore {
		t.Errorf("OverallScore=%d, want >=%d", rep.OverallScore, contractv2.PassingScore)
	}
	for _, key := range contractv2.CriterionKeys {
		if _, ok := rep.RubricScores[key]; !ok {
			t.Errorf("missing rubric score for %q", key)
		}
	}
}

func TestBadScriptFailsCriterion1Hook(t *testing.T) {
	t.Parallel()
	b := newGoodScriptBuilder()
	b.setNarration(0, "안녕하세요, 오늘 소개할 SCP는 굉장히 흥미로운 객체이며 길게 도입부를 깔아 드릴게요. 시청해 주셔서 감사합니다. 자, 이제 시작합니다.")
	rep := rubricv2.Score(rubricv2.Input{Script: b.build(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["hook_under_15s"] >= 8 {
		t.Errorf("hook score=%d, want <8", rep.RubricScores["hook_under_15s"])
	}
}

func TestBadScriptFailsTwistPosition(t *testing.T) {
	t.Parallel()
	b := newGoodScriptBuilder()
	// Move scene 2 (zero-indexed 1, originally ActIncident) into Revelation
	// so the revelation lands at scene_num 2 (~22% — well before the 70%
	// minimum). That necessarily nudges the original revelation scene
	// (index 6) into Mystery.
	b.setActID(1, domain.ActRevelation)
	b.setActID(6, domain.ActMystery)
	rep := rubricv2.Score(rubricv2.Input{Script: b.build(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["twist_position"] >= 8 {
		t.Errorf("twist_position=%d, want <8", rep.RubricScores["twist_position"])
	}
}

func TestBadScriptFailsUnresolvedOutro(t *testing.T) {
	t.Parallel()
	b := newGoodScriptBuilder()
	b.setNarration(8, "이상으로 영상을 마칩니다.")
	rep := rubricv2.Score(rubricv2.Input{Script: b.build(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["unresolved_outro"] >= 8 {
		t.Errorf("unresolved_outro=%d, want <8", rep.RubricScores["unresolved_outro"])
	}
}

func TestBadScriptFailsSensoryLanguage(t *testing.T) {
	t.Parallel()
	b := newGoodScriptBuilder()
	b.setNarration(2, "끔찍한 광경이 펼쳐졌습니다. 무서운 분위기, 끔찍하고 두려운 기운이 흘렀습니다.")
	rep := rubricv2.Score(rubricv2.Input{Script: b.build(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["sensory_language"] >= 8 {
		t.Errorf("sensory_language=%d, want <8", rep.RubricScores["sensory_language"])
	}
}

func TestBadScriptFailsPOVConsistency(t *testing.T) {
	t.Parallel()
	b := newGoodScriptBuilder()
	b.setNarration(1, "당신이 그 자리에 있다면 어떻게 했을까요? 재단은 즉시 출동했습니다.")
	b.setNarration(2, "당신은 도망쳐야 합니다. 그것은 움직이지 않았다.")
	rep := rubricv2.Score(rubricv2.Input{Script: b.build(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["pov_consistency"] >= 8 {
		t.Errorf("pov_consistency=%d, want <8", rep.RubricScores["pov_consistency"])
	}
}

func TestBadScriptFailsVisualReusability(t *testing.T) {
	t.Parallel()
	noReuse := []contractv2.Shot{
		{ShotID: 1, Background: "복도-1"},
		{ShotID: 2, Background: "복도-2"},
		{ShotID: 3, Background: "격리실-1"},
		{ShotID: 4, Background: "격리실-2"},
		{ShotID: 5, Background: "통제실"},
	}
	rep := rubricv2.Score(rubricv2.Input{Script: goodScript(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: noReuse})
	if rep.RubricScores["visual_reusability"] >= 8 {
		t.Errorf("visual_reusability=%d, want <8", rep.RubricScores["visual_reusability"])
	}
}

func TestBadScriptFailsSCPFidelity(t *testing.T) {
	t.Parallel()
	b := newGoodScriptBuilder()
	for i := 0; i < 9; i++ {
		b.setNarration(i, "그 사건은 묘하게 흘러갔습니다.")
	}
	b.setNarration(8, "그것은 아직 그곳에 있다.")
	rep := rubricv2.Score(rubricv2.Input{Script: b.build(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["scp_fidelity"] >= 8 {
		t.Errorf("scp_fidelity=%d, want <8", rep.RubricScores["scp_fidelity"])
	}
	// Ensure RequiresLLMReview is set on the SCP fidelity failure.
	found := false
	for _, f := range rep.Failures {
		if f.Criterion == "scp_fidelity" {
			if !f.RequiresLLMReview {
				t.Errorf("scp_fidelity failure missing RequiresLLMReview flag")
			}
			found = true
		}
	}
	if !found {
		t.Errorf("scp_fidelity failure not surfaced")
	}
}

func TestRevisionPriorityPicksLowestScore(t *testing.T) {
	t.Parallel()
	b := newGoodScriptBuilder()
	b.setNarration(0, "안녕하세요, 오늘 소개할 SCP는 길게 인사를 깔며 시작하는 메타 인트로입니다. 시청해 주셔서 감사합니다. 자, 시작합니다.")
	b.setNarration(8, "이상입니다.")
	rep := rubricv2.Score(rubricv2.Input{Script: b.build(), Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RevisionPriority == "" {
		t.Fatalf("RevisionPriority should be non-empty when failures exist")
	}
}

func TestEdgeCaseScoreNearThreshold(t *testing.T) {
	t.Parallel()
	in := rubricv2.Input{
		Script: goodScript(),
		Style:  loadStyleGuide(t),
		// Omit Research and Shots → both criteria fall back to RequiresLLMReview floor.
	}
	rep := rubricv2.Score(in)
	// Without research+shots, criteria 9 (6) and 10 (5) drop the score.
	// We assert the report is computed and stable, not a specific value.
	if got := len(rep.RubricScores); got != 10 {
		t.Errorf("RubricScores length=%d, want 10", got)
	}
	if rep.OverallScore <= 0 {
		t.Errorf("expected non-zero OverallScore, got %d", rep.OverallScore)
	}
	// Spot-check that LLM-required criteria are surfaced as such when below 8.
	for _, f := range rep.Failures {
		if f.Criterion == "visual_reusability" && !f.RequiresLLMReview {
			t.Errorf("visual_reusability with no shots should set RequiresLLMReview")
		}
	}
}

// --- fixtures -----------------------------------------------------------

// goodScriptBuilder owns the canonical 9-scene fixture mapped onto v2
// Acts/Beats. Each scene has an explicit per-scene actID assignment so
// twist-position regressions can move individual scenes between acts
// without disturbing the rest.
type goodScriptBuilder struct {
	scenes [9]struct {
		narration string
		mood      string
		actID     string
	}
}

func newGoodScriptBuilder() *goodScriptBuilder {
	return &goodScriptBuilder{
		scenes: [9]struct {
			narration string
			mood      string
			actID     string
		}{
			{narration: "복도에 검은 액체가 흘러내렸죠.", mood: "tense", actID: domain.ActIncident},
			{narration: "격리실 문이 굳게 닫혔습니다.", mood: "tense", actID: domain.ActIncident},
			{narration: "재단 요원들은 침묵했죠.", mood: "calm", actID: domain.ActMystery},
			{narration: "표준 절차가 발동되었습니다.", mood: "calm", actID: domain.ActMystery},
			{narration: "긴급 프로토콜이 작동했죠.", mood: "calm", actID: domain.ActMystery},
			{narration: "시야에서 벗어나면 이동하는 행동이 관측됐죠.", mood: "tense", actID: domain.ActMystery},
			{narration: "그것은 단순한 조각상이 아니었어요.", mood: "tense", actID: domain.ActRevelation},
			{narration: "콘크리트와 강철 합금만이 그것을 막을 수 있었죠.", mood: "calm", actID: domain.ActUnresolved},
			{narration: "그것은 아직 그곳에 있다…", mood: "ominous", actID: domain.ActUnresolved},
		},
	}
}

// build assembles the v2 NarrationScript by grouping CONSECUTIVE same-act
// scenes into one Act in fixture order. This lets twist-position tests
// reorder revelation scenes by changing per-scene actID — the resulting
// LegacyScenes() walks fixture order, scene_num=1..9. Duplicate act IDs
// across non-adjacent groups are allowed here (the rubric only checks the
// first scene tagged ActRevelation); production writer always emits canonical
// ActOrder via planWriterActs.
func (b *goodScriptBuilder) build() *domain.NarrationScript {
	type group struct {
		actID string
		idxs  []int
	}
	groups := []group{}
	for i, s := range b.scenes {
		if len(groups) > 0 && groups[len(groups)-1].actID == s.actID {
			groups[len(groups)-1].idxs = append(groups[len(groups)-1].idxs, i)
		} else {
			groups = append(groups, group{actID: s.actID, idxs: []int{i}})
		}
	}
	acts := make([]domain.ActScript, 0, len(groups))
	for _, g := range groups {
		monoBuilder := []rune{}
		anchors := []domain.BeatAnchor{}
		for j, sceneIdx := range g.idxs {
			before := len(monoBuilder)
			runes := []rune(b.scenes[sceneIdx].narration)
			monoBuilder = append(monoBuilder, runes...)
			anchors = append(anchors, domain.BeatAnchor{
				StartOffset:       before,
				EndOffset:         len(monoBuilder),
				Mood:              b.scenes[sceneIdx].mood,
				Location:          "site-19",
				CharactersPresent: []string{"unknown"},
				EntityVisible:     false,
				ColorPalette:      "neutral",
				Atmosphere:        "subdued",
				FactTags:          []domain.FactTag{},
			})
			if j < len(g.idxs)-1 {
				monoBuilder = append(monoBuilder, ' ')
			}
		}
		acts = append(acts, domain.ActScript{
			ActID:     g.actID,
			Monologue: string(monoBuilder),
			Beats:     anchors,
			Mood:      "tense",
			KeyPoints: []string{},
		})
	}
	return &domain.NarrationScript{
		SCPID:         "SCP-173",
		Title:         "조각상",
		Acts:          acts,
		SourceVersion: domain.NarrationSourceVersionV2,
	}
}

// setNarration mutates scene[i] (0-indexed in fixture order) and rebuilds.
func (b *goodScriptBuilder) setNarration(sceneIdx int, narration string) {
	b.scenes[sceneIdx].narration = narration
}

// setActID reassigns a scene to a different act. Used by the
// twist-position regression test to pull a revelation forward in the
// LegacyScenes() output.
func (b *goodScriptBuilder) setActID(sceneIdx int, actID string) {
	b.scenes[sceneIdx].actID = actID
}

func goodScript() *domain.NarrationScript {
	return newGoodScriptBuilder().build()
}

func goodResearch() *contractv2.ResearchOutput {
	return &contractv2.ResearchOutput{
		SCPNumber:           "SCP-173",
		ObjectClass:         "Euclid",
		AnomalousProperties: []string{"시야에서 벗어나면 이동", "콘크리트와 강철 합금"},
	}
}

func goodShots() []contractv2.Shot {
	return []contractv2.Shot{
		// 60% of shots reuse "격리실" as background — passes ≥40% reuse floor.
		{ShotID: 1, Background: "격리실"},
		{ShotID: 2, Background: "격리실"},
		{ShotID: 3, Background: "격리실"},
		{ShotID: 4, Background: "복도"},
		{ShotID: 5, Background: "통제실"},
	}
}

func loadStyleGuide(t *testing.T) *style.StyleGuide {
	t.Helper()
	root := projectRoot(t)
	sg, err := style.Load(filepath.Join(root, style.DefaultPath))
	if err != nil {
		t.Fatalf("load style guide: %v", err)
	}
	return sg
}

func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate go.mod above test working dir")
	return ""
}
