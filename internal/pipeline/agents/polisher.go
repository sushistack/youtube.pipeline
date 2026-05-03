package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

const polisherRetryBudget = 1

// NewPolisher constructs the whole-scenario smooth-pass agent (Lever C).
//
// Two error regimes coexist here, by design:
//
//  1. **Wiring-bug fail-fast**: nil dependencies (state, gen, validator,
//     terms) or empty model/provider return ErrValidation and abort the
//     run. These are call-site bugs — silently falling back would degrade
//     every run forever with no signal.
//  2. **Runtime fallback-not-fail**: LLM error after retry, schema
//     violation, edit-budget overage, read-only field mutation, or
//     newly-introduced forbidden terms all trigger fallback (writer's
//     output is preserved, polisher_failed audit emitted, nil returned).
//
// Context cancellation is in neither regime: ctx.Canceled / DeadlineExceeded
// propagate directly to the runner. The caller asked the run to stop —
// continuing to the next stage would silently ignore that.
//
// Audit ordering: AuditEventTextGeneration is emitted ONLY after every
// post-LLM check passes and state.Narration has been replaced. This is the
// "only observable difference between fallback and happy run is the audit
// event type" guarantee — emitting inside the retry callback would pair
// text_generation with polisher_failed on any post-LLM failure.
func NewPolisher(
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	validator *Validator,
	terms *ForbiddenTerms,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state == nil:
			return fmt.Errorf("polisher: %w: state is nil", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("polisher: %w: generator is nil", domain.ErrValidation)
		case validator == nil:
			return fmt.Errorf("polisher: %w: validator is nil", domain.ErrValidation)
		case terms == nil:
			return fmt.Errorf("polisher: %w: forbidden terms are nil", domain.ErrValidation)
		case cfg.Model == "":
			return fmt.Errorf("polisher: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("polisher: %w: provider is empty", domain.ErrValidation)
		}

		if state.Narration == nil {
			// Writer has not run or was skipped — nothing to polish.
			return nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		// Snapshot original scenes for edit-budget and read-only invariance
		// checks, and original forbidden-term hits for delta-only rejection.
		// The writer's pre-existing matches are the writer's responsibility;
		// only NEW hits introduced by the polisher should trigger fallback.
		originalScenes := make([]domain.NarrationScene, len(state.Narration.Scenes))
		copy(originalScenes, state.Narration.Scenes)
		originalHitSet := forbiddenHitKeySet(terms.MatchNarration(state.Narration))

		prompt, err := renderPolisherPrompt(state, prompts)
		if err != nil {
			return err
		}

		type polisherResult struct {
			script domain.NarrationScript
			resp   domain.TextResponse
		}

		opts := retryOpts{
			Stage:  "polisher",
			Budget: polisherRetryBudget,
			Logger: cfg.Logger,
			BaseAttrs: []slog.Attr{
				slog.String("run_id", state.RunID),
			},
		}

		out, llmErr := runWithRetry(ctx, opts, func(attempt int) (polisherResult, retryReason, error) {
			if cfg.Logger != nil {
				cfg.Logger.Info("polisher attempt start",
					"run_id", state.RunID,
					"attempt", attempt,
					"provider", cfg.Provider,
					"model", cfg.Model,
					"prompt_chars", utf8.RuneCountInString(prompt),
				)
			}
			callStart := time.Now()
			resp, genErr := gen.Generate(ctx, domain.TextRequest{
				Prompt:      prompt,
				Model:       cfg.Model,
				MaxTokens:   cfg.MaxTokens,
				Temperature: cfg.Temperature,
			})
			if genErr != nil {
				if cfg.Logger != nil {
					cfg.Logger.Error("polisher attempt failed",
						"run_id", state.RunID,
						"attempt", attempt,
						"duration_ms", time.Since(callStart).Milliseconds(),
						"error", genErr.Error(),
					)
				}
				// Context cancellation: propagate without retry. Re-running the
				// same call against a cancelled context would just fail again.
				if errors.Is(genErr, context.Canceled) || errors.Is(genErr, context.DeadlineExceeded) {
					return polisherResult{}, retryReasonAbort, genErr
				}
				// Transient transport error: retryable. The polisher's mandate
				// is quality lift; a single TCP reset must not permanently
				// downgrade the run. Empty reason → runWithRetry loops.
				return polisherResult{}, "", genErr
			}
			if cfg.Logger != nil {
				cfg.Logger.Info("polisher attempt complete",
					"run_id", state.RunID,
					"attempt", attempt,
					"duration_ms", time.Since(callStart).Milliseconds(),
					"finish_reason", resp.FinishReason,
					"tokens_in", resp.TokensIn,
					"tokens_out", resp.TokensOut,
				)
			}

			// Truncated completion: re-running the same prompt against the
			// same MaxTokens would truncate again. Abort retry; the post-loop
			// fallback path will preserve writer output and surface the
			// "raise max_tokens" hint via warn-log.
			if isTruncatedFinishReason(resp.FinishReason) {
				return polisherResult{}, retryReasonAbort, fmt.Errorf(
					"polisher: provider truncated completion (finish_reason=%q); raise max_tokens: %w",
					resp.FinishReason, domain.ErrValidation,
				)
			}

			var polished domain.NarrationScript
			if decErr := decodeJSONResponse(resp.Content, &polished); decErr != nil {
				return polisherResult{}, retryReasonJSONDecode, fmt.Errorf("polisher: %w", decErr)
			}
			return polisherResult{script: polished, resp: resp}, "", nil
		})

		if llmErr != nil {
			// Context cancellation surfaces here either from runWithRetry's
			// pre-iteration ctx check or from the generator. Either way, the
			// runner asked us to stop — propagate, do not fall back.
			if errors.Is(llmErr, context.Canceled) || errors.Is(llmErr, context.DeadlineExceeded) {
				return llmErr
			}
			polisherFallback(ctx, cfg, state, llmErr)
			return nil
		}

		polished := out.script

		if valErr := validator.Validate(polished); valErr != nil {
			polisherFallback(ctx, cfg, state, valErr)
			return nil
		}

		if budgetErr := checkPolisherEditBudget(originalScenes, polished.Scenes); budgetErr != nil {
			polisherFallback(ctx, cfg, state, budgetErr)
			return nil
		}

		if invErr := checkPolisherReadOnlyInvariance(originalScenes, polished.Scenes); invErr != nil {
			polisherFallback(ctx, cfg, state, invErr)
			return nil
		}

		newHits := filterNewForbiddenHits(terms.MatchNarration(&polished), originalHitSet)
		if len(newHits) > 0 {
			termsErr := fmt.Errorf("polisher introduced new forbidden terms: %s: %w",
				formatForbiddenTermHits(newHits), domain.ErrValidation)
			polisherFallback(ctx, cfg, state, termsErr)
			return nil
		}

		// All checks passed: replace state.Narration, THEN emit the
		// text_generation audit so success and fallback remain mutually
		// exclusive in the audit ledger.
		state.Narration = &polished
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "polisher",
				Provider:  out.resp.Provider,
				Model:     out.resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   out.resp.CostUSD,
			})
		}
		return nil
	}
}

