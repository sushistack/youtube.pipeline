package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const manifestRelPath = "testdata/golden/eval/manifest.json"

// Manifest is the single source of truth for Golden set metadata.
type Manifest struct {
	Version                  int        `json:"version"`
	NextIndex                int        `json:"next_index"`
	LastRefreshedAt          time.Time  `json:"last_refreshed_at"`
	LastSuccessfulRunAt      *time.Time `json:"last_successful_run_at,omitempty"`
	LastSuccessfulPromptHash string     `json:"last_successful_prompt_hash,omitempty"`
	LastReport               *Report    `json:"last_report,omitempty"`
	Pairs                    []PairEntry `json:"pairs"`
}

// PairEntry records a single positive/negative pair in the manifest.
type PairEntry struct {
	Index        int       `json:"index"`
	CreatedAt    time.Time `json:"created_at"`
	PositivePath string    `json:"positive_path"`
	NegativePath string    `json:"negative_path"`
}

func manifestPath(projectRoot string) string {
	return filepath.Join(projectRoot, manifestRelPath)
}

func loadManifest(projectRoot string) (Manifest, error) {
	path := manifestPath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return m, nil
}

func saveManifest(projectRoot string, m Manifest) error {
	data, err := marshalIndented(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath(projectRoot), data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// marshalIndented returns JSON with two-space indentation and trailing newline.
func marshalIndented(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
