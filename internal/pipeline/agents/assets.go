package agents

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/prompts"
)

type PromptAssets struct {
	WriterTemplate          string
	CriticTemplate          string
	VisualBreakdownTemplate string
	ReviewerTemplate        string
	FormatGuide             string
}

const (
	writerPromptPath          = "docs/prompts/scenario/03_writing.md"
	criticPromptPath          = "docs/prompts/scenario/critic_agent.md"
	visualBreakdownPromptPath = "docs/prompts/scenario/03_5_visual_breakdown.md"
	reviewerPromptPath        = "docs/prompts/scenario/04_review.md"
	formatGuidePath           = "docs/prompts/scenario/format_guide.md"
)

// LoadPromptAssets reads every agent prompt off disk. useTemplatePrompts
// selects the writer template source: false → legacy markdown at
// docs/prompts/scenario/03_writing.md (default); true → embedded v2
// template at prompts/agents/script_writer.tmpl. The flag is sourced
// from domain.PipelineConfig.UseTemplatePrompts (config.yaml), not from
// the environment — env-only toggles in a 1-operator pipeline are dead
// layers (memory: feedback_config_not_env.md).
func LoadPromptAssets(projectRoot string, useTemplatePrompts bool) (PromptAssets, error) {
	if projectRoot == "" {
		return PromptAssets{}, fmt.Errorf("load prompt assets: %w: projectRoot is empty", domain.ErrValidation)
	}

	writerTemplate, err := loadWriterTemplate(projectRoot, useTemplatePrompts)
	if err != nil {
		return PromptAssets{}, err
	}
	criticTemplate, err := readAsset(projectRoot, criticPromptPath)
	if err != nil {
		return PromptAssets{}, err
	}
	visualBreakdownTemplate, err := readAsset(projectRoot, visualBreakdownPromptPath)
	if err != nil {
		return PromptAssets{}, err
	}
	reviewerTemplate, err := readAsset(projectRoot, reviewerPromptPath)
	if err != nil {
		return PromptAssets{}, err
	}
	formatGuide, err := readAsset(projectRoot, formatGuidePath)
	if err != nil {
		return PromptAssets{}, err
	}

	return PromptAssets{
		WriterTemplate:          writerTemplate,
		CriticTemplate:          criticTemplate,
		VisualBreakdownTemplate: visualBreakdownTemplate,
		ReviewerTemplate:        reviewerTemplate,
		FormatGuide:             formatGuide,
	}, nil
}

func readAsset(projectRoot, rel string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(projectRoot, rel))
	if err != nil {
		return "", fmt.Errorf("load prompt asset %s: %w", rel, domain.ErrValidation)
	}
	return string(raw), nil
}

// loadWriterTemplate selects the writer prompt template source. With
// useTemplatePrompts=false (default), behavior is byte-for-byte
// identical to the pre-flag implementation: the legacy markdown at
// docs/prompts/scenario/03_writing.md drives the writer.
//
// When true, the writer reads the embedded template from
// prompts/agents/script_writer.tmpl (go:embed). The placeholder set is
// identical, so renderWriterActPrompt's strings.NewReplacer pipeline
// keeps producing valid prompts without any other code changes.
func loadWriterTemplate(projectRoot string, useTemplatePrompts bool) (string, error) {
	if useTemplatePrompts {
		body, err := prompts.ReadAgent(prompts.AgentScriptWriter)
		if err != nil {
			return "", fmt.Errorf("load prompt asset %s: %w", prompts.AgentScriptWriter, domain.ErrValidation)
		}
		return body, nil
	}
	return readAsset(projectRoot, writerPromptPath)
}
