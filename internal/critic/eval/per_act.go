package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// PerAct beat-count window per D1 spec (stage-2 segmenter validator).
const (
	BeatCountMin = 8
	BeatCountMax = 10
)

// ActMetric captures per-act deterministic measurements for one fixture.
// All fields are derived without an LLM call from the parsed
// domain.NarrationScript; this scorer is the v2 baseline that v1's
// per-scene rubric collapses to when projected onto monologue acts.
//
// All bool fields are serialized without `omitempty` so the failure case
// (`false`) survives JSON round-trips. The v1 reviewer audit caught
// PrevSeamMonotonic/RuneCapOverflow silently disappearing for the
// failure value because Go's `omitempty` drops the bool zero value.
type ActMetric struct {
	ActID                string  `json:"act_id"`
	UnknownActID         bool    `json:"unknown_act_id"`
	MonologueRuneCount   int     `json:"monologue_rune_count"`
	RuneCap              int     `json:"rune_cap"`
	RuneCapUtilization   float64 `json:"rune_cap_utilization"`
	RuneCapOverflow      bool    `json:"rune_cap_overflow"`
	BeatCount            int     `json:"beat_count"`
	BeatCountInRange     bool    `json:"beat_count_in_range"`
	MoodPresent          bool    `json:"mood_present"`
	KeyPointsPresent     bool    `json:"key_points_present"`
	MetadataComplete     bool    `json:"metadata_complete"`
	OffsetsValid         bool    `json:"offsets_valid"`
	PrevSeamMonotonic    bool    `json:"prev_seam_monotonic"`
}

// FixtureActReport is the per-act metric set for a single fixture.
type FixtureActReport struct {
	FixtureID  string      `json:"fixture_id"`
	Acts       []ActMetric `json:"acts"`
	TotalRunes int         `json:"total_monologue_runes"`
}

// PerActAggregate is the run-level rollup of per-act metrics across all
// fixtures evaluated in a Golden run. Aggregates surface as both summary
// numbers (average utilization, total acts with metadata gaps) and a
// per-criterion pass count so v1→v2 per-criterion comparability tables
// can be assembled without re-walking individual fixtures.
//
// Some signals double-count by design — e.g., an act with zero beats fails
// `BeatCountInRange`, `OffsetsValid`, AND `PrevSeamMonotonic`. The aggregate
// keeps each counter independent so a reader can correlate "which floor
// broke first" against the per-fixture detail rather than collapsing the
// signal up-front. The doc comment on each counter explains its semantic.
type PerActAggregate struct {
	FixtureCount         int                  `json:"fixture_count"`
	ActCount             int                  `json:"act_count"`
	UnknownActIDActs     int                  `json:"unknown_act_id_acts"`
	AvgRuneCapUtilization float64             `json:"avg_rune_cap_utilization"`
	ActsWithRuneOverflow int                  `json:"acts_with_rune_overflow"`
	ActsWithBadBeatCount int                  `json:"acts_with_bad_beat_count"`
	ActsWithMetadataGap  int                  `json:"acts_with_metadata_gap"`
	ActsWithBadOffsets   int                  `json:"acts_with_bad_offsets"`
	ActsWithSeamGap      int                  `json:"acts_with_seam_gap"`
	Fixtures             []FixtureActReport   `json:"fixtures,omitempty"`
}

// computeFixtureActReport derives per-act metrics for one fixture by parsing
// its Input as a v2 NarrationScript. Returns a zero report and a
// domain.ErrValidation-wrapped error if the input cannot be parsed; the
// caller decides whether to surface or continue.
func computeFixtureActReport(f Fixture) (FixtureActReport, error) {
	var script domain.NarrationScript
	if err := json.Unmarshal(f.Input, &script); err != nil {
		return FixtureActReport{}, fmt.Errorf("per-act: parse fixture input: %w: %v", domain.ErrValidation, err)
	}

	report := FixtureActReport{FixtureID: f.FixtureID}
	for i, act := range script.Acts {
		runes := utf8.RuneCountInString(act.Monologue)
		// runeCap (not `cap` — would shadow the Go builtin and is a slot for a
		// future maintainer to reach for `cap(slice)` and silently get the int).
		runeCap, hasCap := domain.ActMonologueRuneCap[act.ActID]

		var utilization float64
		overflow := false
		if hasCap && runeCap > 0 {
			utilization = float64(runes) / float64(runeCap)
			overflow = runes > runeCap
		}

		// Beat count window per D1 spec: stage-2 validator rejects outside [8, 10].
		beatCount := len(act.Beats)
		beatInRange := beatCount >= BeatCountMin && beatCount <= BeatCountMax

		// Metadata gate: D2/D4 forbidden-term + per-act metadata gate enforces
		// non-empty mood + key_points. We re-check here so a fixture that
		// somehow bypassed upstream validation still surfaces the gap.
		moodOK := act.Mood != ""
		keyPointsOK := len(act.KeyPoints) > 0

		// Offset validity: beats must be monotonic non-overlapping in-range
		// rune-offset slices into Monologue. D1 stage-2 validator already
		// enforces this on the writer path, but golden eval still re-checks
		// because golden fixtures bypass the writer.
		offsetsOK := beatsValid(runes, act.Beats)

		// Act-seam continuity: i-th act must continue from (i-1)-th act
		// without leaving a hole or going backward in narrative flow. For
		// per-act metrics computed in isolation we treat the seam as
		// monotonic when the previous act's last beat ended on its
		// monologue boundary (no truncation) AND the current act's first
		// beat starts at offset 0. Stronger semantic continuity (e.g.,
		// topical bridge between acts) is an LLM-rubric concern that v2's
		// 4-criterion rubric still owns; this metric is a deterministic floor.
		prevSeamOK := true
		if i > 0 {
			prev := script.Acts[i-1]
			prevSeamOK = actSeamMonotonic(prev, act)
		}

		report.Acts = append(report.Acts, ActMetric{
			ActID:                act.ActID,
			UnknownActID:         !hasCap,
			MonologueRuneCount:   runes,
			RuneCap:              runeCap,
			RuneCapUtilization:   utilization,
			RuneCapOverflow:      overflow,
			BeatCount:            beatCount,
			BeatCountInRange:     beatInRange,
			MoodPresent:          moodOK,
			KeyPointsPresent:     keyPointsOK,
			MetadataComplete:     moodOK && keyPointsOK,
			OffsetsValid:         offsetsOK,
			PrevSeamMonotonic:    prevSeamOK,
		})
		report.TotalRunes += runes
	}
	return report, nil
}