// polisherFallback logs and emits a polisher_failed audit event, leaving
// state.Narration unchanged. The caller returns nil so the run continues.
func polisherFallback(ctx context.Context, cfg TextAgentConfig, state *PipelineState, cause error) {
	if cfg.Logger != nil {
		cfg.Logger.Warn("polisher fallback: writer output preserved",
			"run_id", state.RunID,
			"error", cause.Error(),
		)
	}
	if cfg.AuditLogger != nil {
		_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
			Timestamp: time.Now(),
			EventType: domain.AuditEventPolisherFailed,
			RunID:     state.RunID,
			Stage:     "polisher",
		})
	}
}

// checkPolisherEditBudget verifies that no scene's narration rune-length
// delta exceeds domain.PolisherMaxEditRatio relative to the original. A
// scene-count mismatch is itself a structural violation. A ratio of
// exactly PolisherMaxEditRatio is accepted; only strictly greater
// triggers fallback.
func checkPolisherEditBudget(original, polished []domain.NarrationScene) error {
	if len(original) != len(polished) {
		return fmt.Errorf("polisher: scene count changed (%d → %d): %w",
			len(original), len(polished), domain.ErrValidation)
	}
	for i, orig := range original {
		pol := polished[i]
		origRunes := utf8.RuneCountInString(orig.Narration)
		polRunes := utf8.RuneCountInString(pol.Narration)
		if origRunes == 0 {
			if polRunes > 0 {
				return fmt.Errorf("polisher: scene[%d] narration grew from 0 to %d runes: %w",
					i, polRunes, domain.ErrValidation)
			}
			continue
		}
		delta := polRunes - origRunes
		if delta < 0 {
			delta = -delta
		}
		ratio := float64(delta) / float64(origRunes)
		if ratio > domain.PolisherMaxEditRatio {
			return fmt.Errorf(
				"polisher: scene[%d] narration rune-delta ratio %.2f exceeds budget %.2f: %w",
				i, ratio, domain.PolisherMaxEditRatio, domain.ErrValidation,
			)
		}
	}
	return nil
}

