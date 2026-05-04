package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

const (
	// v1ArchiveLastReportRelPath is the frozen pre-D5 last_report snapshot.
	// Captured during the D5 reshape so v1's last-cycle score remains
	// reproducible byte-identically without keeping the v1 critic stack alive.
	v1ArchiveLastReportRelPath = "testdata/golden/eval/v1/last_report.json"

	// v1ArchiveManifestRelPath is the frozen pre-D5 manifest snapshot.
	// Useful for inspecting the v1 prompt hash + sample list without git
	// archeology. Pre-D5 evaluator + prompt are NOT preserved beyond this.
	v1ArchiveManifestRelPath = "testdata/golden/eval/v1/manifest.json"
)

// LoadV1ArchiveReport returns the frozen v1 last-cycle Report. The intended
// use is byte-identical reproducibility of the v1 score for the v1→v2
// comparability claim — the spec acceptance criterion D5#4. The v1 sample
// stack and v1 critic prompt have been deleted by D1/D4, so live re-execution
// of v1 fixtures against the current evaluator is no longer meaningful;
// the archive is therefore a snapshot, not a live replay path.
//
// Returns domain.ErrValidation when the snapshot is missing, malformed, or
// would parse as a vacuously-empty Report (zero TotalNegative AND zero
// PairResults). The empty-Report guard keeps a corrupted/truncated snapshot
// from silently turning the v1→v2 comparability log into "v2 won by
// recall=+1.00" against an all-zero baseline. Mutating the returned Report
// has no effect on disk.
func LoadV1ArchiveReport(projectRoot string) (Report, error) {
	path := filepath.Join(projectRoot, v1ArchiveLastReportRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, fmt.Errorf("v1 archive: read %s: %w", path, domain.ErrValidation)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return Report{}, fmt.Errorf("v1 archive: parse %s: %w", path, domain.ErrValidation)
	}
	if r.TotalNegative == 0 && len(r.Pairs) == 0 {
		return Report{}, fmt.Errorf(
			"v1 archive: snapshot at %s carries zero pairs and zero negatives — refusing to use as comparability baseline: %w",
			path, domain.ErrValidation,
		)
	}
	return r, nil
}

// LoadV1ArchiveManifest returns the frozen v1 manifest snapshot. Used for
// surfacing the v1 prompt hash and pair count alongside v2 in comparability
// reports. Errors mirror LoadV1ArchiveReport.
func LoadV1ArchiveManifest(projectRoot string) (Manifest, error) {
	path := filepath.Join(projectRoot, v1ArchiveManifestRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("v1 archive: read %s: %w", path, domain.ErrValidation)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("v1 archive: parse %s: %w", path, domain.ErrValidation)
	}
	return m, nil
}
