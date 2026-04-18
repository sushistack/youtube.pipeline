package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFRCoverageJSON_ValidStructure(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fr-coverage.json")
	if err != nil {
		t.Fatalf("read fr-coverage.json: %v", err)
	}

	var fc frCoverage
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("unmarshal fr-coverage.json: %v", err)
	}

	if fc.Meta.TotalFRs == 0 {
		t.Error("meta.total_frs should be > 0")
	}
	if len(fc.Coverage) == 0 {
		t.Error("coverage should have at least one entry")
	}
}

func TestFRCoverageJSON_GraceModeEnabled(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fr-coverage.json")
	if err != nil {
		t.Fatalf("read fr-coverage.json: %v", err)
	}

	var fc frCoverage
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !fc.Meta.Grace {
		t.Error("grace mode should be true")
	}
}

func TestFRCoverageJSON_AnnotatedCountWithin25Percent(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fr-coverage.json")
	if err != nil {
		t.Fatalf("read fr-coverage.json: %v", err)
	}

	var fc frCoverage
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	annotated := 0
	for _, c := range fc.Coverage {
		if c.Annotation != nil && *c.Annotation != "" {
			annotated++
		}
	}

	maxAnnotated := fc.Meta.TotalFRs * 25 / 100
	if annotated > maxAnnotated {
		t.Errorf("annotated FRs (%d) exceed 25%% of total (%d); max: %d", annotated, fc.Meta.TotalFRs, maxAnnotated)
	}
}

func TestFRCoverageJSON_AllEntriesHaveFRID(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fr-coverage.json")
	if err != nil {
		t.Fatalf("read fr-coverage.json: %v", err)
	}

	var fc frCoverage
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for i, c := range fc.Coverage {
		if c.FRID == "" {
			t.Errorf("coverage[%d] missing fr_id", i)
		}
	}
}

func TestFRCoverageJSON_NoDuplicateFRIDs(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fr-coverage.json")
	if err != nil {
		t.Fatalf("read fr-coverage.json: %v", err)
	}

	var fc frCoverage
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	seen := make(map[string]bool)
	for _, c := range fc.Coverage {
		if seen[c.FRID] {
			t.Errorf("duplicate fr_id: %s", c.FRID)
		}
		seen[c.FRID] = true
	}
}
