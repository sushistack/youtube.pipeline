package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// hangulRE matches any Hangul Jamo (U+1100–U+11FF), Hangul Compatibility Jamo
// (U+3130–U+318F), or Hangul Syllables (U+AC00–U+D7A3). Compiled once at
// package init to avoid per-call recompilation.
var hangulRE = regexp.MustCompile(`[\x{1100}-\x{11FF}\x{3130}-\x{318F}\x{AC00}-\x{D7A3}]`)

func NewPostWriterCritic(
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	writerValidator *Validator,
	criticValidator *Validator,
	terms *ForbiddenTerms,
	writerProvider string,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state == nil:
			return fmt.Errorf("critic: %w: state is nil", domain.ErrValidation)
		case state.Narration == nil:
			return fmt.Errorf("critic: %w: narration is nil", domain.ErrValidation)
		case cfg.Model == "":
			return fmt.Errorf("critic: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("critic: %w: provider is empty", domain.ErrValidation)
		case writerProvider == "":
			return fmt.Errorf("critic: %w: writer provider is empty", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("critic: %w: generator is nil", domain.ErrValidation)
		case writerValidator == nil:
			return fmt.Errorf("critic: %w: writer validator is nil", domain.ErrValidation)
		case criticValidator == nil:
			return fmt.Errorf("critic: %w: critic validator is nil", domain.ErrValidation)
		case terms == nil:
			return fmt.Errorf("critic: %w: forbidden terms are nil", domain.ErrValidation)
		}
		if err := validateDistinctProvidersLocal(writerProvider, cfg.Provider); err != nil {
			return fmt.Errorf("critic: %w", err)
		}

		precheck, err := runPostWriterPrecheck(state.Narration, writerValidator, terms)
		if err != nil {
			return fmt.Errorf("critic: %w", err)
		}
		if precheck.ShortCircuited {
			report := buildPrecheckRetryReport(precheck, cfg)
			// Defense-in-depth: validate the synthesized precheck report against
			// the critic schema so any future struct drift is caught here rather
			// than silently emitted downstream.
			if err := criticValidator.Validate(report); err != nil {
				return fmt.Errorf("critic: precheck report failed schema: %w", err)
			}
			ensureCriticState(state).PostWriter = &report
			return nil
		}

		prompt, err := renderCriticPrompt(state.Narration, prompts)
		if err != nil {
			return err
		}

		if cfg.Logger != nil {
			cfg.Logger.Info("critic call start",
				"run_id", state.RunID,
				"checkpoint", "post_writer",
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
				cfg.Logger.Error("critic call failed",
					"run_id", state.RunID,
					"checkpoint", "post_writer",
					"duration_ms", time.Since(callStart).Milliseconds(),
					"error", err.Error(),
				)
			}
			return err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("critic call complete",
				"run_id", state.RunID,
				"checkpoint", "post_writer",
				"duration_ms", time.Since(callStart).Milliseconds(),
				"finish_reason", resp.FinishReason,
				"tokens_in", resp.TokensIn,
				"tokens_out", resp.TokensOut,
			)
		}

		// Non-fatal audit write after successful post-writer critic generation.
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "post_writer_critic",
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}

		var report domain.CriticCheckpointReport
		if err := decodeJSONResponse(resp.Content, &report); err != nil {
			return fmt.Errorf("critic: %w", err)
		}
		if report.OverallScore == 0 && report.Verdict != domain.CriticVerdictRetry {
			report.OverallScore = scoreRubric(report.Rubric)
		}
		if report.Verdict == domain.CriticVerdictRetry && report.RetryReason == "" {
			report.RetryReason = DeriveRetryReason(report.Rubric)
		} else if report.Verdict != domain.CriticVerdictRetry {
			report.RetryReason = ""
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("critic verdict",
				"run_id", state.RunID,
				"checkpoint", "post_writer",
				"verdict", string(report.Verdict),
				"overall_score", report.OverallScore,
				"retry_reason", report.RetryReason,
			)
		}
		report.Checkpoint = domain.CriticCheckpointPostWriter
		report.Precheck = precheck
		if resp.Model != "" {
			report.CriticModel = resp.Model
		} else if report.CriticModel == "" {
			report.CriticModel = cfg.Model
		}
		if resp.Provider != "" {
			report.CriticProvider = resp.Provider
		} else if report.CriticProvider == "" {
			report.CriticProvider = cfg.Provider
		}
		report.SourceVersion = domain.CriticSourceVersionV2
		if !containsHangul(report.Feedback) {
			return fmt.Errorf("critic: feedback must remain Korean: %w", domain.ErrValidation)
		}
		if err := criticValidator.Validate(report); err != nil {
			return fmt.Errorf("critic: %w", err)
		}

		ensureCriticState(state).PostWriter = &report
		return nil
	}
}