// checkPolisherReadOnlyInvariance enforces that every non-text field on
// every scene survives the polish unchanged. The polisher's mandate is
// to edit narration and narration_beats only; the JSON schema is too
// permissive (it would accept a polisher that swapped act_id values,
// reordered scenes, or rewrote fact_tags). This explicit check closes
// that gap.
//
// Caller must invoke checkPolisherEditBudget first to guarantee scene
// counts match — this function panics on a mismatch since that would be
// a programming error here.
func checkPolisherReadOnlyInvariance(original, polished []domain.NarrationScene) error {
	if len(original) != len(polished) {
		// Defensive: should be caught by checkPolisherEditBudget first.
		return fmt.Errorf("polisher: scene count changed (%d → %d): %w",
			len(original), len(polished), domain.ErrValidation)
	}
	for i := range original {
		orig := original[i]
		pol := polished[i]
		if orig.SceneNum != pol.SceneNum {
			return fmt.Errorf("polisher: scene[%d] scene_num mutated (%d → %d): %w",
				i, orig.SceneNum, pol.SceneNum, domain.ErrValidation)
		}
		if orig.ActID != pol.ActID {
			return fmt.Errorf("polisher: scene[%d] act_id mutated (%q → %q): %w",
				i, orig.ActID, pol.ActID, domain.ErrValidation)
		}
		if orig.Mood != pol.Mood {
			return fmt.Errorf("polisher: scene[%d] mood mutated (%q → %q): %w",
				i, orig.Mood, pol.Mood, domain.ErrValidation)
		}
		if orig.EntityVisible != pol.EntityVisible {
			return fmt.Errorf("polisher: scene[%d] entity_visible mutated (%v → %v): %w",
				i, orig.EntityVisible, pol.EntityVisible, domain.ErrValidation)
		}
		if orig.Location != pol.Location {
			return fmt.Errorf("polisher: scene[%d] location mutated (%q → %q): %w",
				i, orig.Location, pol.Location, domain.ErrValidation)
		}
		if orig.ColorPalette != pol.ColorPalette {
			return fmt.Errorf("polisher: scene[%d] color_palette mutated: %w",
				i, domain.ErrValidation)
		}
		if orig.Atmosphere != pol.Atmosphere {
			return fmt.Errorf("polisher: scene[%d] atmosphere mutated: %w",
				i, domain.ErrValidation)
		}
		if !reflect.DeepEqual(orig.CharactersPresent, pol.CharactersPresent) {
			return fmt.Errorf("polisher: scene[%d] characters_present mutated: %w",
				i, domain.ErrValidation)
		}
		if !reflect.DeepEqual(orig.FactTags, pol.FactTags) {
			return fmt.Errorf("polisher: scene[%d] fact_tags mutated: %w",
				i, domain.ErrValidation)
		}
	}
	return nil
}

// forbiddenHitKey is the dedup key for ForbiddenTermHit equality. Two
// hits collide iff they share the same scene number AND pattern.
type forbiddenHitKey struct {
	SceneNum int
	Pattern  string
}

func forbiddenHitKeySet(hits []ForbiddenTermHit) map[forbiddenHitKey]struct{} {
	out := make(map[forbiddenHitKey]struct{}, len(hits))
	for _, h := range hits {
		out[forbiddenHitKey{SceneNum: h.SceneNum, Pattern: h.Pattern}] = struct{}{}
	}
	return out
}

// filterNewForbiddenHits returns the subset of polishedHits not already
// present in originalHitSet. The writer is responsible for its own
// pre-existing matches; only newly-introduced hits should trigger
// polisher fallback.
func filterNewForbiddenHits(polishedHits []ForbiddenTermHit, originalHitSet map[forbiddenHitKey]struct{}) []ForbiddenTermHit {
	var out []ForbiddenTermHit
	for _, h := range polishedHits {
		if _, seen := originalHitSet[forbiddenHitKey{SceneNum: h.SceneNum, Pattern: h.Pattern}]; seen {
			continue
		}
		out = append(out, h)
	}
	return out
}

func renderPolisherPrompt(state *PipelineState, prompts PromptAssets) (string, error) {
	scriptJSON, err := json.MarshalIndent(state.Narration, "", "  ")
	if err != nil {
		return "", fmt.Errorf("polisher: marshal narration: %w", domain.ErrValidation)
	}
	replacer := strings.NewReplacer(
		"{scp_id}", state.SCPID,
		"{narration_script_json}", string(scriptJSON),
	)
	return replacer.Replace(prompts.PolisherTemplate), nil
}
