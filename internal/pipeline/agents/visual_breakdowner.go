package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type visualBreakdownResponse struct {
	SceneNum int                           `json:"scene_num"`
	Shots    []visualBreakdownResponseShot `json:"shots"`
}

type visualBreakdownResponseShot struct {
	VisualDescriptor   string `json:"visual_descriptor"`
	Transition         string `json:"transition"`
	NarrationBeatIndex int    `json:"narration_beat_index"`
}

const visualBreakdownerPerSceneRetryBudget = 1 // one retry per scene on json/schema failure

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
		if len(state.Narration.Scenes) == 0 {
			return fmt.Errorf("visual breakdowner: %w: narration has no scenes", domain.ErrValidation)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		frozen := BuildFrozenDescriptor(state.Research.VisualIdentity)
		output := domain.VisualBreakdownOutput{
			SCPID:            state.Narration.SCPID,
			Title:            state.Narration.Title,
			FrozenDescriptor: frozen,
			Scenes:           make([]domain.VisualBreakdownScene, 0, len(state.Narration.Scenes)),
			ShotOverrides:    map[int]domain.ShotOverride{},
			Metadata: domain.VisualBreakdownMetadata{
				VisualBreakdownModel:    cfg.Model,
				VisualBreakdownProvider: cfg.Provider,
				PromptTemplate:          filepath.Base(visualBreakdownPromptPath),
				ShotFormulaVersion:      domain.ShotFormulaVersionV1,
			},
			SourceVersion: domain.VisualBreakdownSourceVersionV1,
		}

		seen := make(map[int]struct{}, len(state.Narration.Scenes))
		for _, scene := range state.Narration.Scenes {
			if _, dup := seen[scene.SceneNum]; dup {
				return fmt.Errorf("visual breakdowner: %w: duplicate scene_num=%d", domain.ErrValidation, scene.SceneNum)
			}
			seen[scene.SceneNum] = struct{}{}
		}

		scenes := state.Narration.Scenes
		results := make([]domain.VisualBreakdownScene, len(scenes))

		concurrency := cfg.Concurrency
		if concurrency <= 0 {
			concurrency = defaultAgentConcurrency
		}
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(concurrency)
		for i, scene := range scenes {
			i, scene := i, scene
			g.Go(func() error {
				decoded, sceneDuration, err := runVisualBreakdownerScene(gctx, gen, cfg, prompts, state, scene, frozen, estimator)
				if err != nil {
					return err
				}
				results[i] = buildVisualBreakdownScene(scene, frozen, sceneDuration, decoded, cfg.Logger)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		output.Scenes = results

		if err := validator.Validate(output); err != nil {
			return fmt.Errorf("visual breakdowner: %w", err)
		}
		state.VisualBreakdown = &output
		return nil
	}
}

func runVisualBreakdownerScene(
	ctx context.Context,
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	state *PipelineState,
	scene domain.NarrationScene,
	frozen string,
	estimator SceneDurationEstimator,
) (visualBreakdownResponse, float64, error) {
	sceneDuration := estimator.Estimate(scene)
	shotCount := len(scene.NarrationBeats)
	if shotCount < 1 {
		return visualBreakdownResponse{}, 0, fmt.Errorf(
			"visual breakdowner: scene %d has no narration_beats (min 1 required): %w",
			scene.SceneNum, domain.ErrValidation,
		)
	}
	prompt, err := renderVisualBreakdownPrompt(state, prompts, scene, frozen, sceneDuration, shotCount)
	if err != nil {
		return visualBreakdownResponse{}, 0, err
	}

	opts := retryOpts{
		Stage:  "visual_breakdowner",
		Budget: visualBreakdownerPerSceneRetryBudget,
		Logger: cfg.Logger,
		BaseAttrs: []slog.Attr{
			slog.String("run_id", state.RunID),
			slog.Int("scene_num", scene.SceneNum),
		},
	}

	decoded, err := runWithRetry(ctx, opts, func(attempt int) (visualBreakdownResponse, retryReason, error) {
		if cfg.Logger != nil {
			cfg.Logger.Info("visual breakdowner attempt start",
				"run_id", state.RunID,
				"scene_num", scene.SceneNum,
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
					"scene_num", scene.SceneNum,
					"attempt", attempt,
					"duration_ms", time.Since(callStart).Milliseconds(),
					"error", err.Error(),
				)
			}
			return visualBreakdownResponse{}, retryReasonAbort, err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("visual breakdowner attempt complete",
				"run_id", state.RunID,
				"scene_num", scene.SceneNum,
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

		var decoded visualBreakdownResponse
		if err := decodeJSONResponse(resp.Content, &decoded); err != nil {
			return visualBreakdownResponse{}, retryReasonJSONDecode, fmt.Errorf("visual breakdowner: %w", err)
		}
		if err := validateVisualBreakdownResponse(scene.SceneNum, shotCount, decoded); err != nil {
			return visualBreakdownResponse{}, retryReasonSchemaValidation, err
		}
		return decoded, "", nil
	})
	if err != nil {
		return visualBreakdownResponse{}, 0, err
	}
	return decoded, sceneDuration, nil
}

func renderVisualBreakdownPrompt(
	state *PipelineState,
	prompts PromptAssets,
	scene domain.NarrationScene,
	frozen string,
	sceneDuration float64,
	shotCount int,
) (string, error) {
	visualJSON, err := json.MarshalIndent(state.Research.VisualIdentity, "", "  ")
	if err != nil {
		return "", fmt.Errorf("visual breakdowner: marshal visual identity: %w", domain.ErrValidation)
	}
	replacer := strings.NewReplacer(
		"{scene_num}", strconv.Itoa(scene.SceneNum),
		"{location}", scene.Location,
		"{characters_present}", strings.Join(scene.CharactersPresent, ", "),
		"{color_palette}", scene.ColorPalette,
		"{atmosphere}", scene.Atmosphere,
		"{scp_visual_reference}", string(visualJSON),
		"{narration}", scene.Narration,
		"{narration_beats}", renderNarrationBeats(scene.NarrationBeats),
		"{frozen_descriptor}", frozen,
		"{estimated_tts_duration_s}", strconv.FormatFloat(sceneDuration, 'f', 1, 64),
		"{shot_count}", strconv.Itoa(shotCount),
	)
	return replacer.Replace(prompts.VisualBreakdownTemplate), nil
}

func renderNarrationBeats(beats []string) string {
	if len(beats) == 0 {
		return "(none)"
	}
	lines := make([]string, 0, len(beats))
	for i, beat := range beats {
		lines = append(lines, fmt.Sprintf("- [beat %d] %s", i, beat))
	}
	return strings.Join(lines, "\n")
}

func validateVisualBreakdownResponse(sceneNum, shotCount int, decoded visualBreakdownResponse) error {
	if decoded.SceneNum != sceneNum {
		return fmt.Errorf("visual breakdowner: scene %d response scene_num=%d: %w", sceneNum, decoded.SceneNum, domain.ErrValidation)
	}
	if len(decoded.Shots) != shotCount {
		return fmt.Errorf("visual breakdowner: scene %d shot count=%d want=%d: %w", sceneNum, len(decoded.Shots), shotCount, domain.ErrValidation)
	}
	for i, shot := range decoded.Shots {
		if strings.TrimSpace(shot.VisualDescriptor) == "" {
			return fmt.Errorf("visual breakdowner: scene %d shot %d empty descriptor: %w", sceneNum, i+1, domain.ErrValidation)
		}
		if !isAllowedTransition(shot.Transition) {
			return fmt.Errorf("visual breakdowner: scene %d shot %d invalid transition %q: %w", sceneNum, i+1, shot.Transition, domain.ErrValidation)
		}
		if shot.NarrationBeatIndex != i {
			return fmt.Errorf(
				"visual breakdowner: scene %d shot %d narration_beat_index=%d (must equal shot order %d): %w",
				sceneNum, i+1, shot.NarrationBeatIndex, i, domain.ErrValidation,
			)
		}
	}
	return nil
}

func buildVisualBreakdownScene(
	scene domain.NarrationScene,
	frozen string,
	sceneDuration float64,
	decoded visualBreakdownResponse,
	logger *slog.Logger,
) domain.VisualBreakdownScene {
	durations := NormalizeShotDurations(sceneDuration, len(decoded.Shots))
	shots := make([]domain.VisualShot, 0, len(decoded.Shots))
	for i, shot := range decoded.Shots {
		descriptor := strings.TrimSpace(shot.VisualDescriptor)
		beatIdx := shot.NarrationBeatIndex
		var beatText string
		if beatIdx >= 0 && beatIdx < len(scene.NarrationBeats) {
			beatText = scene.NarrationBeats[beatIdx]
		}
		shots = append(shots, domain.VisualShot{
			ShotIndex:          i + 1,
			VisualDescriptor:   descriptor,
			EstimatedDurationS: durations[i],
			Transition:         shot.Transition,
			NarrationBeatIndex: beatIdx,
			NarrationBeatText:  beatText,
		})
	}
	return domain.VisualBreakdownScene{
		SceneNum:              scene.SceneNum,
		ActID:                 scene.ActID,
		Narration:             scene.Narration,
		EstimatedTTSDurationS: roundToTenth(sceneDuration),
		ShotCount:             len(shots),
		Shots:                 shots,
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
