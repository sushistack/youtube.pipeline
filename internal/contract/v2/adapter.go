package contractv2

import (
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// FromNarration converts a v2 domain.NarrationScript into a v2 contract
// ScriptOutput. The conversion is best-effort: contract-only fields the
// pipeline does not produce (TitleCandidates list, structured Attribution
// fields like WikiURL/License) are zero-valued unless the caller fills them.
//
// Each beat in the source script becomes one Scene in the output, indexed
// 1..N in flat beat order across all acts. NarrationKO carries the beat's
// rune-slice text from its parent act's Monologue.
func FromNarration(in *domain.NarrationScript) ScriptOutput {
	if in == nil {
		return ScriptOutput{}
	}
	beats := in.FlatBeats()
	scenes := make([]Scene, len(beats))
	for i, beat := range beats {
		scenes[i] = Scene{
			SceneID:         beat.Index,
			Section:         beat.ActID,
			DurationSeconds: 0,
			NarrationKO:     beat.Text,
			VisualDirection: composeVisualDirection(beat.Anchor),
			EmotionCurve:    firstNonEmptyAdapter(beat.Anchor.Mood, beat.ActMood),
			SFXHint:         beat.Anchor.Atmosphere,
		}
	}
	titles := []string{}
	if title := strings.TrimSpace(in.Title); title != "" {
		titles = []string{title}
	}
	out := ScriptOutput{
		TitleCandidates: titles,
		Scenes:          scenes,
		OutroHookKO:     extractOutroHook(beats),
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
// the v2 BeatAnchor metadata fields (Location, ColorPalette, CharactersPresent,
// EntityVisible). v2 contract Scene wants a single composed direction string.
func composeVisualDirection(b domain.BeatAnchor) string {
	parts := []string{}
	if b.Location != "" {
		parts = append(parts, "loc="+b.Location)
	}
	if b.ColorPalette != "" {
		parts = append(parts, "palette="+b.ColorPalette)
	}
	if len(b.CharactersPresent) > 0 {
		parts = append(parts, "chars="+strings.Join(b.CharactersPresent, ","))
	}
	if b.EntityVisible {
		parts = append(parts, "entity_visible=true")
	}
	return strings.Join(parts, "; ")
}

// extractOutroHook returns the last beat's narration as the outro hook.
// The structurer guarantees the final act is "unresolved", so the last
// beat's text is by construction the closing hook.
func extractOutroHook(beats []domain.NarrationBeatView) string {
	if len(beats) == 0 {
		return ""
	}
	return beats[len(beats)-1].Text
}

func firstNonEmptyAdapter(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