func NewPostReviewerCritic(
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	writerValidator *Validator,
	visualValidator *Validator,
	reviewValidator *Validator,
	criticValidator *Validator,
	terms *ForbiddenTerms,
	writerProvider string,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state == nil:
			return fmt.Errorf("critic: %w: state is nil", domain.ErrValidation)
		case state.Narration == nil:
			return fmt.Errorf("critic: %w: narration is nil", domain.ErrValidation)
		case state.VisualScript == nil:
			return fmt.Errorf("critic: %w: visual breakdown is nil", domain.ErrValidation)
		case state.Review == nil:
			return fmt.Errorf("critic: %w: review is nil", domain.ErrValidation)
		case state.Critic == nil || state.Critic.PostWriter == nil:
			return fmt.Errorf("critic: %w: post_writer critic is nil", domain.ErrValidation)
		case cfg.Model == "":
			return fmt.Errorf("critic: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("critic: %w: provider is empty", domain.ErrValidation)
		case writerProvider == "":
			return fmt.Errorf("critic: %w: writer provider is empty", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("critic: %w: generator is nil", domain.ErrValidation)
		case writerValidator == nil:
			return fmt.Errorf("critic: %w: writer validator is nil", domain.ErrValidation)
		case visualValidator == nil:
			return fmt.Errorf("critic: %w: visual validator is nil", domain.ErrValidation)
		case reviewValidator == nil:
			return fmt.Errorf("critic: %w: review validator is nil", domain.ErrValidation)
		case criticValidator == nil:
			return fmt.Errorf("critic: %w: critic validator is nil", domain.ErrValidation)
		case terms == nil:
			return fmt.Errorf("critic: %w: forbidden terms are nil", domain.ErrValidation)
		}
		if err := validateDistinctProvidersLocal(writerProvider, cfg.Provider); err != nil {
			return fmt.Errorf("critic: %w", err)
		}

		precheck, shortCircuit, err := runPostReviewerPrecheck(state, cfg, writerValidator, visualValidator, reviewValidator, terms)
		if err != nil {
			return fmt.Errorf("critic: %w", err)
		}
		if shortCircuit != nil {
			shortCircuit.Precheck = precheck
			if err := criticValidator.Validate(*shortCircuit); err != nil {
				return fmt.Errorf("critic: short-circuit report failed schema: %w", err)
			}
			ensureCriticState(state).PostReviewer = shortCircuit
			return nil
		}

		prompt, err := renderCriticPrompt(state, prompts)
		if err != nil {
			return err
		}

		if cfg.Logger != nil {
			cfg.Logger.Info("critic call start",
				"run_id", state.RunID,
				"checkpoint", "post_reviewer",
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
				cfg.Logger.Error("critic call failed",
					"run_id", state.RunID,
					"checkpoint", "post_reviewer",
					"duration_ms", time.Since(callStart).Milliseconds(),
					"error", err.Error(),
				)
			}
			return err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("critic call complete",
				"run_id", state.RunID,
				"checkpoint", "post_reviewer",
				"duration_ms", time.Since(callStart).Milliseconds(),
				"finish_reason", resp.FinishReason,
				"tokens_in", resp.TokensIn,
				"tokens_out", resp.TokensOut,
			)
		}

		// Non-fatal audit write after successful post-reviewer critic generation.
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "critic",
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}

		var report domain.CriticCheckpointReport
		if err := decodeJSONResponse(resp.Content, &report); err != nil {
			return fmt.Errorf("critic: %w", err)
		}
		if report.OverallScore == 0 && report.Verdict != domain.CriticVerdictRetry {
			report.OverallScore = scoreRubric(report.Rubric)
		}
		if report.Verdict == domain.CriticVerdictRetry && report.RetryReason == "" {
			report.RetryReason = DeriveRetryReason(report.Rubric)
		} else if report.Verdict != domain.CriticVerdictRetry {
			report.RetryReason = ""
		}
		if err := validateMinorPolicyFindings(report.MinorPolicyFindings, state.Narration); err != nil {
			return fmt.Errorf("critic: %w", err)
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("critic verdict",
				"run_id", state.RunID,
				"checkpoint", "post_reviewer",
				"verdict", string(report.Verdict),
				"overall_score", report.OverallScore,
				"retry_reason", report.RetryReason,
			)
		}
		report.Checkpoint = domain.CriticCheckpointPostReviewer
		report.Precheck = precheck
		if resp.Model != "" {
			report.CriticModel = resp.Model
		} else if report.CriticModel == "" {
			report.CriticModel = cfg.Model
		}
		if resp.Provider != "" {
			report.CriticProvider = resp.Provider
		} else if report.CriticProvider == "" {
			report.CriticProvider = cfg.Provider
		}
		report.SourceVersion = domain.CriticSourceVersionPostReviewerV2
		if !containsHangul(report.Feedback) {
			return fmt.Errorf("critic: feedback must remain Korean: %w", domain.ErrValidation)
		}
		if err := criticValidator.Validate(report); err != nil {
			return fmt.Errorf("critic: %w", err)
		}

		ensureCriticState(state).PostReviewer = &report
		return nil
	}
}

