package contractv2_test

import (
	"strings"
	"testing"

	contractv2 "github.com/sushistack/youtube.pipeline/internal/contract/v2"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

func TestFromNarrationLossyAdapter(t *testing.T) {
	t.Parallel()
	in := &domain.NarrationScript{
		SCPID: "SCP-173",
		Title: "조각상",
		Scenes: []domain.NarrationScene{
			{SceneNum: 1, ActID: "incident", Narration: "검은 액체가 흘러내렸죠.", Mood: "tense", Location: "복도", ColorPalette: "암녹색", CharactersPresent: []string{"D-1099"}, EntityVisible: true, Atmosphere: "정적"},
			{SceneNum: 2, ActID: "mystery", Narration: "그것은 움직이지 않았다.", Mood: "calm", Location: "격리실"},
			{SceneNum: 3, ActID: "unresolved", Narration: "그것은 아직 그곳에 있다.", Mood: "ominous"},
		},
		SourceVersion: domain.NarrationSourceVersionV1,
	}
	got := contractv2.FromNarration(in)
	if len(got.Scenes) != 3 {
		t.Fatalf("scenes=%d, want 3", len(got.Scenes))
	}
	if got.Scenes[0].SceneID != 1 || got.Scenes[0].Section != "incident" {
		t.Errorf("scene 1 = %+v", got.Scenes[0])
	}
	if got.Scenes[0].EmotionCurve != "tense" {
		t.Errorf("EmotionCurve mapped from Mood: got %q", got.Scenes[0].EmotionCurve)
	}
	if got.Scenes[0].NarrationKO != "검은 액체가 흘러내렸죠." {
		t.Errorf("NarrationKO mismatch: %q", got.Scenes[0].NarrationKO)
	}
	if !strings.Contains(got.Scenes[0].VisualDirection, "loc=복도") {
		t.Errorf("VisualDirection missing location: %q", got.Scenes[0].VisualDirection)
	}
	if !strings.Contains(got.Scenes[0].VisualDirection, "entity_visible=true") {
		t.Errorf("VisualDirection missing entity flag: %q", got.Scenes[0].VisualDirection)
	}
	if got.OutroHookKO != "그것은 아직 그곳에 있다." {
		t.Errorf("OutroHookKO=%q", got.OutroHookKO)
	}
	if len(got.TitleCandidates) != 1 || got.TitleCandidates[0] != "조각상" {
		t.Errorf("TitleCandidates=%v", got.TitleCandidates)
	}
	if got.SourceAttribution.SCPNumber != "SCP-173" || got.SourceAttribution.License != "CC BY-SA 3.0" {
		t.Errorf("SourceAttribution=%+v", got.SourceAttribution)
	}
}

func TestFromNarrationNilSafe(t *testing.T) {
	t.Parallel()
	got := contractv2.FromNarration(nil)
	if len(got.Scenes) != 0 || got.OutroHookKO != "" {
		t.Errorf("nil input must yield zero value, got %+v", got)
	}
}

func TestFromStructurerMapsActsToArc(t *testing.T) {
	t.Parallel()
	in := &domain.StructurerOutput{
		SCPID: "SCP-173",
		Acts: []domain.Act{
			{ID: domain.ActIncident, Synopsis: "최초 격리 실패"},
			{ID: domain.ActMystery, Synopsis: "조사관의 의문"},
			{ID: domain.ActRevelation, Synopsis: "진짜 위협 공개"},
			{ID: domain.ActUnresolved, Synopsis: "아직 그곳에"},
		},
		TargetSceneCount: 12,
	}
	got := contractv2.FromStructurer(in)
	if got.HookAngle != "최초 격리 실패" {
		t.Errorf("HookAngle=%q", got.HookAngle)
	}
	if got.TwistPoint != "진짜 위협 공개" {
		t.Errorf("TwistPoint=%q", got.TwistPoint)
	}
	if got.EndingHook != "아직 그곳에" {
		t.Errorf("EndingHook=%q", got.EndingHook)
	}
	if got.SceneCount != 12 {
		t.Errorf("SceneCount=%d", got.SceneCount)
	}
	if !strings.Contains(got.NarrativeArc, "incident:") || !strings.Contains(got.NarrativeArc, "unresolved:") {
		t.Errorf("NarrativeArc missing act ids: %q", got.NarrativeArc)
	}
}

func TestFromResearcherCopiesAnomalousProperties(t *testing.T) {
	t.Parallel()
	in := &domain.ResearcherOutput{
		SCPID:               "SCP-173",
		ObjectClass:         "Euclid",
		AnomalousProperties: []string{"시야에서 벗어나면 이동", "콘크리트와 강철 합금"},
		MainTextExcerpt:     "조각상 형태의 적대적 개체.",
	}
	got := contractv2.FromResearcher(in)
	if got.SCPNumber != "SCP-173" || got.ObjectClass != "Euclid" {
		t.Errorf("scalar fields wrong: %+v", got)
	}
	if len(got.AnomalousProperties) != 2 {
		t.Errorf("AnomalousProperties=%v", got.AnomalousProperties)
	}
	if got.OriginalSummary == "" {
		t.Errorf("OriginalSummary should map from MainTextExcerpt")
	}
	// Ensure adapter does not alias the source slice — mutating the source
	// must not corrupt the v2 copy.
	in.AnomalousProperties[0] = "변경"
	if got.AnomalousProperties[0] == "변경" {
		t.Errorf("adapter must copy AnomalousProperties slice, not alias it")
	}
}
