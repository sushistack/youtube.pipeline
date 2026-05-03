package agents

import (
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestLoadPromptAssets_LegacyByDefault is the v1-regression guard
// required by spec section 8.1. With UseTemplatePrompts=false (the
// PipelineConfig default in config.yaml), the writer template must
// come from docs/prompts/scenario/03_writing.md byte-for-byte.
func TestLoadPromptAssets_LegacyByDefault(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	assets, err := LoadPromptAssets(testutil.ProjectRoot(t), false)
	if err != nil {
		t.Fatalf("LoadPromptAssets: %v", err)
	}
	if !strings.Contains(assets.WriterTemplate, "Stage 3: Korean Narration Script Writing") {
		t.Errorf("default path must serve the legacy 03_writing.md template")
	}
	// The legacy template lacks the v2-only "SCP Explained quality bar"
	// section. Its absence is the strongest signal that we did not
	// accidentally serve the new template.
	if strings.Contains(assets.WriterTemplate, "SCP Explained quality bar") {
		t.Errorf("default path leaked v2 template marker")
	}
}

// TestLoadPromptAssets_TemplateOptIn covers the
// PipelineConfig.UseTemplatePrompts=true path.
func TestLoadPromptAssets_TemplateOptIn(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	assets, err := LoadPromptAssets(testutil.ProjectRoot(t), true)
	if err != nil {
		t.Fatalf("LoadPromptAssets: %v", err)
	}
	if !strings.Contains(assets.WriterTemplate, "SCP Explained quality bar") {
		t.Errorf("opt-in path must serve the v2 script_writer.tmpl template")
	}
	// The v2 template MUST keep the same {var} placeholder set as the
	// legacy renderer expects — otherwise strings.NewReplacer will leak
	// raw "{xxx}" tokens into the LLM prompt at runtime.
	for _, placeholder := range []string{
		"{scp_id}", "{act_id}", "{scene_num_range}", "{scene_budget}",
		"{act_synopsis}", "{act_key_points}", "{prior_act_summary}",
		"{scp_visual_reference}", "{format_guide}",
		"{forbidden_terms_section}", "{glossary_section}", "{quality_feedback}",
	} {
		if !strings.Contains(assets.WriterTemplate, placeholder) {
			t.Errorf("v2 template missing placeholder %s — strings.NewReplacer would leak raw token at runtime", placeholder)
		}
	}
}
