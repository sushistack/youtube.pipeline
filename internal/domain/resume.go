package domain

import (
	"fmt"
	"strings"
)

// Mismatch describes a single filesystem↔DB discrepancy observed at resume entry.
type Mismatch struct {
	// Kind identifies the category: "missing_file", "orphan_segment",
	// "unexpected_scenario_json", "run_directory_missing", "suspicious_path".
	Kind string `json:"kind"`
	// Path is the filesystem path (absolute or runDir-relative) involved.
	Path string `json:"path"`
	// Expected describes what the DB expected to find.
	Expected string `json:"expected,omitempty"`
	// Detail carries additional context.
	Detail string `json:"detail,omitempty"`
}

// String renders a Mismatch as `kind@path`, the compact form used by
// InconsistencyReport.Error() and the API `warnings` field.
func (m Mismatch) String() string {
	return fmt.Sprintf("%s@%s", m.Kind, m.Path)
}

// InconsistencyReport aggregates all filesystem↔DB mismatches found for a run.
// A report with zero mismatches is still a valid, non-nil report (callers check len).
type InconsistencyReport struct {
	RunID      string     `json:"run_id"`
	Stage      Stage      `json:"stage"`
	Mismatches []Mismatch `json:"mismatches"`
}

// Error renders the report as a single-line human-readable summary.
// It is safe to embed into a fmt.Errorf chain together with ErrValidation.
func (r InconsistencyReport) Error() string {
	if len(r.Mismatches) == 0 {
		return fmt.Sprintf("fs/db consistent for run %s (stage %s)", r.RunID, r.Stage)
	}
	parts := make([]string, 0, len(r.Mismatches))
	for _, m := range r.Mismatches {
		parts = append(parts, fmt.Sprintf("%s@%s", m.Kind, m.Path))
	}
	return fmt.Sprintf("fs/db inconsistency for run %s at stage %s: %s",
		r.RunID, r.Stage, strings.Join(parts, ", "))
}
