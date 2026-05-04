package eval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// makePerActFixture builds a fixture with a v2 NarrationScript and a per-act
// override map. monoRunes specifies the rune count of each act's monologue.
func makePerActFixture(t *testing.T, fixtureID string, perActBeats map[string]int, perActMonoRunes map[string]int, withMood bool, withKeyPoints bool) Fixture {
	t.Helper()
	actIDs := []string{
		domain.ActIncident,
		domain.ActMystery,
		domain.ActRevelation,
		domain.ActUnresolved,
	}
	acts := make([]domain.ActScript, len(actIDs))
	for i, id := range actIDs {
		runes := perActMonoRunes[id]
		monologue := strings.Repeat("가", runes)
		nBeats := perActBeats[id]
		beats := make([]domain.BeatAnchor, nBeats)
		// Distribute offsets evenly to fill the monologue.
		if nBeats > 0 {
			step := runes / nBeats
			for b := 0; b < nBeats; b++ {
				start := b * step
				end := start + step
				if b == nBeats-1 {
					end = runes
				}
				beats[b] = domain.BeatAnchor{
					StartOffset:       start,
					EndOffset:         end,
					Mood:              "calm",
					Location:          "loc",
					CharactersPresent: []string{"researcher"},
					EntityVisible:     false,
					ColorPalette:      "gray",
					Atmosphere:        "still",
				}
			}
		}
		mood := ""
		if withMood {
			mood = "calm"
		}
		var keyPoints []string
		if withKeyPoints {
			keyPoints = []string{"observation"}
		}
		acts[i] = domain.ActScript{
			ActID:     id,
			Monologue: monologue,
			Beats:     beats,
			Mood:      mood,
			KeyPoints: keyPoints,
		}
	}
	script := domain.NarrationScript{
		SCPID:         "SCP-TEST",
		Title:         "test",
		Acts:          acts,
		SourceVersion: domain.NarrationSourceVersionV2,
		Metadata: domain.NarrationMetadata{
			Language:        domain.LanguageKorean,
			SceneCount:      32,
			WriterModel:     "qwen-max",
			WriterProvider:  "dashscope",
			PromptTemplate:  "v2",
		},
	}
	raw, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("marshal script: %v", err)
	}
	return Fixture{
		FixtureID:       fixtureID,
		Kind:            "positive",
		Checkpoint:      "post_writer",
		Input:           raw,
		ExpectedVerdict: "pass",
		Category:        "synthetic",
	}
}

func TestComputeFixtureActReport_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := makePerActFixture(t, "happy",
		map[string]int{
			domain.ActIncident:   8,
			domain.ActMystery:    9,
			domain.ActRevelation: 10,
			domain.ActUnresolved: 8,
		},
		map[string]int{
			domain.ActIncident:   400,
			domain.ActMystery:    1500,
			domain.ActRevelation: 2000,
			domain.ActUnresolved: 1000,
		},
		true, true,
	)

	report, err := computeFixtureActReport(f)
	if err != nil {
		t.Fatalf("computeFixtureActReport: %v", err)
	}
	testutil.AssertEqual(t, 4, len(report.Acts))
	for _, am := range report.Acts {
		if !am.BeatCountInRange {
			t.Errorf("act %s: BeatCountInRange=false (count=%d)", am.ActID, am.BeatCount)
		}
		if !am.MetadataComplete {
			t.Errorf("act %s: MetadataComplete=false", am.ActID)
		}
		if !am.OffsetsValid {
			t.Errorf("act %s: OffsetsValid=false", am.ActID)
		}
		if am.RuneCapOverflow {
			t.Errorf("act %s: unexpected RuneCapOverflow", am.ActID)
		}
		if am.RuneCapUtilization <= 0 || am.RuneCapUtilization > 1.0 {
			t.Errorf("act %s: utilization out of range: %v", am.ActID, am.RuneCapUtilization)
		}
	}
	// All acts seam-connected because beats fill the monologue.
	for i, am := range report.Acts {
		if i == 0 {
			continue // first act has no previous seam
		}
		if !am.PrevSeamMonotonic {
			t.Errorf("act %s: PrevSeamMonotonic=false unexpectedly", am.ActID)
		}
	}
	expectedTotal := 400 + 1500 + 2000 + 1000
	testutil.AssertEqual(t, expectedTotal, report.TotalRunes)
}

func TestComputeFixtureActReport_BeatCountOutOfRange(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := makePerActFixture(t, "bad-beats",
		map[string]int{
			domain.ActIncident:   3, // below floor
			domain.ActMystery:    9,
			domain.ActRevelation: 12, // above ceiling
			domain.ActUnresolved: 8,
		},
		map[string]int{
			domain.ActIncident:   400,
			domain.ActMystery:    1500,
			domain.ActRevelation: 2000,
			domain.ActUnresolved: 1000,
		},
		true, true,
	)
	report, err := computeFixtureActReport(f)
	if err != nil {
		t.Fatalf("computeFixtureActReport: %v", err)
	}
	if report.Acts[0].BeatCountInRange {
		t.Error("incident: expected BeatCountInRange=false (3 beats)")
	}
	if !report.Acts[1].BeatCountInRange {
		t.Error("mystery: expected BeatCountInRange=true (9 beats)")
	}
	if report.Acts[2].BeatCountInRange {
		t.Error("revelation: expected BeatCountInRange=false (12 beats)")
	}
}

