package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type visualBreakdownActResponse struct {
	ActID string                          `json:"act_id"`
	Shots []visualBreakdownActResponseShot `json:"shots"`
}

type visualBreakdownActResponseShot struct {
	VisualDescriptor string             `json:"visual_descriptor"`
	Transition       string             `json:"transition"`
	NarrationAnchor  domain.BeatAnchor  `json:"narration_anchor"`
}

// visualBreakdownerPerActRetryBudget mirrors v1's per-scene retry budget value
// (cycle-C policy, commit `2ef1d3c`): one retry per act on json/schema/anchor
// validation failure. Transport errors short-circuit before consuming budget.
const visualBreakdownerPerActRetryBudget = 1

// visualBreakdownerActConcurrency caps per-act fan-out at 4 — one goroutine
// per act, per spec D2 Design Notes ("Per-act vs per-scene fan-out"). v1's
// ~32 per-scene goroutines drop to 4 acts (rate-friendlier for DashScope).
const visualBreakdownerActConcurrency = 4

func NewVisualBreakdowner(
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	validator *Validator,
	estimator SceneDurationEstimator,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state == nil:
			return fmt.Errorf("visual breakdowner: %w: state is nil", domain.ErrValidation)
		case state.Research == nil:
			return fmt.Errorf("visual breakdowner: %w: research is nil", domain.ErrValidation)
		case state.Narration == nil:
			return fmt.Errorf("visual breakdowner: %w: narration is nil", domain.ErrValidation)
		case cfg.Model == "":
			return fmt.Errorf("visual breakdowner: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("visual breakdowner: %w: provider is empty", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("visual breakdowner: %w: generator is nil", domain.ErrValidation)
		case validator == nil:
			return fmt.Errorf("visual breakdowner: %w: validator is nil", domain.ErrValidation)
		case estimator == nil:
			return fmt.Errorf("visual breakdowner: %w: estimator is nil", domain.ErrValidation)
		}
		if len(state.Narration.Acts) == 0 {
			return fmt.Errorf("visual breakdowner: %w: narration has no acts", domain.ErrValidation)
		}
		// Reject acts with zero beats — anchor-equality validator cannot
		// produce zero-shot acts (1:1 beat→shot invariant per spec).
		for i, act := range state.Narration.Acts {
			if len(act.Beats) == 0 {
				return fmt.Errorf("visual breakdowner: %w: act %d (%s) has no beats", domain.ErrValidation, i, act.ActID)
			}
		}
		// Reject duplicate ActIDs — the v1-shape bridge relies on ActID
		// lookup into narration; a duplicate would silently shadow.
		seen := make(map[string]struct{}, len(state.Narration.Acts))
		for _, act := range state.Narration.Acts {
			if _, dup := seen[act.ActID]; dup {
				return fmt.Errorf("visual breakdowner: %w: duplicate act_id=%q", domain.ErrValidation, act.ActID)
			}
			seen[act.ActID] = struct{}{}
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		frozen := BuildFrozenDescriptor(state.Research.VisualIdentity)
		output := domain.VisualScript{
			SCPID:            state.Narration.SCPID,
			Title:            state.Narration.Title,
			FrozenDescriptor: frozen,
			Acts:             make([]domain.VisualAct, 0, len(state.Narration.Acts)),
			ShotOverrides:    map[int]domain.ShotOverride{},
			Metadata: domain.VisualBreakdownMetadata{
				VisualBreakdownModel:    cfg.Model,
				VisualBreakdownProvider: cfg.Provider,
				PromptTemplate:          filepath.Base(visualBreakdownPromptPath),
				ShotFormulaVersion:      domain.ShotFormulaVersionV1,
			},
			SourceVersion: domain.VisualBreakdownSourceVersionV2,
		}

		acts := state.Narration.Acts
		results := make([]domain.VisualAct, len(acts))

		concurrency := cfg.Concurrency
		if concurrency <= 0 {
			concurrency = visualBreakdownerActConcurrency
		}
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(concurrency)
		for i, act := range acts {
			i, act := i, act
			g.Go(func() error {
				decoded, err := runVisualBreakdownerAct(gctx, gen, cfg, prompts, state, act, frozen)
				if err != nil {
					return err
				}
				results[i] = buildVisualAct(act, frozen, decoded, estimator, cfg.Logger)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		output.Acts = results

		if err := validator.Validate(output); err != nil {
			return fmt.Errorf("visual breakdowner: %w", err)
		}
		state.VisualScript = &output
		return nil
	}
}

func runVisualBreakdownerAct(
	ctx context.Context,
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	state *PipelineState,
	act domain.ActScript,
	frozen string,
) (visualBreakdownActResponse, error) {
	shotCount := len(act.Beats)
	if shotCount < 1 {
		return visualBreakdownActResponse{}, fmt.Errorf(
			"visual breakdowner: act %s has no beats (min 1 required): %w",
			act.ActID, domain.ErrValidation,
		)
	}
	prompt, err := renderVisualBreakdownActPrompt(state, prompts, act, frozen, shotCount)
	if err != nil {
		return visualBreakdownActResponse{}, err
	}

	opts := retryOpts{
		Stage:  "visual_breakdowner",
		Budget: visualBreakdownerPerActRetryBudget,
		Logger: cfg.Logger,
		BaseAttrs: []slog.Attr{
			slog.String("run_id", state.RunID),
			slog.String("act_id", act.ActID),
		},
	}

	decoded, err := runWithRetry(ctx, opts, func(attempt int) (visualBreakdownActResponse, retryReason, error) {
		if cfg.Logger != nil {
			cfg.Logger.Info("visual breakdowner attempt start",
				"run_id", state.RunID,
				"act_id", act.ActID,
				"attempt", attempt,
				"provider", cfg.Provider,
				"model", cfg.Model,
			)
		}
		callStart := time.Now()
		resp, err := gen.Generate(ctx, domain.TextRequest{
			Prompt:      prompt,
			Model:       cfg.Model,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		})
		if err != nil {
			if cfg.Logger != nil {
				cfg.Logger.Error("visual breakdowner attempt failed",
					"run_id", state.RunID,
					"act_id", act.ActID,
					"attempt", attempt,
					"duration_ms", time.Since(callStart).Milliseconds(),
					"error", err.Error(),
				)
			}
			// Context cancellation propagates verbatim — no retry.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return visualBreakdownActResponse{}, retryReasonAbort, err
			}
			// Transient upstream failures (provider returned empty content,
			// 5xx, gateway timeout, …) are signaled with ErrStageFailed by
			// the text clients. Make these retryable instead of aborting on
			// the first attempt — a single empty-content blip would otherwise
			// kill the whole stage despite the per-act retry budget.
			if errors.Is(err, domain.ErrStageFailed) {
				return visualBreakdownActResponse{}, "", err
			}
			return visualBreakdownActResponse{}, retryReasonAbort, err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("visual breakdowner attempt complete",
				"run_id", state.RunID,
				"act_id", act.ActID,
				"attempt", attempt,
				"duration_ms", time.Since(callStart).Milliseconds(),
			)
		}

		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "visual_breakdowner",
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}

		var decoded visualBreakdownActResponse
		if err := decodeJSONResponse(resp.Content, &decoded); err != nil {
			return visualBreakdownActResponse{}, retryReasonJSONDecode, fmt.Errorf("visual breakdowner: %w", err)
		}
		if err := validateVisualBreakdownActResponse(act, decoded); err != nil {
			return visualBreakdownActResponse{}, retryReasonSchemaValidation, err
		}
		return decoded, "", nil
	})
	if err != nil {
		return visualBreakdownActResponse{}, err
	}
	return decoded, nil
}

func renderVisualBreakdownActPrompt(
	state *PipelineState,
	prompts PromptAssets,
	act domain.ActScript,
	frozen string,
	shotCount int,
) (string, error) {
	visualJSON, err := json.MarshalIndent(state.Research.VisualIdentity, "", "  ")
	if err != nil {
		return "", fmt.Errorf("visual breakdowner: marshal visual identity: %w", domain.ErrValidation)
	}
	replacer := strings.NewReplacer(
		"{act_id}", act.ActID,
		"{act_mood}", act.Mood,
		"{monologue}", act.Monologue,
		"{beats_table}", renderBeatsTable(act.Beats),
		"{scp_visual_reference}", string(visualJSON),
		"{frozen_descriptor}", frozen,
		"{shot_count}", strconv.Itoa(shotCount),
	)
	return replacer.Replace(prompts.VisualBreakdownTemplate), nil
}

// renderBeatsTable produces a compact human-readable list of the ordered
// BeatAnchors, surfacing every load-bearing field to the LLM. The LLM is
// instructed to echo each beat's metadata byte-for-byte into its output's
// `narration_anchor` block; the post-response validator enforces equality.
func renderBeatsTable(beats []domain.BeatAnchor) string {
	if len(beats) == 0 {
		return "(none)"
	}
	lines := make([]string, 0, len(beats))
	for i, beat := range beats {
		factTags := "[]"
		if len(beat.FactTags) > 0 {
			parts := make([]string, 0, len(beat.FactTags))
			for _, ft := range beat.FactTags {
				parts = append(parts, fmt.Sprintf("%s=%s", ft.Key, ft.Content))
			}
			factTags = "[" + strings.Join(parts, ", ") + "]"
		}
		lines = append(lines, fmt.Sprintf(
			"- [beat %d] start_offset=%d end_offset=%d mood=%q location=%q characters_present=%v entity_visible=%v color_palette=%q atmosphere=%q fact_tags=%s",
			i,
			beat.StartOffset, beat.EndOffset,
			beat.Mood, beat.Location,
			beat.CharactersPresent,
			beat.EntityVisible,
			beat.ColorPalette,
			beat.Atmosphere,
			factTags,
		))
	}
	return strings.Join(lines, "\n")
}

// validateVisualBreakdownActResponse enforces the per-act anchor-equality
// invariant: shots[i].NarrationAnchor MUST deeply equal act.Beats[i] for
// every i, with len(shots) == len(act.Beats). Mismatches are retryable
// ErrValidation per cycle-C policy.
func validateVisualBreakdownActResponse(act domain.ActScript, decoded visualBreakdownActResponse) error {
	if decoded.ActID != act.ActID {
		return fmt.Errorf("visual breakdowner: act %s response act_id=%q: %w", act.ActID, decoded.ActID, domain.ErrValidation)
	}
	if len(decoded.Shots) != len(act.Beats) {
		return fmt.Errorf(
			"visual breakdowner: act %s shot count=%d want=%d (1:1 beat→shot invariant): %w",
			act.ActID, len(decoded.Shots), len(act.Beats), domain.ErrValidation,
		)
	}
	for i, shot := range decoded.Shots {
		if strings.TrimSpace(shot.VisualDescriptor) == "" {
			return fmt.Errorf("visual breakdowner: act %s shot %d empty descriptor: %w", act.ActID, i+1, domain.ErrValidation)
		}
		if !isAllowedTransition(shot.Transition) {
			return fmt.Errorf("visual breakdowner: act %s shot %d invalid transition %q: %w", act.ActID, i+1, shot.Transition, domain.ErrValidation)
		}
		if !beatAnchorEqual(shot.NarrationAnchor, act.Beats[i]) {
			return fmt.Errorf(
				"visual breakdowner: act %s shot %d narration_anchor does not match source beat anchor (byte-for-byte equality required): %w",
				act.ActID, i+1, domain.ErrValidation,
			)
		}
	}
	return nil
}

// beatAnchorEqual reports whether two BeatAnchors carry identical field
// values. nil-vs-empty slices for CharactersPresent / FactTags are treated
// as equal (LLMs may serialize empty arrays slightly differently across
// retries; the load-bearing semantic is "no entries").
func beatAnchorEqual(a, b domain.BeatAnchor) bool {
	if a.StartOffset != b.StartOffset || a.EndOffset != b.EndOffset {
		return false
	}
	if a.Mood != b.Mood || a.Location != b.Location {
		return false
	}
	if a.EntityVisible != b.EntityVisible {
		return false
	}
	if a.ColorPalette != b.ColorPalette || a.Atmosphere != b.Atmosphere {
		return false
	}
	if !stringSliceEqualNilSafe(a.CharactersPresent, b.CharactersPresent) {
		return false
	}
	if !factTagSliceEqualNilSafe(a.FactTags, b.FactTags) {
		return false
	}
	return true
}

func stringSliceEqualNilSafe(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func factTagSliceEqualNilSafe(a, b []domain.FactTag) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func buildVisualAct(
	act domain.ActScript,
	_ string,
	decoded visualBreakdownActResponse,
	estimator SceneDurationEstimator,
	_ *slog.Logger,
) domain.VisualAct {
	// Estimate per-shot duration by treating each beat as a v1 NarrationScene
	// with its rune slice as Narration text (the SceneDurationEstimator
	// interface is v1-shaped; we compute one estimate per shot rather than
	// dividing one act-level total). This matches the v1 "shot-as-tts-unit"
	// formula but per beat instead of per scene.
	runes := []rune(act.Monologue)
	runeLen := len(runes)
	shots := make([]domain.VisualShot, 0, len(decoded.Shots))
	for i, shot := range decoded.Shots {
		descriptor := strings.TrimSpace(shot.VisualDescriptor)
		anchor := act.Beats[i]
		start := anchor.StartOffset
		end := anchor.EndOffset
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
		beatText := ""
		if runeLen > 0 {
			beatText = string(runes[start:end])
		}
		dur := estimator.Estimate(domain.NarrationScene{
			SceneNum:  i + 1,
			ActID:     act.ActID,
			Narration: beatText,
		})
		shots = append(shots, domain.VisualShot{
			ShotIndex:          i + 1,
			VisualDescriptor:   descriptor,
			EstimatedDurationS: roundToTenth(dur),
			Transition:         shot.Transition,
			NarrationAnchor:    anchor,
		})
	}
	return domain.VisualAct{
		ActID: act.ActID,
		Shots: shots,
	}
}

func isAllowedTransition(value string) bool {
	switch value {
	case domain.TransitionKenBurns, domain.TransitionCrossDissolve, domain.TransitionHardCut:
		return true
	default:
		return false
	}
}

func fallbackString(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
