package pipeline

import (
	"fmt"
	"sort"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

type SceneGateInput struct {
	SceneIndex      int
	CriticScore     *float64
	RegexTriggered  bool
	CriticTriggered bool
}

type SceneGateResult struct {
	ReviewStatus   domain.ReviewStatus
	SafeguardFlags []string
	AutoApproved   bool
}

func DecideSceneGate(input SceneGateInput, threshold float64) (SceneGateResult, error) {
	if threshold <= 0 || threshold >= 1 {
		return SceneGateResult{}, fmt.Errorf("decide scene gate: invalid threshold %v: %w", threshold, domain.ErrValidation)
	}
	if input.RegexTriggered || input.CriticTriggered {
		return SceneGateResult{
			ReviewStatus:   domain.ReviewStatusWaitingForReview,
			SafeguardFlags: []string{domain.SafeguardFlagMinors},
			AutoApproved:   false,
		}, nil
	}
	if input.CriticScore != nil && *input.CriticScore > threshold {
		return SceneGateResult{
			ReviewStatus: domain.ReviewStatusAutoApproved,
			AutoApproved: true,
		}, nil
	}
	return SceneGateResult{
		ReviewStatus: domain.ReviewStatusWaitingForReview,
	}, nil
}

// MergeMinorSignals returns a scene-index keyed union of regex and critic
// signals. Input scene numbers are 1-based narration scene numbers; output
// keys are 0-based segment scene_index values.
func MergeMinorSignals(regexHits []agents.MinorRegexHit, criticFindings []domain.MinorPolicyFinding) map[int][]string {
	if len(regexHits) == 0 && len(criticFindings) == 0 {
		return map[int][]string{}
	}
	out := make(map[int][]string)
	seen := make(map[int]map[string]struct{})
	add := func(sceneNum int, value string) {
		if sceneNum <= 0 || value == "" {
			return
		}
		sceneIndex := sceneNum - 1
		if seen[sceneIndex] == nil {
			seen[sceneIndex] = make(map[string]struct{})
		}
		if _, ok := seen[sceneIndex][value]; ok {
			return
		}
		seen[sceneIndex][value] = struct{}{}
		out[sceneIndex] = append(out[sceneIndex], value)
	}
	for _, hit := range regexHits {
		add(hit.SceneNum, hit.Pattern)
	}
	for _, finding := range criticFindings {
		add(finding.SceneNum, finding.Reason)
	}
	for idx := range out {
		sort.Strings(out[idx])
	}
	return out
}
