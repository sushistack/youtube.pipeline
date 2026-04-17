package pipeline

import (
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// NextStage returns the next stage for a given (current, event) pair.
// This is a pure function: no DB access, no side effects.
// The caller is responsible for persisting the transition.
func NextStage(current domain.Stage, event domain.Event) (domain.Stage, error) {
	switch current {
	case domain.StagePending:
		switch event {
		case domain.EventStart:
			return domain.StageResearch, nil
		}
	case domain.StageResearch:
		switch event {
		case domain.EventComplete:
			return domain.StageStructure, nil
		}
	case domain.StageStructure:
		switch event {
		case domain.EventComplete:
			return domain.StageWrite, nil
		}
	case domain.StageWrite:
		switch event {
		case domain.EventComplete:
			return domain.StageVisualBreak, nil
		}
	case domain.StageVisualBreak:
		switch event {
		case domain.EventComplete:
			return domain.StageReview, nil
		}
	case domain.StageReview:
		switch event {
		case domain.EventComplete:
			return domain.StageCritic, nil
		}
	case domain.StageCritic:
		switch event {
		case domain.EventComplete:
			return domain.StageScenarioReview, nil
		case domain.EventRetry:
			return domain.StageWrite, nil
		}
	case domain.StageScenarioReview:
		switch event {
		case domain.EventApprove:
			return domain.StageCharacterPick, nil
		}
	case domain.StageCharacterPick:
		switch event {
		case domain.EventApprove:
			return domain.StageImage, nil
		}
	case domain.StageImage:
		switch event {
		case domain.EventComplete:
			return domain.StageTTS, nil
		}
	case domain.StageTTS:
		switch event {
		case domain.EventComplete:
			return domain.StageBatchReview, nil
		}
	case domain.StageBatchReview:
		switch event {
		case domain.EventApprove:
			return domain.StageAssemble, nil
		}
	case domain.StageAssemble:
		switch event {
		case domain.EventComplete:
			return domain.StageMetadataAck, nil
		}
	case domain.StageMetadataAck:
		switch event {
		case domain.EventApprove:
			return domain.StageComplete, nil
		}
	case domain.StageComplete:
		// Terminal state — no valid transitions.
	}
	return "", fmt.Errorf("invalid transition: stage=%s event=%s", current, event)
}

// IsHITLStage returns true if the stage is a human-in-the-loop wait point.
func IsHITLStage(s domain.Stage) bool {
	switch s {
	case domain.StageScenarioReview, domain.StageCharacterPick,
		domain.StageBatchReview, domain.StageMetadataAck:
		return true
	}
	return false
}

// StatusForStage returns the operational status for a given stage.
// HITL stages → StatusWaiting, automated stages → StatusRunning,
// pending → StatusPending, complete → StatusCompleted.
func StatusForStage(stage domain.Stage) domain.Status {
	switch stage {
	case domain.StagePending:
		return domain.StatusPending
	case domain.StageComplete:
		return domain.StatusCompleted
	}
	if IsHITLStage(stage) {
		return domain.StatusWaiting
	}
	return domain.StatusRunning
}
