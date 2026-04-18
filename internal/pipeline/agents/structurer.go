package agents

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// V1: This agent is deterministic - no LLM call. docs/prompts/scenario/
// 02_structure.md is the reference prompt template that a V1.5 upgrade
// will wire through a domain.TextGenerator. The deterministic derivation
// here is the scaffolding and schema contract that the LLM path must honor.

func NewStructurer(validator *Validator) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if state.Research == nil {
			return fmt.Errorf("structurer: research is nil: %w", domain.ErrValidation)
		}
		if len(state.Research.DramaticBeats) < 4 {
			return fmt.Errorf("structurer: insufficient beats: %d < 4: %w", len(state.Research.DramaticBeats), domain.ErrValidation)
		}

		target := 10
		budgets := distributeSceneBudget(target)
		acts := make([]domain.Act, 0, len(domain.ActOrder))
		for idx, actID := range domain.ActOrder {
			beatIDs := assignedBeatIDs(state.Research.DramaticBeats, idx)
			keyPoints := make([]string, 0, len(beatIDs))
			for _, beatID := range beatIDs {
				keyPoints = append(keyPoints, state.Research.DramaticBeats[beatID].Description)
			}
			synopsis := "No dramatic beats assigned to this act (V1 deterministic placeholder)."
			if len(keyPoints) > 0 {
				synopsis = "Act " + strconv.Itoa(idx+1) + " opens with " + keyPoints[0] + ". (" + strconv.Itoa(len(keyPoints)) + " beats; " + strconv.Itoa(int(domain.ActDurationRatio[actID]*100)) + "% of runtime.)"
			}
			acts = append(acts, domain.Act{
				ID:              actID,
				Name:            "Act " + strconv.Itoa(idx+1) + " — " + titleCase(actID),
				Synopsis:        synopsis,
				SceneBudget:     budgets[idx],
				DurationRatio:   domain.ActDurationRatio[actID],
				DramaticBeatIDs: beatIDs,
				KeyPoints:       keyPoints,
			})
		}

		output := domain.StructurerOutput{
			SCPID:            state.Research.SCPID,
			Acts:             acts,
			TargetSceneCount: target,
			SourceVersion:    state.Research.SourceVersion,
		}
		if err := validator.Validate(output); err != nil {
			return fmt.Errorf("structurer: %w", err)
		}
		state.Structure = &output
		return nil
	}
}

func distributeSceneBudget(target int) [4]int {
	var floors [4]int
	var fracs [4]float64
	var sum int
	for i, actID := range domain.ActOrder {
		quota := float64(target) * domain.ActDurationRatio[actID]
		floors[i] = int(math.Floor(quota))
		fracs[i] = quota - float64(floors[i])
		sum += floors[i]
	}

	remaining := target - sum
	order := []int{0, 1, 2, 3}
	sort.Slice(order, func(i, j int) bool {
		left, right := order[i], order[j]
		if fracs[left] == fracs[right] {
			return left < right
		}
		return fracs[left] > fracs[right]
	})
	for i := 0; i < remaining; i++ {
		floors[order[i]]++
	}

	for i := range floors {
		if floors[i] >= 1 {
			continue
		}
		donor := maxAllocationIndex(floors)
		floors[donor]--
		floors[i]++
	}
	return floors
}

func maxAllocationIndex(values [4]int) int {
	best := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[best] || (values[i] == values[best] && i > best) {
			best = i
		}
	}
	return best
}

func assignedBeatIDs(beats []domain.DramaticBeat, actIndex int) []int {
	ids := make([]int, 0, len(beats)/4+1)
	for _, beat := range beats {
		if beat.Index%4 == actIndex {
			ids = append(ids, beat.Index)
		}
	}
	sort.Ints(ids)
	return ids
}

func titleCase(id string) string {
	if id == "" {
		return ""
	}
	return strings.ToUpper(id[:1]) + id[1:]
}
