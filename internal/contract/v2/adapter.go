package contractv2

import (
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// FromNarration converts a v1 domain.NarrationScript into a v2
// ScriptOutput. The conversion is lossy: v2-only fields (TitleCandidates
// list, structured Attribution fields like WikiURL/License) are zero-
// valued unless the caller fills them.
//
// Per the spec section 4 migration constraint, this lets v2-aware code
// consume what the existing writer produces today without requiring the
// writer to be rewritten.
func FromNarration(in *domain.NarrationScript) ScriptOutput {
	if in == nil {
		return ScriptOutput{}
	}
	scenes := make([]Scene, len(in.Scenes))
	for i, s := range in.Scenes {
		scenes[i] = Scene{
			SceneID:         s.SceneNum,
			Section:         s.ActID,
			DurationSeconds: 0, // v1 does not carry per-scene duration
			NarrationKO:     s.Narration,
			VisualDirection: composeVisualDirection(s),
			EmotionCurve:    s.Mood,
			SFXHint:         s.Atmosphere,
		}
	}
	titles := []string{}
	if title := strings.TrimSpace(in.Title); title != "" {
		titles = []string{title}
	}
	out := ScriptOutput{
		TitleCandidates: titles,
		Scenes:          scenes,
		OutroHookKO:     extractOutroHook(in.Scenes),
		SourceAttribution: Attribution{
			SCPNumber: in.SCPID,
			License:   "CC BY-SA 3.0",
		},
	}
	return out
}

// FromResearcher converts v1 ResearcherOutput into v2 ResearchOutput.
// Fields with no v1 counterpart (Author, WikiURL, Branch, RelatedSCPs,
// LoreConnections, KoreanTerms, KeyIncidents) are left empty. Callers
// can layer those on top from external metadata sources.
func FromResearcher(in *domain.ResearcherOutput) ResearchOutput {
	if in == nil {
		return ResearchOutput{}
	}
	props := append([]string(nil), in.AnomalousProperties...)
	return ResearchOutput{
		SCPNumber:           in.SCPID,
		ObjectClass:         in.ObjectClass,
		OriginalSummary:     in.MainTextExcerpt,
		AnomalousProperties: props,
		KoreanTerms:         map[string]string{},
		KeyIncidents:        []Incident{},
		RelatedSCPs:         []string{},
		LoreConnections:     []string{},
	}
}

// FromStructurer converts v1 StructurerOutput into v2 StructureOutput.
// The v1 4-act layout (incident → mystery → revelation → unresolved)
// already encodes hook → drip → twist → cliffhanger; we surface that
// shape via NarrativeArc rather than dropping the act metadata.
func FromStructurer(in *domain.StructurerOutput) StructureOutput {
	if in == nil {
		return StructureOutput{}
	}
	arc := make([]string, 0, len(in.Acts))
	hook := ""
	twist := ""
	ending := ""
	for _, a := range in.Acts {
		arc = append(arc, a.ID+":"+a.Synopsis)
		switch a.ID {
		case domain.ActIncident:
			hook = a.Synopsis
		case domain.ActRevelation:
			twist = a.Synopsis
		case domain.ActUnresolved:
			ending = a.Synopsis
		}
	}
	return StructureOutput{
		NarrativeArc: strings.Join(arc, "\n"),
		HookAngle:    hook,
		TwistPoint:   twist,
		EndingHook:   ending,
		Tone:         "",
		SceneCount:   in.TargetSceneCount,
	}
}

// composeVisualDirection assembles a single visual direction string from
// v1 NarrationScene's scattered fields (Location, ColorPalette,
// CharactersPresent, EntityVisible). v2 wants a single composed
// direction string.
func composeVisualDirection(s domain.NarrationScene) string {
	parts := []string{}
	if s.Location != "" {
		parts = append(parts, "loc="+s.Location)
	}
	if s.ColorPalette != "" {
		parts = append(parts, "palette="+s.ColorPalette)
	}
	if len(s.CharactersPresent) > 0 {
		parts = append(parts, "chars="+strings.Join(s.CharactersPresent, ","))
	}
	if s.EntityVisible {
		parts = append(parts, "entity_visible=true")
	}
	return strings.Join(parts, "; ")
}

// extractOutroHook returns the last scene's narration as the outro hook.
// The structurer guarantees the final act is "unresolved", so the last
// scene's narration is by construction the closing hook.
func extractOutroHook(scenes []domain.NarrationScene) string {
	if len(scenes) == 0 {
		return ""
	}
	return scenes[len(scenes)-1].Narration
}
