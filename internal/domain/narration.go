package domain

import "unicode/utf8"

const (
	// NarrationSourceVersionV2 is the canonical writer source-version emitted
	// by D1+ writer v2. v1 strings are no longer recognized — the per-scene
	// shape and its bridge died with D4 (clean cut per the D plan).
	NarrationSourceVersionV2 = "v2-monologue"
	LanguageKorean           = "ko"
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

// ActScript is the act-level narration unit. Monologue is a single continuous
// KR text; Beats segment that text into visual cuts via rune-offset slices.
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

// NarrationBeatView is a v2 read-only flat projection of one BeatAnchor with
// its parent act context resolved. It carries the parent ActID + ActMood
// fall-through and the rune slice text from the parent monologue. Returned
// by NarrationScript.FlatBeats() in stable beat order.
//
// Index is 1-based across all acts and matches the segments table's
// scene_index = Index - 1 convention. Downstream consumers that previously
// keyed on v1 NarrationScene.SceneNum can key on Index unchanged.
//
// Anchor is a copy of the source BeatAnchor; mutating it does not propagate
// back to the parent NarrationScript. ActMood is the parent ActScript.Mood —
// callers should fall through to it when Anchor.Mood is empty.
type NarrationBeatView struct {
	Index   int
	ActID   string
	ActMood string
	Anchor  BeatAnchor
	Text    string
}

// FlatBeats returns a flat 1-based ordered view over every beat in the script.
// The view materializes each beat's rune-slice text from its parent act's
// Monologue once, which lets downstream consumers read the per-beat narration
// text without re-slicing offsets themselves. Slices are clamped to the
// monologue's rune length defensively; out-of-range offsets produce empty
// text, never a panic.
//
// Returns nil for nil receiver, nil for an empty Acts list. The returned
// slice has length sum(len(act.Beats) for act in Acts).
func (n *NarrationScript) FlatBeats() []NarrationBeatView {
	if n == nil || len(n.Acts) == 0 {
		return nil
	}
	total := 0
	for _, act := range n.Acts {
		total += len(act.Beats)
	}
	out := make([]NarrationBeatView, 0, total)
	idx := 1
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
			out = append(out, NarrationBeatView{
				Index:   idx,
				ActID:   act.ActID,
				ActMood: act.Mood,
				Anchor:  beat,
				Text:    string(runes[start:end]),
			})
			idx++
		}
	}
	return out
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

// ActByID returns a pointer to the ActScript with the matching ID, or nil
// when no act has that ID. Mutations through the returned pointer DO
// propagate back into the script — callers that need an immutable copy
// should dereference and copy.
func (n *NarrationScript) ActByID(actID string) *ActScript {
	if n == nil || actID == "" {
		return nil
	}
	for i := range n.Acts {
		if n.Acts[i].ActID == actID {
			return &n.Acts[i]
		}
	}
	return nil
}

// BeatIndexAt returns the 1-based flat beat index for the beat in the named
// act whose [StartOffset, EndOffset) range contains runeOffset. Returns 0
// when no such beat exists (act_id not found, runeOffset out of range, or
// the act has no beats). The returned index matches NarrationBeatView.Index
// from FlatBeats().
//
// runeOffset is a half-open inclusive-on-the-left coordinate within the
// parent act's Monologue, in rune units. An offset that lands exactly on a
// beat boundary maps to the trailing beat (the one whose StartOffset equals
// runeOffset).
func (n *NarrationScript) BeatIndexAt(actID string, runeOffset int) int {
	if n == nil || actID == "" || runeOffset < 0 {
		return 0
	}
	flatIdx := 0
	for _, act := range n.Acts {
		for _, beat := range act.Beats {
			flatIdx++
			if act.ActID != actID {
				continue
			}
			if runeOffset >= beat.StartOffset && runeOffset < beat.EndOffset {
				return flatIdx
			}
			// A boundary hit (runeOffset == beat.StartOffset) is included
			// above (>=), so a trailing-edge match (runeOffset ==
			// beat.EndOffset) belongs to the next beat in iteration order.
		}
	}
	return 0
}
