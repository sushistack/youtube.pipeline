package agents

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// V1.2: Beats are placed into acts by their RoleSuggestion (set by the
// researcher's role-classifier LLM call). Each act's scene budget is
// `len(beats_in_act) * domain.ActScenesPerBeat[actID]` — no global
// constant, no ratio-based distribution. The classifier guarantees
// every role appears at least once, so every act receives ≥1 beat.

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

		beatsByAct, err := groupBeatsByAct(state.Research.DramaticBeats)
		if err != nil {
			return fmt.Errorf("structurer: %w", err)
		}

		acts := make([]domain.Act, 0, len(domain.ActOrder))
		totalScenes := 0
		for idx, actID := range domain.ActOrder {
			beatIDs := beatsByAct[actID]
			role := domain.RoleForAct[actID]
			label := domain.RoleKoreanLabel[role]
			multiplier := domain.ActScenesPerBeat[actID]
			budget := len(beatIDs) * multiplier
			totalScenes += budget

			keyPoints := make([]string, 0, len(beatIDs))
			for _, beatID := range beatIDs {
				keyPoints = append(keyPoints, fmt.Sprintf("[ROLE: %s] %s", label, state.Research.DramaticBeats[beatID].Description))
			}
			synopsis := fmt.Sprintf(
				"[ROLE: %s] Act %d opens with %s. (%d beats × %d scenes/beat = %d scenes; %d%% of runtime.)",
				label,
				idx+1,
				state.Research.DramaticBeats[beatIDs[0]].Description,
				len(beatIDs),
				multiplier,
				budget,
				int(domain.ActDurationRatio[actID]*100),
			)

			acts = append(acts, domain.Act{
				ID:              actID,
				Name:            "Act " + strconv.Itoa(idx+1) + " — " + titleCase(actID),
				Synopsis:        synopsis,
				SceneBudget:     budget,
				DurationRatio:   domain.ActDurationRatio[actID],
				DramaticBeatIDs: beatIDs,
				KeyPoints:       keyPoints,
				Role:            role,
			})
		}

		output := domain.StructurerOutput{
			SCPID:            state.Research.SCPID,
			Acts:             acts,
			TargetSceneCount: totalScenes,
			SourceVersion:    state.Research.SourceVersion,
		}
		if err := validator.Validate(output); err != nil {
			return fmt.Errorf("structurer: %w", err)
		}
		state.Structure = &output
		return nil
	}
}

// groupBeatsByAct partitions beats into the 4-act buckets keyed by act ID.
// Returns ErrValidation if any beat has an empty or unknown RoleSuggestion
// (researcher contract violation — the classifier should have rejected
// these or the run should have already failed) or if any role is missing
// (which the classifier validator also rejects, so reaching here means
// something upstream lied).
func groupBeatsByAct(beats []domain.DramaticBeat) (map[string][]int, error) {
	byAct := make(map[string][]int, len(domain.ActOrder))
	for _, actID := range domain.ActOrder {
		byAct[actID] = []int{}
	}
	for _, beat := range beats {
		if beat.RoleSuggestion == "" {
			return nil, fmt.Errorf("beat %d: empty role_suggestion: %w", beat.Index, domain.ErrValidation)
		}
		actID, ok := domain.ActForRole[beat.RoleSuggestion]
		if !ok {
			return nil, fmt.Errorf("beat %d: unknown role %q: %w", beat.Index, beat.RoleSuggestion, domain.ErrValidation)
		}
		byAct[actID] = append(byAct[actID], beat.Index)
	}
	for _, actID := range domain.ActOrder {
		if len(byAct[actID]) == 0 {
			return nil, fmt.Errorf("act %s has no beats: %w", actID, domain.ErrValidation)
		}
		sort.Ints(byAct[actID])
	}
	return byAct, nil
}

func titleCase(id string) string {
	if id == "" {
		return ""
	}
	return strings.ToUpper(id[:1]) + id[1:]
}
