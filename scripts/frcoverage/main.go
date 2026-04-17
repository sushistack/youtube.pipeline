// check-fr-coverage.go validates testdata/fr-coverage.json:
// (a) mapped test_ids resolve to existing Test* functions,
// (b) annotated FRs do not exceed 15% of total.
//
// In grace mode (meta.grace == true), unmapped FRs produce warnings, not failures.
// TODO: Switch to strict mode after Epic 6 routes exist
//
// Usage: go run scripts/check-fr-coverage.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type meta struct {
	Grace       bool   `json:"grace"`
	TotalFRs    int    `json:"total_frs"`
	LastUpdated string `json:"last_updated"`
}

type coverage struct {
	FRID       string   `json:"fr_id"`
	TestIDs    []string `json:"test_ids"`
	Annotation *string  `json:"annotation"`
}

type frCoverage struct {
	Meta     meta       `json:"meta"`
	Coverage []coverage `json:"coverage"`
}

func main() {
	exitCode := run()
	os.Exit(exitCode)
}

func run() int {
	data, err := os.ReadFile("testdata/fr-coverage.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot read testdata/fr-coverage.json: %v\n", err)
		return 1
	}

	var fc frCoverage
	if err := json.Unmarshal(data, &fc); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: malformed JSON: %v\n", err)
		return 1
	}

	// Discover all existing test function names
	existingTests, err := listTestFunctions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot list test functions: %v\n", err)
		return 1
	}

	hasError := false
	annotatedCount := 0
	mappedFRs := make(map[string]bool)

	for _, c := range fc.Coverage {
		mappedFRs[c.FRID] = true

		if c.Annotation != nil && *c.Annotation != "" {
			annotatedCount++
		}

		// Validate that test_ids resolve to real functions
		for _, testID := range c.TestIDs {
			if !existingTests[testID] {
				fmt.Printf("ERROR: %s references non-existent test function: %s\n", c.FRID, testID)
				hasError = true
			}
		}
	}

	// Check annotated FR count <= 15%
	maxAnnotated := fc.Meta.TotalFRs * 15 / 100
	if annotatedCount > maxAnnotated {
		fmt.Printf("ERROR: annotated FRs (%d) exceed 15%% of total (%d); max allowed: %d\n",
			annotatedCount, fc.Meta.TotalFRs, maxAnnotated)
		hasError = true
	}

	// Grace mode: warn for unmapped FRs
	mappedCount := len(fc.Coverage)
	unmappedCount := fc.Meta.TotalFRs - mappedCount
	if unmappedCount > 0 {
		if fc.Meta.Grace {
			fmt.Printf("WARN: %d of %d FRs are not yet mapped in fr-coverage.json (grace mode)\n",
				unmappedCount, fc.Meta.TotalFRs)
		} else {
			fmt.Printf("ERROR: %d of %d FRs are not mapped in fr-coverage.json (strict mode)\n",
				unmappedCount, fc.Meta.TotalFRs)
			hasError = true
		}
	}

	if hasError {
		fmt.Println("\nfr-coverage check: FAILED")
		return 1
	}

	fmt.Printf("fr-coverage check: OK (%d FRs mapped, %d annotated, %d unmapped)\n",
		mappedCount, annotatedCount, unmappedCount)
	return 0
}

func listTestFunctions() (map[string]bool, error) {
	tests := make(map[string]bool)

	// List Go test functions
	goTests, err := listGoTests()
	if err != nil {
		return nil, err
	}
	for _, t := range goTests {
		tests[t] = true
	}

	return tests, nil
}

func listGoTests() ([]string, error) {
	cmd := exec.Command("go", "test", "-list", ".*", "./cmd/...", "./internal/...", "./migrations/...")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go test -list: %w\n%s", err, out)
	}

	var tests []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Test") {
			tests = append(tests, line)
		}
	}
	return tests, nil
}
