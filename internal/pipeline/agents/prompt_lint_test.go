package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
	"github.com/sushistack/youtube.pipeline/prompts"
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
			"scp_id", "act_id", "monologue_rune_cap",
			"act_synopsis", "act_key_points", "prior_act_summary",
			"scp_visual_reference", "containment_constraints", "format_guide",
			"forbidden_terms_section", "glossary_section", "quality_feedback",
			"exemplar_scenes",
		},
	},
	{
		name: "writer_segmenter",
		path: segmenterPromptPath,
		substitutes: []string{
			"act_id", "act_mood", "act_key_points",
			"monologue", "monologue_rune_count",
			"scp_visual_reference", "fact_tag_catalog",
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
			"act_id", "act_mood", "monologue", "beats_table",
			"scp_visual_reference", "frozen_descriptor", "shot_count",
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

// TestPromptPlaceholders_DocsAndEmbeddedMirrorsAgree asserts that for every
// agent that has BOTH a docs/prompts/scenario/*.md source and a
// prompts/agents/*.tmpl embedded mirror, the two files reference the
// EXACT same set of `{placeholder}` tokens. Drift between the two would
// silently change runtime behavior depending on the `useTemplatePrompts`
// flag — a placeholder present in one but not the other leaks raw
// `{xxx}` text into the LLM call when the flag toggles.
func TestPromptPlaceholders_DocsAndEmbeddedMirrorsAgree(t *testing.T) {
	root := testutil.ProjectRoot(t)
	pairs := []struct {
		name      string
		docsPath  string
		agentName string
	}{
		{"writer", writerPromptPath, prompts.AgentScriptWriter},
		{"segmenter", segmenterPromptPath, prompts.AgentScriptSegmenter},
		{"visual_breakdowner", visualBreakdownPromptPath, prompts.AgentVisualBreakdowner},
	}
	for _, p := range pairs {
		p := p
		t.Run(p.name, func(t *testing.T) {
			docsRaw, err := os.ReadFile(filepath.Join(root, p.docsPath))
			if err != nil {
				t.Fatalf("read docs prompt %s: %v", p.docsPath, err)
			}
			embedded, err := prompts.ReadAgent(p.agentName)
			if err != nil {
				t.Fatalf("read embedded agent %s: %v", p.agentName, err)
			}

			docsTokens := map[string]bool{}
			for _, m := range placeholderRE.FindAllString(string(docsRaw), -1) {
				docsTokens[m[1:len(m)-1]] = true
			}
			embeddedTokens := map[string]bool{}
			for _, m := range placeholderRE.FindAllString(embedded, -1) {
				embeddedTokens[m[1:len(m)-1]] = true
			}

			var docsOnly, embeddedOnly []string
			for name := range docsTokens {
				if !embeddedTokens[name] {
					docsOnly = append(docsOnly, name)
				}
			}
			for name := range embeddedTokens {
				if !docsTokens[name] {
					embeddedOnly = append(embeddedOnly, name)
				}
			}
			sort.Strings(docsOnly)
			sort.Strings(embeddedOnly)

			if len(docsOnly) > 0 || len(embeddedOnly) > 0 {
				t.Errorf("%s: docs vs embedded mirror placeholder drift\n"+
					"  docs-only (%d): %v\n"+
					"  embedded-only (%d): %v\n"+
					"  Sync %s with %s — runtime behavior depends on useTemplatePrompts flag.",
					p.name, len(docsOnly), docsOnly, len(embeddedOnly), embeddedOnly,
					p.docsPath, "prompts/agents/"+p.agentName+".tmpl")
			}
		})
	}
}
