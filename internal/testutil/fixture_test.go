package testutil

import (
	"encoding/json"
	"testing"
)

func TestLoadFixture_ContractFile(t *testing.T) {
	data := LoadFixture(t, "contracts/pipeline_state.json")
	if len(data) == 0 {
		t.Fatal("expected non-empty fixture data")
	}
	// Verify it's valid JSON
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	if m["id"] != "scp-049-run-1" {
		t.Errorf("id = %v, want scp-049-run-1", m["id"])
	}
}

func TestLoadFixture_NotFound(t *testing.T) {
	ft := &fakeT{}
	LoadFixture(ft, "nonexistent/file.json")
	if len(ft.fatals) == 0 {
		t.Error("expected fatal for missing fixture file")
	}
}

func TestLoadRunStateFixture_QueryRun(t *testing.T) {
	testDB := LoadRunStateFixture(t, "paused_at_batch_review")

	var stage, status string
	err := testDB.QueryRow("SELECT stage, status FROM runs WHERE id = 'scp-049-run-1'").Scan(&stage, &status)
	if err != nil {
		t.Fatalf("query run: %v", err)
	}
	if stage != "batch_review" {
		t.Errorf("stage = %s, want batch_review", stage)
	}
	if status != "waiting" {
		t.Errorf("status = %s, want waiting", status)
	}
}

func TestLoadRunStateFixture_QuerySegments(t *testing.T) {
	testDB := LoadRunStateFixture(t, "paused_at_batch_review")

	var count int
	err := testDB.QueryRow("SELECT COUNT(*) FROM segments WHERE run_id = 'scp-049-run-1'").Scan(&count)
	if err != nil {
		t.Fatalf("query segments: %v", err)
	}
	if count != 3 {
		t.Errorf("segment count = %d, want 3", count)
	}
}

func TestLoadRunStateFixture_QueryDecisions(t *testing.T) {
	testDB := LoadRunStateFixture(t, "paused_at_batch_review")

	var count int
	err := testDB.QueryRow("SELECT COUNT(*) FROM decisions WHERE run_id = 'scp-049-run-1'").Scan(&count)
	if err != nil {
		t.Fatalf("query decisions: %v", err)
	}
	if count != 2 {
		t.Errorf("decision count = %d, want 2", count)
	}
}