func TestComputeFixtureActReport_RuneCapOverflow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := makePerActFixture(t, "overflow",
		map[string]int{
			domain.ActIncident:   8,
			domain.ActMystery:    9,
			domain.ActRevelation: 10,
			domain.ActUnresolved: 8,
		},
		map[string]int{
			domain.ActIncident:   600, // exceeds cap 480
			domain.ActMystery:    1500,
			domain.ActRevelation: 2000,
			domain.ActUnresolved: 1000,
		},
		true, true,
	)
	report, err := computeFixtureActReport(f)
	if err != nil {
		t.Fatalf("computeFixtureActReport: %v", err)
	}
	if !report.Acts[0].RuneCapOverflow {
		t.Errorf("incident: expected RuneCapOverflow=true (600 > 480)")
	}
	if report.Acts[1].RuneCapOverflow {
		t.Error("mystery: expected RuneCapOverflow=false")
	}
	if report.Acts[0].RuneCapUtilization <= 1.0 {
		t.Errorf("incident: expected utilization > 1.0, got %v", report.Acts[0].RuneCapUtilization)
	}
}

func TestComputeFixtureActReport_MetadataGap(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := makePerActFixture(t, "metadata-gap",
		map[string]int{
			domain.ActIncident:   8,
			domain.ActMystery:    9,
			domain.ActRevelation: 10,
			domain.ActUnresolved: 8,
		},
		map[string]int{
			domain.ActIncident:   400,
			domain.ActMystery:    1500,
			domain.ActRevelation: 2000,
			domain.ActUnresolved: 1000,
		},
		false, false, // no mood, no key_points
	)
	report, err := computeFixtureActReport(f)
	if err != nil {
		t.Fatalf("computeFixtureActReport: %v", err)
	}
	for _, am := range report.Acts {
		if am.MetadataComplete {
			t.Errorf("act %s: expected MetadataComplete=false", am.ActID)
		}
		if am.MoodPresent {
			t.Errorf("act %s: expected MoodPresent=false", am.ActID)
		}
		if am.KeyPointsPresent {
			t.Errorf("act %s: expected KeyPointsPresent=false", am.ActID)
		}
	}
}

func TestComputeFixtureActReport_BadOffsets(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// Build a fixture and corrupt its first act's offsets.
	f := makePerActFixture(t, "bad-offsets",
		map[string]int{
			domain.ActIncident:   8,
			domain.ActMystery:    9,
			domain.ActRevelation: 10,
			domain.ActUnresolved: 8,
		},
		map[string]int{
			domain.ActIncident:   400,
			domain.ActMystery:    1500,
			domain.ActRevelation: 2000,
			domain.ActUnresolved: 1000,
		},
		true, true,
	)
	var script domain.NarrationScript
	if err := json.Unmarshal(f.Input, &script); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Make beat[1] overlap with beat[0].
	script.Acts[0].Beats[1].StartOffset = script.Acts[0].Beats[0].StartOffset
	corrupted, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	f.Input = corrupted

	report, err := computeFixtureActReport(f)
	if err != nil {
		t.Fatalf("computeFixtureActReport: %v", err)
	}
	if report.Acts[0].OffsetsValid {
		t.Error("expected OffsetsValid=false on overlapping beat offsets")
	}
}

func TestComputeFixtureActReport_RejectsMalformedInput(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := Fixture{
		FixtureID: "broken",
		Input:     []byte("not valid json"),
	}
	_, err := computeFixtureActReport(f)
	if err == nil {
		t.Fatal("expected error on malformed input")
	}
}

func TestAggregatePerAct_RollsUpCounts(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	good := makePerActFixture(t, "good",
		map[string]int{
			domain.ActIncident:   8,
			domain.ActMystery:    9,
			domain.ActRevelation: 10,
			domain.ActUnresolved: 8,
		},
		map[string]int{
			domain.ActIncident:   400,
			domain.ActMystery:    1500,
			domain.ActRevelation: 2000,
			domain.ActUnresolved: 1000,
		},
		true, true,
	)
	bad := makePerActFixture(t, "bad",
		map[string]int{
			domain.ActIncident:   3, // bad beat count
			domain.ActMystery:    9,
			domain.ActRevelation: 10,
			domain.ActUnresolved: 8,
		},
		map[string]int{
			domain.ActIncident:   600, // overflow
			domain.ActMystery:    1500,
			domain.ActRevelation: 2000,
			domain.ActUnresolved: 1000,
		},
		false, true, // no mood
	)

	gr, err := computeFixtureActReport(good)
	if err != nil {
		t.Fatalf("good: %v", err)
	}
	br, err := computeFixtureActReport(bad)
	if err != nil {
		t.Fatalf("bad: %v", err)
	}

	agg := AggregatePerAct([]FixtureActReport{gr, br})
	testutil.AssertEqual(t, 2, agg.FixtureCount)
	testutil.AssertEqual(t, 8, agg.ActCount)
	testutil.AssertEqual(t, 1, agg.ActsWithRuneOverflow)
	testutil.AssertEqual(t, 1, agg.ActsWithBadBeatCount)
	// Bad fixture: all 4 acts have empty mood -> 4 metadata gaps.
	testutil.AssertEqual(t, 4, agg.ActsWithMetadataGap)
	if agg.AvgRuneCapUtilization <= 0 {
		t.Error("expected positive avg utilization")
	}
	// Sorted-by-fixture-id: "bad" comes before "good".
	if len(agg.Fixtures) != 2 {
		t.Fatalf("expected 2 fixtures in aggregate, got %d", len(agg.Fixtures))
	}
	if agg.Fixtures[0].FixtureID != "bad" {
		t.Errorf("expected sorted order [bad, good], first=%q", agg.Fixtures[0].FixtureID)
	}
}

func TestAggregatePerAct_EmptyInput(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	agg := AggregatePerAct(nil)
	testutil.AssertEqual(t, 0, agg.FixtureCount)
	testutil.AssertEqual(t, 0, agg.ActCount)
	testutil.AssertEqual(t, 0.0, agg.AvgRuneCapUtilization)
}
