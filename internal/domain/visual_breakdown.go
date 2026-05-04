package domain

const (
	// VisualBreakdownSourceVersionV1 is the legacy per-scene visual_breakdown
	// shape. Reserved only as a const for archival fixtures; v2 producers MUST
	// emit VisualBreakdownSourceVersionV2.
	VisualBreakdownSourceVersionV1 = "v1-visual-breakdown"
	VisualBreakdownSourceVersionV2 = "v2-visual"
	ShotFormulaVersionV1           = "tts-duration-v1"

	TransitionKenBurns      = "ken_burns"
	TransitionCrossDissolve = "cross_dissolve"
	TransitionHardCut       = "hard_cut"
)

// VisualScript v2: per-act visual planning unit. Top-level Acts replace v1's
// flat Scenes array; each VisualAct emits one VisualShot per source BeatAnchor
// (1:1). FrozenDescriptor / ShotOverrides / Metadata semantics carry from v1.
//
// ShotOverrides retains the v1 type `map[int]ShotOverride`, but the int key
// SEMANTIC shifts to "global 1-indexed shot number across the flattened
// Acts[*].Shots[*] sequence" — the same numbering produced by LegacyScenes()
// for v1-shaped consumers (D2 spec, Design Notes #3). Image-prompt assembly
// is intentionally NOT redesigned in D2 (frozen-block constraint).
type VisualScript struct {
	SCPID            string                  `json:"scp_id"`
	Title            string                  `json:"title"`
	FrozenDescriptor string                  `json:"frozen_descriptor"`
	Acts             []VisualAct             `json:"acts"`
	ShotOverrides    map[int]ShotOverride    `json:"shot_overrides"`
	Metadata         VisualBreakdownMetadata `json:"metadata"`
	SourceVersion    string                  `json:"source_version"`
}

// VisualAct groups one act's shots. ActID matches the upstream
// NarrationScript.Acts[k].ActID. Shots length == len(NarrationScript.Acts[k].Beats)
// (1:1 invariant enforced by the visual_breakdowner anchor-equality validator).
type VisualAct struct {
	ActID string       `json:"act_id"`
	Shots []VisualShot `json:"shots"`
}

// VisualShot is one rendered image plan. NarrationAnchor carries the source
// BeatAnchor verbatim (every field byte-for-byte) so downstream consumers
// (image regen, TTS independence) can resolve the rune slice into the act
// monologue without having to round-trip through a separate text echo.
type VisualShot struct {
	ShotIndex          int        `json:"shot_index"`
	VisualDescriptor   string     `json:"visual_descriptor"`
	EstimatedDurationS float64    `json:"estimated_duration_s"`
	Transition         string     `json:"transition"`
	NarrationAnchor    BeatAnchor `json:"narration_anchor"`
}

type ShotOverride struct {
	ShotCount  *int    `json:"shot_count,omitempty"`
	Transition *string `json:"transition,omitempty"`
}

type VisualBreakdownMetadata struct {
	VisualBreakdownModel    string `json:"visual_breakdown_model"`
	VisualBreakdownProvider string `json:"visual_breakdown_provider"`
	PromptTemplate          string `json:"prompt_template"`
	ShotFormulaVersion      string `json:"shot_formula_version"`
}

// VisualBreakdownScene is the v1 per-scene shape, kept ONLY as the return
// element type of (*VisualScript).LegacyScenes(). Every downstream visual
// consumer reaches it through the bridge during the D2–D6 migration; the
// bridge is deleted in the same PR as the last consumer.
//
// Deprecated: use VisualScript / VisualAct / VisualShot. The bridge
// LegacyScenes() exists purely to keep v1 consumers (image_track v1,
// reviewer/critic v1, scene_service v1) compiling and functionally green
// until D3/D4/D6 migrate them off.
//
// TODO(D-vis-final): remove with last visual consumer migration.
type VisualBreakdownScene struct {
	SceneNum              int            `json:"scene_num"`
	ActID                 string         `json:"act_id"`
	Narration             string         `json:"narration"`
	EstimatedTTSDurationS float64        `json:"estimated_tts_duration_s"`
	ShotCount             int            `json:"shot_count"`
	Shots                 []LegacyShotV1 `json:"shots"`
}

