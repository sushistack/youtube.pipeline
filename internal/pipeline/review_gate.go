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

// MergeMinorSignals returns a 0-based segments.scene_index keyed union of
// regex and critic minor-protection signals. Inputs key on (ActID, RuneOffset)
// inside NarrationScript.Acts[].Monologue (v2 monologue mode); the script
// argument is required to translate those coordinates back to a flat
// segments.scene_index via NarrationScript.BeatIndexAt.
//
// Hits whose (ActID, RuneOffset) does not resolve to a beat (act_id missing,
// or offset outside any beat's [start_offset, end_offset)) are dropped — the
// translation is best-effort and the v2 critic schema validator gates
// upstream emissions, so a failure here means the script changed under the
// hits or the LLM emitted an invalid offset that escaped validation.
func MergeMinorSignals(
	script *domain.NarrationScript,
	regexHits []agents.MinorRegexHit,
	criticFindings []domain.MinorPolicyFinding,
) map[int][]string {
	if (len(regexHits) == 0 && len(criticFindings) == 0) || script == nil {
		return map[int][]string{}
	}
	out := make(map[int][]string)
	seen := make(map[int]map[string]struct{})
	add := func(actID string, runeOffset int, value string) {
		if value == "" {
			return
		}
		beatIdx := script.BeatIndexAt(actID, runeOffset)
		if beatIdx <= 0 {
			return
		}
		sceneIndex := beatIdx - 1
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
		add(hit.ActID, hit.RuneOffset, hit.Pattern)
	}
	for _, finding := range criticFindings {
		add(finding.ActID, finding.RuneOffset, finding.Reason)
	}
	for idx := range out {
		sort.Strings(out[idx])
	}
	return out
}
