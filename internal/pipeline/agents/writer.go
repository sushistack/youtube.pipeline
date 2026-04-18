package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type TextAgentConfig struct {
	Model       string
	Provider    string
	MaxTokens   int
	Temperature float64
}

func NewWriter(
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	validator *Validator,
	terms *ForbiddenTerms,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state == nil:
			return fmt.Errorf("writer: %w: state is nil", domain.ErrValidation)
		case state.Research == nil:
			return fmt.Errorf("writer: %w: research is nil", domain.ErrValidation)
		case state.Structure == nil:
			return fmt.Errorf("writer: %w: structure is nil", domain.ErrValidation)
		case cfg.Model == "":
			return fmt.Errorf("writer: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("writer: %w: provider is empty", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("writer: %w: generator is nil", domain.ErrValidation)
		case validator == nil:
			return fmt.Errorf("writer: %w: validator is nil", domain.ErrValidation)
		case terms == nil:
			return fmt.Errorf("writer: %w: forbidden terms are nil", domain.ErrValidation)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		prompt, err := renderWriterPrompt(state, prompts, terms)
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

		var script domain.NarrationScript
		if err := decodeJSONResponse(resp.Content, &script); err != nil {
			return fmt.Errorf("writer: %w", err)
		}

		fillNarrationMetadata(&script, resp, cfg, terms)
		if err := validator.Validate(script); err != nil {
			return fmt.Errorf("writer: %w", err)
		}

		hits := terms.MatchNarration(&script)
		if len(hits) > 0 {
			return fmt.Errorf("writer: %s: %w", formatForbiddenTermHits(hits), domain.ErrValidation)
		}

		state.Narration = &script
		return nil
	}
}

func renderWriterPrompt(state *PipelineState, prompts PromptAssets, terms *ForbiddenTerms) (string, error) {
	structureJSON, err := json.MarshalIndent(state.Structure, "", "  ")
	if err != nil {
		return "", fmt.Errorf("writer: marshal structure: %w", domain.ErrValidation)
	}
	visualJSON, err := json.MarshalIndent(state.Research.VisualIdentity, "", "  ")
	if err != nil {
		return "", fmt.Errorf("writer: marshal visual identity: %w", domain.ErrValidation)
	}
	forbidden := renderForbiddenTermsSection(terms.Raw)
	replacer := strings.NewReplacer(
		"{scp_id}", state.SCPID,
		"{scene_structure}", string(structureJSON),
		"{scp_visual_reference}", string(visualJSON),
		"{format_guide}", prompts.FormatGuide,
		"{forbidden_terms_section}", forbidden,
		"{glossary_section}", "",
		"{quality_feedback}", "",
	)
	return replacer.Replace(prompts.WriterTemplate), nil
}

func renderForbiddenTermsSection(patterns []string) string {
	if len(patterns) == 0 {
		return "## Forbidden Terms\n- None"
	}
	lines := make([]string, 0, len(patterns)+1)
	lines = append(lines, "## Forbidden Terms")
	for _, pattern := range patterns {
		lines = append(lines, "- "+pattern)
	}
	return strings.Join(lines, "\n")
}

func fillNarrationMetadata(script *domain.NarrationScript, resp domain.TextResponse, cfg TextAgentConfig, terms *ForbiddenTerms) {
	if resp.Model == "" {
		resp.Model = cfg.Model
	}
	if resp.Provider == "" {
		resp.Provider = cfg.Provider
	}
	script.Metadata = domain.NarrationMetadata{
		Language:              domain.LanguageKorean,
		SceneCount:            len(script.Scenes),
		WriterModel:           resp.Model,
		WriterProvider:        resp.Provider,
		PromptTemplate:        filepath.Base(writerPromptPath),
		FormatGuideTemplate:   filepath.Base(formatGuidePath),
		ForbiddenTermsVersion: terms.Version,
	}
	script.SourceVersion = domain.NarrationSourceVersionV1
}
