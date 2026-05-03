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

func LoadPromptAssets(projectRoot string) (PromptAssets, error) {
	if projectRoot == "" {
		return PromptAssets{}, fmt.Errorf("load prompt assets: %w: projectRoot is empty", domain.ErrValidation)
	}

	writerTemplate, err := loadWriterTemplate(projectRoot)
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

// loadWriterTemplate honors the USE_TEMPLATE_PROMPTS feature flag from
// spec section 7. With the flag off (default), behavior is byte-for-byte
// identical to the pre-change `readAsset(projectRoot, writerPromptPath)`
// call: the legacy markdown at docs/prompts/scenario/03_writing.md
// continues to drive the writer.
//
// When the flag is set to "true", the writer reads the embedded template
// from the prompts/agents/script_writer.tmpl file shipped via go:embed.
// The placeholder set is identical (same {var} tokens substituted by
// renderWriterActPrompt), so the existing strings.NewReplacer pipeline
// keeps producing valid prompts without any other code changes.
func loadWriterTemplate(projectRoot string) (string, error) {
	if os.Getenv(prompts.EnvFlag) == prompts.EnvOn {
		body, err := prompts.ReadAgent(prompts.AgentScriptWriter)
		if err != nil {
			return "", fmt.Errorf("load prompt asset %s: %w", prompts.AgentScriptWriter, domain.ErrValidation)
		}
		return body, nil
	}
	return readAsset(projectRoot, writerPromptPath)
}
