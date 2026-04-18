package agents

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/domain"
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

	writerTemplate, err := readAsset(projectRoot, writerPromptPath)
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
