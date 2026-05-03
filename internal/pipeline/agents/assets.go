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
	PolisherTemplate        string
	CriticTemplate          string
	VisualBreakdownTemplate string
	ReviewerTemplate        string
	RoleClassifierTemplate  string
	FormatGuide             string
	// ExemplarsByAct holds per-act narration text from real Korean SCP
	// YouTube exemplar files, keyed by domain act ID. Injected into the
	// writer prompt at render time so the LLM gets in-context tone/rhythm
	// signal from native channels (Phase 1: 2 exemplars × 4 acts).
	ExemplarsByAct map[string]string
}

const (
	writerPromptPath          = "docs/prompts/scenario/03_writing.md"
	polisherPromptPath        = "docs/prompts/scenario/03_5_polish.md"
	criticPromptPath          = "docs/prompts/scenario/critic_agent.md"
	visualBreakdownPromptPath = "docs/prompts/scenario/03_5_visual_breakdown.md"
	reviewerPromptPath        = "docs/prompts/scenario/04_review.md"
	roleClassifierPromptPath  = "docs/prompts/scenario/01_5_role_classifier.md"
	formatGuidePath           = "docs/prompts/scenario/format_guide.md"
)

// exemplarPaths lists the writer fewshot exemplar markdown files. Phase 1
// pins two: SCP-049 (humanoid emotional, "이게 뭐냐" hook) + SCP-2790
// (humanoid threat, mystery-question hook). Both are 하다Hada channel —
// single-creator imitation accepted at this hypothesis-verification stage
// (per the spec's design notes). Phase 2 may add 유령시티 / 한국 cultural
// channels for diversity.
var exemplarPaths = []string{
	"docs/exemplars/scp-049.exemplar.md",
	"docs/exemplars/scp-2790.exemplar.md",
}

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
	polisherTemplate, err := readAsset(projectRoot, polisherPromptPath)
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
	roleClassifierTemplate, err := readAsset(projectRoot, roleClassifierPromptPath)
	if err != nil {
		return PromptAssets{}, err
	}
	formatGuide, err := readAsset(projectRoot, formatGuidePath)
	if err != nil {
		return PromptAssets{}, err
	}

	exemplarsByAct, err := loadExemplars(projectRoot, exemplarPaths)
	if err != nil {
		return PromptAssets{}, err
	}

	return PromptAssets{
		WriterTemplate:          writerTemplate,
		PolisherTemplate:        polisherTemplate,
		CriticTemplate:          criticTemplate,
		VisualBreakdownTemplate: visualBreakdownTemplate,
		ReviewerTemplate:        reviewerTemplate,
		RoleClassifierTemplate:  roleClassifierTemplate,
		FormatGuide:             formatGuide,
		ExemplarsByAct:          exemplarsByAct,
	}, nil
}

// loadExemplars reads each exemplar markdown file, parses out its per-act
// narration via parseExemplar, then merges all inputs into one per-act map
// where each entry concatenates every exemplar's contribution. Any read or
// parse failure surfaces as ErrValidation so server start fails fast — a
// runtime fallback to "no exemplars" would silently regress writer output
// quality below the level we just shipped.
func loadExemplars(projectRoot string, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("load exemplars: no exemplar paths configured: %w", domain.ErrValidation)
	}
	parsed := make([]map[string]string, 0, len(paths))
	for _, rel := range paths {
		raw, err := readAsset(projectRoot, rel)
		if err != nil {
			return nil, err
		}
		acts, err := parseExemplar(raw)
		if err != nil {
			return nil, fmt.Errorf("load exemplar %s: %w", rel, err)
		}
		parsed = append(parsed, acts)
	}
	return concatExemplars(parsed)
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
