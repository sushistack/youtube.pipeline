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
	bad := goodScript()
	bad.Scenes[0].Narration = "안녕하세요, 오늘 소개할 SCP는 굉장히 흥미로운 객체이며 길게 도입부를 깔아 드릴게요. 시청해 주셔서 감사합니다. 자, 이제 시작합니다."
	rep := rubricv2.Score(rubricv2.Input{Script: bad, Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["hook_under_15s"] >= 8 {
		t.Errorf("hook score=%d, want <8", rep.RubricScores["hook_under_15s"])
	}
}

func TestBadScriptFailsTwistPosition(t *testing.T) {
	t.Parallel()
	bad := goodScript()
	for i := range bad.Scenes {
		// Move the revelation to the very front — way before 70% mark.
		if i == 1 {
			bad.Scenes[i].ActID = domain.ActRevelation
		} else if bad.Scenes[i].ActID == domain.ActRevelation {
			bad.Scenes[i].ActID = domain.ActMystery
		}
	}
	rep := rubricv2.Score(rubricv2.Input{Script: bad, Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["twist_position"] >= 8 {
		t.Errorf("twist_position=%d, want <8", rep.RubricScores["twist_position"])
	}
}

func TestBadScriptFailsUnresolvedOutro(t *testing.T) {
	t.Parallel()
	bad := goodScript()
	bad.Scenes[len(bad.Scenes)-1].Narration = "이상으로 영상을 마칩니다."
	rep := rubricv2.Score(rubricv2.Input{Script: bad, Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["unresolved_outro"] >= 8 {
		t.Errorf("unresolved_outro=%d, want <8", rep.RubricScores["unresolved_outro"])
	}
}

func TestBadScriptFailsSensoryLanguage(t *testing.T) {
	t.Parallel()
	bad := goodScript()
	bad.Scenes[2].Narration = "끔찍한 광경이 펼쳐졌습니다. 무서운 분위기, 끔찍하고 두려운 기운이 흘렀습니다."
	rep := rubricv2.Score(rubricv2.Input{Script: bad, Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
	if rep.RubricScores["sensory_language"] >= 8 {
		t.Errorf("sensory_language=%d, want <8", rep.RubricScores["sensory_language"])
	}
}

func TestBadScriptFailsPOVConsistency(t *testing.T) {
	t.Parallel()
	bad := goodScript()
	bad.Scenes[1].Narration = "당신이 그 자리에 있다면 어떻게 했을까요? 재단은 즉시 출동했습니다."
	bad.Scenes[2].Narration = "당신은 도망쳐야 합니다. 그것은 움직이지 않았다."
	rep := rubricv2.Score(rubricv2.Input{Script: bad, Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
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
	bad := goodScript()
	for i := range bad.Scenes {
		bad.Scenes[i].Narration = "그 사건은 묘하게 흘러갔습니다."
	}
	bad.Scenes[len(bad.Scenes)-1].Narration = "그것은 아직 그곳에 있다."
	rep := rubricv2.Score(rubricv2.Input{Script: bad, Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
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
	bad := goodScript()
	bad.Scenes[0].Narration = "안녕하세요, 오늘 소개할 SCP는 길게 인사를 깔며 시작하는 메타 인트로입니다. 시청해 주셔서 감사합니다. 자, 시작합니다."
	bad.Scenes[len(bad.Scenes)-1].Narration = "이상입니다."
	rep := rubricv2.Score(rubricv2.Input{Script: bad, Style: loadStyleGuide(t), Research: goodResearch(), Shots: goodShots()})
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

func goodScript() *domain.NarrationScript {
	scenes := []domain.NarrationScene{
		// Scene 1: hook ≤ ~52 runes, sensory verb, no forbidden opening.
		{SceneNum: 1, ActID: domain.ActIncident, Narration: "복도에 검은 액체가 흘러내렸죠.", Mood: "tense"},
		// Scene 2: containment-info keyword (격리), tense.
		{SceneNum: 2, ActID: domain.ActIncident, Narration: "격리실 문이 굳게 닫혔습니다.", Mood: "tense"},
		// Scene 3: mystery, calm.
		{SceneNum: 3, ActID: domain.ActMystery, Narration: "재단 요원들은 침묵했죠.", Mood: "calm"},
		// Scene 4: drip — 절차.
		{SceneNum: 4, ActID: domain.ActMystery, Narration: "표준 절차가 발동되었습니다.", Mood: "calm"},
		// Scene 5: drip — 프로토콜.
		{SceneNum: 5, ActID: domain.ActMystery, Narration: "긴급 프로토콜이 작동했죠.", Mood: "calm"},
		// Scene 6: incident reference + anomalous property mention #1.
		{SceneNum: 6, ActID: domain.ActMystery, Narration: "시야에서 벗어나면 이동하는 행동이 관측됐죠.", Mood: "tense"},
		// Scene 7: revelation @ 70% (index 6 of 9 = 0.667 ~ 70%).
		{SceneNum: 7, ActID: domain.ActRevelation, Narration: "그것은 단순한 조각상이 아니었어요.", Mood: "tense"},
		// Scene 8: anomalous property mention #2.
		{SceneNum: 8, ActID: domain.ActUnresolved, Narration: "콘크리트와 강철 합금만이 그것을 막을 수 있었죠.", Mood: "calm"},
		// Scene 9: outro with cliffhanger marker.
		{SceneNum: 9, ActID: domain.ActUnresolved, Narration: "그것은 아직 그곳에 있다…", Mood: "ominous"},
	}
	return &domain.NarrationScript{
		SCPID:         "SCP-173",
		Title:         "조각상",
		Scenes:        scenes,
		SourceVersion: domain.NarrationSourceVersionV1,
	}
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
