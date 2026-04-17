package testutil

import (
	"encoding/json"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

func TestContract_PipelineState(t *testing.T) {
	data := LoadFixture(t, "contracts/pipeline_state.json")

	var run domain.Run
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatalf("unmarshal pipeline_state.json into domain.Run: %v", err)
	}

	// Validate required fields are populated
	if run.ID != "scp-049-run-1" {
		t.Errorf("ID = %q, want %q", run.ID, "scp-049-run-1")
	}
	if run.SCPID != "049" {
		t.Errorf("SCPID = %q, want %q", run.SCPID, "049")
	}
	if run.Stage != domain.StagePending {
		t.Errorf("Stage = %q, want %q", run.Stage, domain.StagePending)
	}
	if run.Status != domain.StatusPending {
		t.Errorf("Status = %q, want %q", run.Status, domain.StatusPending)
	}
	if run.CreatedAt == "" {
		t.Error("CreatedAt should not be empty")
	}
	if run.UpdatedAt == "" {
		t.Error("UpdatedAt should not be empty")
	}

	// Validate zero-value numeric and boolean fields
	if run.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0", run.RetryCount)
	}
	if run.CostUSD != 0.0 {
		t.Errorf("CostUSD = %f, want 0.0", run.CostUSD)
	}
	if run.TokenIn != 0 {
		t.Errorf("TokenIn = %d, want 0", run.TokenIn)
	}
	if run.TokenOut != 0 {
		t.Errorf("TokenOut = %d, want 0", run.TokenOut)
	}
	if run.DurationMs != 0 {
		t.Errorf("DurationMs = %d, want 0", run.DurationMs)
	}
	if run.HumanOverride != false {
		t.Errorf("HumanOverride = %v, want false", run.HumanOverride)
	}

	// Nullable fields should be nil when omitted from JSON
	if run.RetryReason != nil {
		t.Errorf("RetryReason should be nil, got %v", *run.RetryReason)
	}
	if run.CriticScore != nil {
		t.Errorf("CriticScore should be nil, got %v", *run.CriticScore)
	}
	if run.ScenarioPath != nil {
		t.Errorf("ScenarioPath should be nil, got %v", *run.ScenarioPath)
	}

	// Round-trip: re-marshal and verify no data loss
	remarshaled, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var run2 domain.Run
	if err := json.Unmarshal(remarshaled, &run2); err != nil {
		t.Fatalf("unmarshal round-trip: %v", err)
	}
	if run.ID != run2.ID || run.Stage != run2.Stage || run.CostUSD != run2.CostUSD {
		t.Error("round-trip mismatch")
	}
}
