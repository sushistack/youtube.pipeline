package prompts_test

import (
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/prompts"
)

func TestReadAgentScriptWriter(t *testing.T) {
	t.Parallel()
	body, err := prompts.ReadAgent(prompts.AgentScriptWriter)
	if err != nil {
		t.Fatal(err)
	}
	if body == "" {
		t.Fatal("template body empty")
	}
	required := []string{
		"{scp_id}",
		"{act_id}",
		"{scene_num_range}",
		"{scene_budget}",
		"{format_guide}",
		"{forbidden_terms_section}",
		"{quality_feedback}",
		"Cold-open hook within 15 seconds",
		"Twist position 70–85%",
		"Unresolved outro",
	}
	for _, r := range required {
		if !strings.Contains(body, r) {
			t.Errorf("template missing required marker %q", r)
		}
	}
}

func TestReadAgentMissing(t *testing.T) {
	t.Parallel()
	_, err := prompts.ReadAgent("does_not_exist")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestAgentNames(t *testing.T) {
	t.Parallel()
	names, err := prompts.AgentNames()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range names {
		if n == prompts.AgentScriptWriter {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("script_writer not in AgentNames(): %v", names)
	}
}
