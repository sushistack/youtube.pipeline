package domain

import "unicode/utf8"

const (
	// NarrationSourceVersionV1 is the legacy per-scene narration shape. Reserved
	// only for archival fixtures; v2 producers MUST emit NarrationSourceVersionV2.
	NarrationSourceVersionV1 = "v1-llm-writer"
	NarrationSourceVersionV2 = "v2-monologue"
	LanguageKorean           = "ko"
	// PolisherMaxEditRatio is the v1 polisher's per-scene rune-delta ceiling.
	// Retained as a constant only because the v1 polisher.go still compiles
	// against the LegacyScenes() bridge during the D1–D6 incremental migration.
	// Recalibrated in D7 against the v2 unit; the 0.40 figure is v1-specific.
	PolisherMaxEditRatio = 0.40
)

// NarrationScript v2: per-act continuous monologue + ordered BeatAnchor slices.
// "What is said" (Acts[i].Monologue) is now decoupled from "what is shown"
// (Acts[i].Beats[j] visual metadata + offset slice into Monologue).
type NarrationScript struct {
	SCPID         string            `json:"scp_id"`
	Title         string            `json:"title"`
	Acts          []ActScript       `json:"acts"`
	Metadata      NarrationMetadata `json:"metadata"`
	SourceVersion string            `json:"source_version"`
}

// ActScript replaces the v1 per-scene NarrationScene array as the act-level
// narration unit. Monologue is a single continuous KR text; Beats segment that
// text into visual cuts via rune-offset slices.
type ActScript struct {
	ActID     string       `json:"act_id"`
	Monologue string       `json:"monologue"`
	Beats     []BeatAnchor `json:"beats"`
	Mood      string       `json:"mood"`
	KeyPoints []string     `json:"key_points"`
}

// BeatAnchor anchors one visual shot to a contiguous rune-offset slice of an
// ActScript.Monologue. Offsets are rune indices (utf8.RuneCountInString),
// half-open [StartOffset, EndOffset). Stage-2 validator enforces 8–10 beats
// per act with monotonic non-overlapping in-range offsets.
type BeatAnchor struct {
	StartOffset       int       `json:"start_offset"`
	EndOffset         int       `json:"end_offset"`
	Mood              string    `json:"mood"`
	Location          string    `json:"location"`
	CharactersPresent []string  `json:"characters_present"`
	EntityVisible     bool      `json:"entity_visible"`
	ColorPalette      string    `json:"color_palette"`
	Atmosphere        string    `json:"atmosphere"`
	FactTags          []FactTag `json:"fact_tags"`
}

// NarrationScene is the v1 per-scene shape, kept ONLY as the return element
// type of (*NarrationScript).LegacyScenes(). Every downstream consumer
// reaches it through the bridge during the D1–D6 migration; the bridge is
// deleted in the same PR as the last consumer (D6).
//
// Deprecated: use ActScript / BeatAnchor. The bridge LegacyScenes() exists
// purely to keep v1 agents (visual_breakdowner v1, polisher v1, scene_service)
// compiling and functionally green until D2/D4/D6 migrate them off.
type NarrationScene struct {
	SceneNum          int       `json:"scene_num"`
	ActID             string    `json:"act_id"`
	Narration         string    `json:"narration"`
	NarrationBeats    []string  `json:"narration_beats"`
	FactTags          []FactTag `json:"fact_tags"`
	Mood              string    `json:"mood"`
	EntityVisible     bool      `json:"entity_visible"`
	Location          string    `json:"location"`
	CharactersPresent []string  `json:"characters_present"`
	ColorPalette      string    `json:"color_palette"`
	Atmosphere        string    `json:"atmosphere"`
}

type FactTag struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}

type NarrationMetadata struct {
	Language              string `json:"language"`
	SceneCount            int    `json:"scene_count"`
	WriterModel           string `json:"writer_model"`
	WriterProvider        string `json:"writer_provider"`
	PromptTemplate        string `json:"prompt_template"`
	FormatGuideTemplate   string `json:"format_guide_template"`
	ForbiddenTermsVersion string `json:"forbidden_terms_version"`
}

// LegacyScenes derives the v1 []NarrationScene shape from v2 Acts/Beats.
// One scene per beat; the beat's rune slice from Monologue becomes
// NarrationScene.Narration. SceneNum is 1-indexed in beat order.
//
// Deprecated: the bridge is deleted in the PR that lands the last v2 consumer.
// See spec-d1-domain-types-and-writer-v2.md "Design Notes". Caller must
// not mutate the result and expect it to round-trip back into Acts — the
// bridge is in-memory, read-only.
func (n *NarrationScript) LegacyScenes() []NarrationScene {
	if n == nil || len(n.Acts) == 0 {
		return nil
	}
	total := 0
	for _, act := range n.Acts {
		total += len(act.Beats)
	}
	scenes := make([]NarrationScene, 0, total)
	sceneNum := 1
	for _, act := range n.Acts {
		runes := []rune(act.Monologue)
		runeLen := len(runes)
		for _, beat := range act.Beats {
			start := beat.StartOffset
			end := beat.EndOffset
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
			narration := string(runes[start:end])
			scenes = append(scenes, NarrationScene{
				SceneNum:          sceneNum,
				ActID:             act.ActID,
				Narration:         narration,
				NarrationBeats:    []string{narration},
				FactTags:          beat.FactTags,
				Mood:              firstNonEmpty(beat.Mood, act.Mood),
				EntityVisible:     beat.EntityVisible,
				Location:          beat.Location,
				CharactersPresent: beat.CharactersPresent,
				ColorPalette:      beat.ColorPalette,
				Atmosphere:        beat.Atmosphere,
			})
			sceneNum++
		}
	}
	return scenes
}

// MonologueRuneCount returns the total rune count across all act monologues.
// Used by phase-A acceptance gates and the golden-eval rubric (≥4500 floor
// per Lever P parity).
func (n *NarrationScript) MonologueRuneCount() int {
	if n == nil {
		return 0
	}
	total := 0
	for _, act := range n.Acts {
		total += utf8.RuneCountInString(act.Monologue)
	}
	return total
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
