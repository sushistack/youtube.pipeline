// Package prompts is the embedded source-of-truth for v2 agent prompt
// templates. The legacy markdown prompts under docs/prompts/scenario/
// are still loaded by internal/pipeline/agents/assets.go via os.ReadFile;
// this package adds the SCP-Explained-aligned templates required by spec
// section 7 of next-session-enhance-prompts.md.
//
// The package lives at module root (not under internal/) intentionally:
// Go's embed directive cannot reference files above the embedding
// package's directory, so the templates and the loader must share a
// parent. Top-level non-internal packages are exempt from the layer
// linter (scripts/lintlayers/main.go ignores non-internal imports), so
// agents/ can consume this without a layer rule.
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed agents/*.tmpl
var agentTemplates embed.FS

// AgentScriptWriter is the canonical name for the script-writing agent
// template. Constants live here so that callers don't sprinkle string
// literals; renaming a template file becomes a single-source change.
//
// The toggle for using the embedded v2 template lives in
// domain.PipelineConfig.UseTemplatePrompts (config.yaml), not in env —
// 1-operator pipeline avoids env-only toggles per
// memory/feedback_config_not_env.md.
const AgentScriptWriter = "script_writer"

// AgentScriptSegmenter is the canonical name for the v2 stage-2 beat
// segmentation template. Stage 1 (script_writer) produces the act
// monologue; this template is rendered with the just-written monologue
// + offsets contract so the LLM emits BeatAnchor[] slices.
const AgentScriptSegmenter = "script_segmenter"

// AgentVisualBreakdowner is the canonical name for the v2 visual
// breakdowner template. Consumes one act's monologue + ordered
// BeatAnchor slices, emits one VisualShot per beat with the
// narration_anchor block carrying every BeatAnchor field byte-for-byte.
const AgentVisualBreakdowner = "visual_breakdowner"

// ReadAgent returns the embedded template body for the given agent name.
// Returns an error wrapped with the agent name when the template is
// missing — the caller can decide whether to fall back to the legacy
// path or fail closed.
func ReadAgent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("prompts: empty agent name")
	}
	body, err := fs.ReadFile(agentTemplates, "agents/"+name+".tmpl")
	if err != nil {
		return "", fmt.Errorf("prompts: read agent %q: %w", name, err)
	}
	return string(body), nil
}

// AgentNames lists every agent template currently embedded. Useful for
// startup smoke tests and CLI introspection (`pipeline doctor`).
func AgentNames() ([]string, error) {
	entries, err := fs.ReadDir(agentTemplates, "agents")
	if err != nil {
		return nil, fmt.Errorf("prompts: list agents: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) > len(".tmpl") && n[len(n)-len(".tmpl"):] == ".tmpl" {
			names = append(names, n[:len(n)-len(".tmpl")])
		}
	}
	return names, nil
}