// validateMinorPolicyFindings checks every LLM-emitted finding against the
// script's act/monologue layout: ActID must match an existing
// NarrationScript.Acts[i].ActID, RuneOffset must point inside that act's
// monologue (0 ≤ RuneOffset ≤ utf8.RuneCount(monologue)), and Reason must
// remain Korean. RuneOffset == 0 is always allowed (act-paragraph anchor).
// An empty monologue (rune count 0) accepts only RuneOffset == 0; any
// positive offset against an empty act fails validation.
func validateMinorPolicyFindings(findings []domain.MinorPolicyFinding, script *domain.NarrationScript) error {
	if len(findings) == 0 || script == nil {
		return nil
	}
	actMonoLen := make(map[string]int, len(script.Acts))
	for _, act := range script.Acts {
		actMonoLen[act.ActID] = utf8.RuneCountInString(act.Monologue)
	}
	for _, finding := range findings {
		monoLen, ok := actMonoLen[finding.ActID]
		if !ok {
			return fmt.Errorf("minor_policy_findings act_id=%q not in narration: %w", finding.ActID, domain.ErrValidation)
		}
		if finding.RuneOffset < 0 || finding.RuneOffset > monoLen {
			return fmt.Errorf("minor_policy_findings act_id=%q rune_offset=%d out of range (0..%d): %w", finding.ActID, finding.RuneOffset, monoLen, domain.ErrValidation)
		}
		if !containsHangul(finding.Reason) {
			return fmt.Errorf("minor_policy_findings reason must remain Korean: %w", domain.ErrValidation)
		}
	}
	return nil
}

func renderCriticPrompt(payload any, prompts PromptAssets) (string, error) {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("critic: marshal scenario payload: %w", domain.ErrValidation)
	}
	replacer := strings.NewReplacer(
		"{format_guide}", prompts.FormatGuide,
		"{scenario_json}", string(raw),
	)
	return replacer.Replace(prompts.CriticTemplate), nil
}

func ensureCriticState(state *PipelineState) *domain.CriticOutput {
	if state.Critic == nil {
		state.Critic = &domain.CriticOutput{}
	}
	return state.Critic
}

func scoreRubric(scores domain.CriticRubricScores) int {
	return int(math.Round(float64(scores.Hook)*domain.CriticRubricWeights["hook"] +
		float64(scores.FactAccuracy)*domain.CriticRubricWeights["fact_accuracy"] +
		float64(scores.EmotionalVariation)*domain.CriticRubricWeights["emotional_variation"] +
		float64(scores.Immersion)*domain.CriticRubricWeights["immersion"]))
}

func containsHangul(s string) bool {
	return hangulRE.MatchString(s)
}

func validateDistinctProvidersLocal(writerProvider, criticProvider string) error {
	if writerProvider == "" || criticProvider == "" {
		return fmt.Errorf("%w: writer and critic providers must be non-empty", domain.ErrValidation)
	}
	if writerProvider == criticProvider {
		return fmt.Errorf("%w: Writer and Critic must use different LLM providers", domain.ErrValidation)
	}
	return nil
}

func decodeJSONResponse(content string, dst any) error {
	trimmed := strings.TrimSpace(content)
	// Strip a leading UTF-8 BOM (U+FEFF) which TrimSpace does not remove.
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return fmt.Errorf("empty JSON response: %w", domain.ErrValidation)
	}
	if strings.HasPrefix(trimmed, "```") {
		inner, ok := trimSingleFence(trimmed)
		if !ok {
			return fmt.Errorf("response is not bare JSON: %w", domain.ErrValidation)
		}
		trimmed = inner
	}
	if !(strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		return fmt.Errorf("response is not bare JSON: %w", domain.ErrValidation)
	}
	if err := json.Unmarshal([]byte(trimmed), dst); err != nil {
		return fmt.Errorf("invalid JSON (%s): %w", err.Error(), domain.ErrValidation)
	}
	return nil
}

func trimSingleFence(s string) (string, bool) {
	// Normalize Windows line endings so that downstream TrimSpace / label
	// comparison does not see stray '\r' characters.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) < 3 {
		return "", false
	}
	first := strings.TrimSpace(lines[0])
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(first, "```") || last != "```" {
		return "", false
	}
	label := strings.TrimSpace(strings.TrimPrefix(first, "```"))
	if label != "" && !strings.EqualFold(label, "json") {
		return "", false
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n")), true
}
