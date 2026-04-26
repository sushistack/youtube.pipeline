package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

func NewReviewer(
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	visualValidator *Validator,
	reviewValidator *Validator,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state == nil:
			return fmt.Errorf("reviewer: %w: state is nil", domain.ErrValidation)
		case state.Research == nil:
			return fmt.Errorf("reviewer: %w: research is nil", domain.ErrValidation)
		case state.Narration == nil:
			return fmt.Errorf("reviewer: %w: narration is nil", domain.ErrValidation)
		case state.VisualBreakdown == nil:
			return fmt.Errorf("reviewer: %w: visual breakdown is nil", domain.ErrValidation)
		case cfg.Model == "":
			return fmt.Errorf("reviewer: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("reviewer: %w: provider is empty", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("reviewer: %w: generator is nil", domain.ErrValidation)
		case visualValidator == nil:
			return fmt.Errorf("reviewer: %w: visual validator is nil", domain.ErrValidation)
		case reviewValidator == nil:
			return fmt.Errorf("reviewer: %w: review validator is nil", domain.ErrValidation)
		}
		if err := visualValidator.Validate(*state.VisualBreakdown); err != nil {
			return fmt.Errorf("reviewer: %w", err)
		}

		prompt, err := renderReviewerPrompt(state, prompts)
		if err != nil {
			return err
		}
		resp, err := gen.Generate(ctx, domain.TextRequest{
			Prompt:      prompt,
			Model:       cfg.Model,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		})
		if err != nil {
			return err
		}

		// Non-fatal audit write after successful review generation.
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "reviewer",
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}

		var report domain.ReviewReport
		if err := decodeJSONResponse(resp.Content, &report); err != nil {
			return fmt.Errorf("reviewer: %w", err)
		}
		if report.Issues == nil {
			report.Issues = []domain.ReviewIssue{}
		}
		if report.Corrections == nil {
			report.Corrections = []domain.ReviewCorrection{}
		}
		report.ReviewerModel = fallbackString(resp.Model, cfg.Model)
		report.ReviewerProvider = fallbackString(resp.Provider, cfg.Provider)
		report.SourceVersion = domain.ReviewSourceVersionV1
		if firstCritical, ok := firstCriticalIssue(report.Issues); ok && report.OverallPass {
			report.OverallPass = false
			report.Issues = append(report.Issues, domain.ReviewIssue{
				SceneNum:    firstCritical.SceneNum,
				Type:        domain.ReviewIssueConsistencyIssue,
				Severity:    "info",
				Description: "overall_pass forced to false because at least one critical issue was reported",
				Correction:  "resolve the critical issue(s) above before approving this review",
			})
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("reviewer result",
				"run_id", state.RunID,
				"overall_pass", report.OverallPass,
				"coverage_pct", report.CoveragePct,
				"issue_count", len(report.Issues),
			)
			for _, issue := range report.Issues {
				if issue.Severity == "critical" {
					cfg.Logger.Warn("reviewer critical issue",
						"run_id", state.RunID,
						"scene_num", issue.SceneNum,
						"type", issue.Type,
						"description", issue.Description,
						"correction", issue.Correction,
					)
				}
			}
		}
		if err := reviewValidator.Validate(report); err != nil {
			return fmt.Errorf("reviewer: %w", err)
		}
		state.Review = &report
		return nil
	}
}

func renderReviewerPrompt(state *PipelineState, prompts PromptAssets) (string, error) {
	researchJSON, err := json.MarshalIndent(state.Research, "", "  ")
	if err != nil {
		return "", fmt.Errorf("reviewer: marshal research: %w", domain.ErrValidation)
	}
	narrationJSON, err := json.MarshalIndent(state.Narration, "", "  ")
	if err != nil {
		return "", fmt.Errorf("reviewer: marshal narration: %w", domain.ErrValidation)
	}
	visualJSON, err := json.MarshalIndent(state.VisualBreakdown, "", "  ")
	if err != nil {
		return "", fmt.Errorf("reviewer: marshal visual breakdown: %w", domain.ErrValidation)
	}
	replacer := strings.NewReplacer(
		"{scp_fact_sheet}", string(researchJSON),
		"{narration_script}", string(narrationJSON),
		"{visual_descriptions}", string(visualJSON),
		"{scp_visual_reference}", state.VisualBreakdown.FrozenDescriptor,
		"{format_guide}", prompts.FormatGuide,
	)
	return replacer.Replace(prompts.ReviewerTemplate), nil
}

func hasCriticalIssue(issues []domain.ReviewIssue) bool {
	_, ok := firstCriticalIssue(issues)
	return ok
}

func firstCriticalIssue(issues []domain.ReviewIssue) (domain.ReviewIssue, bool) {
	for _, issue := range issues {
		if issue.Severity == "critical" {
			return issue, true
		}
	}
	return domain.ReviewIssue{}, false
}
