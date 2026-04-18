package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// PairMeta describes a successfully added pair.
type PairMeta struct {
	Index        int
	CreatedAt    time.Time
	PositivePath string
	NegativePath string
}

// AddPair validates both candidate files, writes them into a new indexed
// pair directory under testdata/golden/eval/, and updates manifest.json.
// On any failure after the pair directory is created, the partially-written
// directory is removed so no orphan positive/negative files remain on disk.
func AddPair(projectRoot string, positiveSrc string, negativeSrc string, now time.Time) (PairMeta, error) {
	if positiveSrc == "" {
		return PairMeta{}, fmt.Errorf("positive fixture path required: %w", domain.ErrValidation)
	}
	if negativeSrc == "" {
		return PairMeta{}, fmt.Errorf("negative fixture path required: %w", domain.ErrValidation)
	}

	posData, err := readAndValidate(projectRoot, positiveSrc, "positive")
	if err != nil {
		return PairMeta{}, fmt.Errorf("positive fixture: %w", err)
	}
	negData, err := readAndValidate(projectRoot, negativeSrc, "negative")
	if err != nil {
		return PairMeta{}, fmt.Errorf("negative fixture: %w", err)
	}

	m, err := loadManifest(projectRoot)
	if err != nil {
		return PairMeta{}, fmt.Errorf("load manifest: %w", err)
	}

	idx := m.NextIndex
	dirName := fmt.Sprintf("%06d", idx)
	pairDir := filepath.Join(projectRoot, "testdata", "golden", "eval", dirName)
	if err := os.MkdirAll(pairDir, 0o755); err != nil {
		return PairMeta{}, fmt.Errorf("create pair directory: %w", err)
	}

	// Any error after MkdirAll must remove the pair directory so a half-written
	// pair (orphan positive.json, or a pair whose manifest update failed) does
	// not leak to disk. Success path clears this by setting committed = true.
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(pairDir)
		}
	}()

	posRelPath := filepath.Join("eval", dirName, "positive.json")
	negRelPath := filepath.Join("eval", dirName, "negative.json")

	if err := os.WriteFile(filepath.Join(pairDir, "positive.json"), posData, 0o644); err != nil {
		return PairMeta{}, fmt.Errorf("write positive fixture: %w", err)
	}
	if err := os.WriteFile(filepath.Join(pairDir, "negative.json"), negData, 0o644); err != nil {
		return PairMeta{}, fmt.Errorf("write negative fixture: %w", err)
	}

	entry := PairEntry{
		Index:        idx,
		CreatedAt:    now.UTC().Truncate(time.Second),
		PositivePath: posRelPath,
		NegativePath: negRelPath,
	}
	m.Pairs = append(m.Pairs, entry)
	m.NextIndex = idx + 1
	m.LastRefreshedAt = now.UTC().Truncate(time.Second)

	if err := ValidateBalancedSet(m); err != nil {
		return PairMeta{}, err
	}
	if err := saveManifest(projectRoot, m); err != nil {
		return PairMeta{}, fmt.Errorf("save manifest: %w", err)
	}
	committed = true

	return PairMeta{
		Index:        idx,
		CreatedAt:    entry.CreatedAt,
		PositivePath: posRelPath,
		NegativePath: negRelPath,
	}, nil
}

// ListPairs returns all pairs from the manifest in ascending index order.
func ListPairs(projectRoot string) ([]PairMeta, error) {
	m, err := loadManifest(projectRoot)
	if err != nil {
		return nil, err
	}
	pairs := make([]PairMeta, len(m.Pairs))
	for i, e := range m.Pairs {
		pairs[i] = PairMeta{
			Index:        e.Index,
			CreatedAt:    e.CreatedAt,
			PositivePath: e.PositivePath,
			NegativePath: e.NegativePath,
		}
	}
	return pairs, nil
}

// ValidateBalancedSet checks that every manifest entry has exactly one
// positive path and one negative path. File existence is not verified here;
// use ValidateBalancedSetOnDisk when you need to catch manual manifest edits
// or filesystem drift.
func ValidateBalancedSet(m Manifest) error {
	for _, p := range m.Pairs {
		if p.PositivePath == "" {
			return fmt.Errorf("pair %d missing positive_path: %w", p.Index, domain.ErrValidation)
		}
		if p.NegativePath == "" {
			return fmt.Errorf("pair %d missing negative_path: %w", p.Index, domain.ErrValidation)
		}
	}
	return nil
}

// ValidateBalancedSetOnDisk is the stricter variant: in addition to the
// manifest-level checks, it stats both target files for every pair and
// rejects entries whose files are missing. Use this for drift detection
// against hand-edited manifests.
func ValidateBalancedSetOnDisk(projectRoot string, m Manifest) error {
	if err := ValidateBalancedSet(m); err != nil {
		return err
	}
	for _, p := range m.Pairs {
		posAbs := filepath.Join(projectRoot, "testdata", "golden", p.PositivePath)
		if _, err := os.Stat(posAbs); err != nil {
			return fmt.Errorf("pair %d positive file missing (%s): %w", p.Index, p.PositivePath, domain.ErrValidation)
		}
		negAbs := filepath.Join(projectRoot, "testdata", "golden", p.NegativePath)
		if _, err := os.Stat(negAbs); err != nil {
			return fmt.Errorf("pair %d negative file missing (%s): %w", p.Index, p.NegativePath, domain.ErrValidation)
		}
	}
	return nil
}

// readAndValidate reads a fixture source file, validates it, and returns the
// re-marshaled JSON with stable indentation.
func readAndValidate(projectRoot, srcPath, expectedKind string) ([]byte, error) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", domain.ErrValidation)
	}
	fixture, err := ValidateFixture(projectRoot, raw)
	if err != nil {
		return nil, err
	}
	if fixture.Kind != expectedKind {
		return nil, fmt.Errorf("expected kind %q but got %q: %w", expectedKind, fixture.Kind, domain.ErrValidation)
	}
	data, err := marshalIndented(fixture)
	if err != nil {
		return nil, fmt.Errorf("marshal fixture: %w", domain.ErrValidation)
	}
	return data, nil
}