// LegacyShotV1 is the v1 VisualShot shape, surfaced ONLY by the
// (*VisualScript).LegacyScenes() bridge so v1 consumers that read
// NarrationBeatIndex / NarrationBeatText keep compiling during D2–D6.
//
// Deprecated: use VisualShot. Removed alongside VisualBreakdownScene.
//
// TODO(D-vis-final): remove with last visual consumer migration.
type LegacyShotV1 struct {
	ShotIndex          int     `json:"shot_index"`
	VisualDescriptor   string  `json:"visual_descriptor"`
	EstimatedDurationS float64 `json:"estimated_duration_s"`
	Transition         string  `json:"transition"`
	NarrationBeatIndex int     `json:"narration_beat_index"`
	NarrationBeatText  string  `json:"narration_beat_text"`
}

// LegacyScenes derives the v1 []VisualBreakdownScene shape from v2
// Acts/Shots, sliced against the supplied NarrationScript so the v1
// `Narration` field can be reconstructed from the rune offsets carried in
// each shot's NarrationAnchor. One scene per shot; SceneNum is 1-indexed
// in the global flattened (act, shot) order — the same numbering
// ShotOverrides keys against per Design Notes #3.
//
// Deprecated: the bridge is deleted in the PR that lands the last v2
// consumer. See spec-d2-visual-breakdowner-v2.md "Design Notes". Caller
// must not mutate the result and expect it to round-trip back into
// VisualScript.Acts — the bridge is in-memory, read-only.
//
// TODO(D-vis-final): remove with last visual consumer migration.
func (v *VisualScript) LegacyScenes(narration *NarrationScript) []VisualBreakdownScene {
	if v == nil || len(v.Acts) == 0 {
		return nil
	}
	// Index narration acts by ActID for monologue lookup. Falls back to
	// positional match when narration is nil (test fixtures may bridge
	// without narration; the fallback yields empty Narration strings).
	monologueByAct := map[string]string{}
	if narration != nil {
		for _, act := range narration.Acts {
			monologueByAct[act.ActID] = act.Monologue
		}
	}
	total := 0
	for _, act := range v.Acts {
		total += len(act.Shots)
	}
	scenes := make([]VisualBreakdownScene, 0, total)
	sceneNum := 1
	for _, act := range v.Acts {
		runes := []rune(monologueByAct[act.ActID])
		runeLen := len(runes)
		for shotIdx, shot := range act.Shots {
			anchor := shot.NarrationAnchor
			start := anchor.StartOffset
			end := anchor.EndOffset
			if start < 0 {
				start = 0
			}
			if start > runeLen {
				start = runeLen
			}
			if end > runeLen {
				end = runeLen
			}
			if end < start {
				end = start
			}
			narration := ""
			if runeLen > 0 {
				narration = string(runes[start:end])
			}
			legacyShot := LegacyShotV1{
				ShotIndex:          1,
				VisualDescriptor:   shot.VisualDescriptor,
				EstimatedDurationS: shot.EstimatedDurationS,
				Transition:         shot.Transition,
				NarrationBeatIndex: 0,
				NarrationBeatText:  narration,
			}
			scenes = append(scenes, VisualBreakdownScene{
				SceneNum:              sceneNum,
				ActID:                 act.ActID,
				Narration:             narration,
				EstimatedTTSDurationS: shot.EstimatedDurationS,
				ShotCount:             1,
				Shots:                 []LegacyShotV1{legacyShot},
			})
			sceneNum++
			_ = shotIdx
		}
	}
	return scenes
}
