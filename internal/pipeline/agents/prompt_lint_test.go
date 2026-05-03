package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// placeholderRE matches `{snake_case}` template placeholders. JSON example
// blocks inside prompt files (e.g. `{ "act_id": "..." }`) do not match
// because the regex requires a single lowercase identifier with no spaces
// or quotes between the braces.
var placeholderRE = regexp.MustCompile(`\{[a-z][a-z0-9_]*\}`)

// promptRendererContract enumerates, per LLM-driven prompt file, the exact
// set of placeholders the renderer in this package substitutes. It is a
// hand-maintained mirror of the strings.NewReplacer(...) calls in
// renderWriterActPrompt, renderCriticPrompt, renderReviewerPrompt, and
// renderVisualBreakdownPrompt. When you change one of those replacers,
// update this list too — the test fails the build if either side drifts.
var promptRendererContract = []struct {
	name        string
	path        string
	substitutes []string
}{
	{
		name: "writer",
		path: writerPromptPath,
		substitutes: []string{
			"scp_id", "act_id", "scene_num_range", "scene_budget",
			"act_synopsis", "act_key_points", "prior_act_summary",
			"scp_visual_reference", "format_guide",
			"forbidden_terms_section", "glossary_section", "quality_feedback",
			"exemplar_scenes",
		},
	},
	{
		name:        "critic",
		path:        criticPromptPath,
		substitutes: []string{"format_guide", "scenario_json"},
	},
	{
		name: "reviewer",
		path: reviewerPromptPath,
		substitutes: []string{
			"scp_fact_sheet", "narration_script", "visual_descriptions",
			"scp_visual_reference", "format_guide",
		},
	},
	{
		name: "visual_breakdown",
		path: visualBreakdownPromptPath,
		substitutes: []string{
			"scene_num", "location", "characters_present", "color_palette",
			"atmosphere", "scp_visual_reference", "narration",
			"frozen_descriptor", "estimated_tts_duration_s", "shot_count",
		},
	},
	{
		name: "role_classifier",
		path: roleClassifierPromptPath,
		substitutes: []string{
			"scp_id", "beat_count", "beat_count_minus_one", "beat_table",
		},
	},
}

// TestPromptPlaceholders_AreFullyCovered asserts that every {placeholder}
// referenced in an LLM-driven prompt file has a matching substitution in
// its renderer (orphan placeholder → would leak literal "{xxx}" into the
// LLM call), AND that every substitution the renderer attempts is
// actually referenced by the prompt (stale renderer entry → dead code).
//
// Why this matters: prompts are edited as plain Markdown, often by hand,
// and the renderer is in a different file. Without this test, adding a
// new {variable} to a prompt is silently broken until someone notices the
// raw "{variable}" in production output. This test fails the build the
// moment the two sides drift.
func TestPromptPlaceholders_AreFullyCovered(t *testing.T) {
	root := testutil.ProjectRoot(t)
	for _, tc := range promptRendererContract {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, tc.path))
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}

			referenced := map[string]bool{}
			for _, match := range placeholderRE.FindAllString(string(raw), -1) {
				referenced[match[1:len(match)-1]] = true
			}
			substituted := map[string]bool{}
			for _, s := range tc.substitutes {
				substituted[s] = true
			}

			var orphans []string
			for name := range referenced {
				if !substituted[name] {
					orphans = append(orphans, name)
				}
			}
			sort.Strings(orphans)
			if len(orphans) > 0 {
				t.Errorf("%s: prompt references %d placeholder(s) not substituted by renderer: %v\n"+
					"  Either add them to the renderer's strings.NewReplacer call (see %s renderer), or remove them from %s.",
					tc.path, len(orphans), orphans, tc.name, tc.path)
			}

			var stale []string
			for name := range substituted {
				if !referenced[name] {
					stale = append(stale, name)
				}
			}
			sort.Strings(stale)
			if len(stale) > 0 {
				t.Errorf("%s: renderer substitutes %d placeholder(s) not referenced by prompt: %v\n"+
					"  Either add them to %s, or remove them from the renderer (and from promptRendererContract in this file).",
					tc.path, len(stale), stale, tc.path)
			}
		})
	}
}
