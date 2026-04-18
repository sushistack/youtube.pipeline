package agents

import (
	"context"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestNoopAgent_ReturnsNil(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	agent := NoopAgent()
	if agent == nil {
		t.Fatal("NoopAgent() returned nil AgentFunc")
	}

	if err := agent(context.Background(), &PipelineState{RunID: "r", SCPID: "s"}); err != nil {
		t.Fatalf("NoopAgent returned error: %v", err)
	}
}

func TestNoopAgent_IndependentInstances(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Each call returns a fresh closure; tests may rely on identity-free
	// semantics when using NoopAgent as a spy placeholder.
	a := NoopAgent()
	b := NoopAgent()
	if a == nil || b == nil {
		t.Fatal("NoopAgent() returned nil")
	}
}
