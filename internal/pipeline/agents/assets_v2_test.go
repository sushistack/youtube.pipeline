package agents

import (
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestLoadPromptAssets_LegacyByDefault confirms the on-disk markdown path
// (docs/prompts/scenario/03_writing.md) drives the writer when
// UseTemplatePrompts=false. v2 made both paths emit the same monologue-spec
// prompt; the legacy/embedded distinction is now a deploy-time toggle, not
// a content split.
func TestLoadPromptAssets_LegacyByDefault(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	assets, err := LoadPromptAssets(testutil.ProjectRoot(t), false)
	if err != nil {
		t.Fatalf("LoadPromptAssets: %v", err)
	}
	if !strings.Contains(assets.WriterTemplate, "Korean Monologue Writing") {
		t.Errorf("default path must serve the v2 monologue-spec writer template")
	}
	if assets.SegmenterTemplate == "" {
		t.Errorf("default path must populate SegmenterTemplate")
	}
}

// TestLoadPromptAssets_TemplateOptIn covers the
// PipelineConfig.UseTemplatePrompts=true path. The embedded template must
// expose the same v2 placeholder set as the on-disk prompt.
func TestLoadPromptAssets_TemplateOptIn(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	assets, err := LoadPromptAssets(testutil.ProjectRoot(t), true)
	if err != nil {
		t.Fatalf("LoadPromptAssets: %v", err)
	}
	if !strings.Contains(assets.WriterTemplate, "Korean Monologue Writing") {
		t.Errorf("opt-in path must serve the v2 script_writer.tmpl template")
	}
	for _, placeholder := range []string{
		"{scp_id}", "{act_id}", "{monologue_rune_cap}",
		"{act_synopsis}", "{act_key_points}", "{prior_act_summary}",
		"{scp_visual_reference}", "{containment_constraints}",
		"{format_guide}", "{forbidden_terms_section}", "{glossary_section}",
		"{quality_feedback}", "{exemplar_scenes}",
	} {
		if !strings.Contains(assets.WriterTemplate, placeholder) {
			t.Errorf("v2 writer template missing placeholder %s — strings.NewReplacer would leak raw token at runtime", placeholder)
		}
	}
	for _, placeholder := range []string{
		"{act_id}", "{act_mood}", "{act_key_points}",
		"{monologue}", "{monologue_rune_count}",
		"{scp_visual_reference}", "{fact_tag_catalog}",
	} {
		if !strings.Contains(assets.SegmenterTemplate, placeholder) {
			t.Errorf("v2 segmenter template missing placeholder %s", placeholder)
		}
	}
}
