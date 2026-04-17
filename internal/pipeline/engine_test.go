package pipeline

import (
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestNextStage_ValidTransitions(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		name    string
		current domain.Stage
		event   domain.Event
		want    domain.Stage
	}{
		{"pending+start→research", domain.StagePending, domain.EventStart, domain.StageResearch},
		{"research+complete→structure", domain.StageResearch, domain.EventComplete, domain.StageStructure},
		{"structure+complete→write", domain.StageStructure, domain.EventComplete, domain.StageWrite},
		{"write+complete→visual_break", domain.StageWrite, domain.EventComplete, domain.StageVisualBreak},
		{"visual_break+complete→review", domain.StageVisualBreak, domain.EventComplete, domain.StageReview},
		{"review+complete→critic", domain.StageReview, domain.EventComplete, domain.StageCritic},
		{"critic+complete→scenario_review", domain.StageCritic, domain.EventComplete, domain.StageScenarioReview},
		{"critic+retry→write", domain.StageCritic, domain.EventRetry, domain.StageWrite},
		{"scenario_review+approve→character_pick", domain.StageScenarioReview, domain.EventApprove, domain.StageCharacterPick},
		{"character_pick+approve→image", domain.StageCharacterPick, domain.EventApprove, domain.StageImage},
		{"image+complete→tts", domain.StageImage, domain.EventComplete, domain.StageTTS},
		{"tts+complete→batch_review", domain.StageTTS, domain.EventComplete, domain.StageBatchReview},
		{"batch_review+approve→assemble", domain.StageBatchReview, domain.EventApprove, domain.StageAssemble},
		{"assemble+complete→metadata_ack", domain.StageAssemble, domain.EventComplete, domain.StageMetadataAck},
		{"metadata_ack+approve→complete", domain.StageMetadataAck, domain.EventApprove, domain.StageComplete},
	}

	if len(tests) != 15 {
		t.Fatalf("expected 15 valid transitions, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextStage(tt.current, tt.event)
			if err != nil {
				t.Fatalf("NextStage(%s, %s) returned unexpected error: %v", tt.current, tt.event, err)
			}
			testutil.AssertEqual(t, got, tt.want)
		})
	}
}

func TestNextStage_InvalidTransitions(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Build the set of valid (stage, event) pairs.
	type pair struct {
		stage domain.Stage
		event domain.Event
	}
	valid := map[pair]bool{
		{domain.StagePending, domain.EventStart}:             true,
		{domain.StageResearch, domain.EventComplete}:         true,
		{domain.StageStructure, domain.EventComplete}:        true,
		{domain.StageWrite, domain.EventComplete}:            true,
		{domain.StageVisualBreak, domain.EventComplete}:      true,
		{domain.StageReview, domain.EventComplete}:           true,
		{domain.StageCritic, domain.EventComplete}:           true,
		{domain.StageCritic, domain.EventRetry}:              true,
		{domain.StageScenarioReview, domain.EventApprove}:    true,
		{domain.StageCharacterPick, domain.EventApprove}:     true,
		{domain.StageImage, domain.EventComplete}:            true,
		{domain.StageTTS, domain.EventComplete}:              true,
		{domain.StageBatchReview, domain.EventApprove}:       true,
		{domain.StageAssemble, domain.EventComplete}:         true,
		{domain.StageMetadataAck, domain.EventApprove}:       true,
	}

	invalidCount := 0
	for _, stage := range domain.AllStages() {
		for _, event := range domain.AllEvents() {
			if valid[pair{stage, event}] {
				continue
			}
			invalidCount++
			t.Run(string(stage)+"+"+string(event), func(t *testing.T) {
				_, err := NextStage(stage, event)
				if err == nil {
					t.Errorf("NextStage(%s, %s) expected error, got nil", stage, event)
				}
			})
		}
	}

	// 15 stages × 4 events = 60 total, minus 15 valid = 45 invalid.
	testutil.AssertEqual(t, invalidCount, 45)
}

func TestStatusForStage_AllStages(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		stage domain.Stage
		want  domain.Status
	}{
		{domain.StagePending, domain.StatusPending},
		{domain.StageResearch, domain.StatusRunning},
		{domain.StageStructure, domain.StatusRunning},
		{domain.StageWrite, domain.StatusRunning},
		{domain.StageVisualBreak, domain.StatusRunning},
		{domain.StageReview, domain.StatusRunning},
		{domain.StageCritic, domain.StatusRunning},
		{domain.StageScenarioReview, domain.StatusWaiting},
		{domain.StageCharacterPick, domain.StatusWaiting},
		{domain.StageImage, domain.StatusRunning},
		{domain.StageTTS, domain.StatusRunning},
		{domain.StageBatchReview, domain.StatusWaiting},
		{domain.StageAssemble, domain.StatusRunning},
		{domain.StageMetadataAck, domain.StatusWaiting},
		{domain.StageComplete, domain.StatusCompleted},
	}

	if len(tests) != 15 {
		t.Fatalf("expected 15 status mappings, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			testutil.AssertEqual(t, StatusForStage(tt.stage), tt.want)
		})
	}
}

func TestIsHITLStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	hitl := map[domain.Stage]bool{
		domain.StageScenarioReview: true,
		domain.StageCharacterPick:  true,
		domain.StageBatchReview:    true,
		domain.StageMetadataAck:    true,
	}

	for _, s := range domain.AllStages() {
		t.Run(string(s), func(t *testing.T) {
			testutil.AssertEqual(t, IsHITLStage(s), hitl[s])
		})
	}
}