// beatsValid mirrors the D1 stage-2 validator: monotonic non-overlapping
// rune-offset slices in [0, monoRunes]. We re-implement here rather than
// importing internal/pipeline/agents to keep internal/critic/eval's import
// surface to internal/domain only (per layer-import rules).
//
// In addition to the writer-side validator's checks, this guards the v2
// regression scenario where a corrupted fixture has zero-width slices
// (StartOffset == EndOffset) or an empty monologue with all-zero offsets.
// Both pass the writer's `EndOffset > monoRunes` check (because
// monoRunes==0 admits EndOffset==0 trivially) but cover no monologue
// text — clearly broken even though the writer-validator accepted them.
func beatsValid(monoRunes int, beats []domain.BeatAnchor) bool {
	if len(beats) == 0 {
		return false
	}
	if monoRunes <= 0 {
		// An empty monologue cannot host any non-empty beat slice. Any beat
		// list is an invalid mapping by definition.
		return false
	}
	prevEnd := 0
	for _, b := range beats {
		if b.StartOffset < 0 || b.EndOffset <= b.StartOffset {
			// Zero-width or negative-width slice: a beat with no rune content
			// breaks the half-open [Start, End) contract.
			return false
		}
		if b.EndOffset > monoRunes {
			return false
		}
		if b.StartOffset < prevEnd {
			return false
		}
		prevEnd = b.EndOffset
	}
	return true
}

// actSeamMonotonic encodes the deterministic act-seam floor: the previous
// act's beats covered to the monologue boundary AND the current act starts
// at offset 0. Acts with no beats trivially fail (per BeatCountMin>0 floor).
// This is intentionally narrow — v2 LLM rubric still owns semantic
// continuity (topical bridge between act paragraphs).
func actSeamMonotonic(prev, curr domain.ActScript) bool {
	if len(prev.Beats) == 0 || len(curr.Beats) == 0 {
		return false
	}
	prevRunes := utf8.RuneCountInString(prev.Monologue)
	last := prev.Beats[len(prev.Beats)-1]
	first := curr.Beats[0]
	return last.EndOffset == prevRunes && first.StartOffset == 0
}

// AggregatePerAct rolls per-fixture reports into a run-level summary. The
// aggregate is what v1→v2 per-criterion comparison consumes.
func AggregatePerAct(reports []FixtureActReport) PerActAggregate {
	agg := PerActAggregate{FixtureCount: len(reports)}
	if len(reports) == 0 {
		return agg
	}
	// Sort the embedded fixture list by fixture_id so the manifest is
	// reproducible regardless of map-iteration order upstream.
	sorted := make([]FixtureActReport, len(reports))
	copy(sorted, reports)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].FixtureID < sorted[j].FixtureID
	})

	var utilizationSum float64
	var utilizationCount int
	for _, fr := range sorted {
		for i, am := range fr.Acts {
			agg.ActCount++
			if am.UnknownActID {
				agg.UnknownActIDActs++
			}
			if am.RuneCap > 0 {
				utilizationSum += am.RuneCapUtilization
				utilizationCount++
			}
			if am.RuneCapOverflow {
				agg.ActsWithRuneOverflow++
			}
			if !am.BeatCountInRange {
				agg.ActsWithBadBeatCount++
			}
			if !am.MetadataComplete {
				agg.ActsWithMetadataGap++
			}
			if !am.OffsetsValid {
				agg.ActsWithBadOffsets++
			}
			// Skip seam check for the first act of a fixture: there is no
			// previous act to bridge from. PrevSeamMonotonic for index 0 is
			// always set to true by computeFixtureActReport, but we still
			// guard here so this counter only ever flags real seam breaks.
			if i > 0 && !am.PrevSeamMonotonic {
				agg.ActsWithSeamGap++
			}
		}
	}
	if utilizationCount > 0 {
		agg.AvgRuneCapUtilization = utilizationSum / float64(utilizationCount)
	}
	agg.Fixtures = sorted
	return agg
}
