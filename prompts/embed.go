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

// EnvFlag is the env variable that toggles the new templates on for a
// given agent run. Read by callers (writer, future agents) to decide
// whether to load the embedded template instead of the legacy markdown.
const EnvFlag = "USE_TEMPLATE_PROMPTS"

// EnvOn is the literal value EnvFlag must hold to opt in.
const EnvOn = "true"

// AgentScriptWriter is the canonical name for the script-writing agent
// template. Constants live here so that callers don't sprinkle string
// literals; renaming a template file becomes a single-source change.
const AgentScriptWriter = "script_writer"

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
