package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

const (
	ExportTypeDecisions = "decisions"
	ExportTypeArtifacts = "artifacts"

	ExportFormatJSON = "json"
	ExportFormatCSV  = "csv"
)

type ExportRunStore interface {
	GetExportRecord(ctx context.Context, id string) (*db.ExportRunRecord, error)
}

type ExportDecisionStore interface {
	ListByRunIDForExport(ctx context.Context, runID string) ([]*db.ExportDecisionRow, error)
}

type ExportSegmentStore interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
}

type ExportRequest struct {
	RunID  string
	Type   string
	Format string
}

type ExportResult struct {
	RunID   string `json:"run_id"`
	Type    string `json:"type"`
	Format  string `json:"format"`
	Path    string `json:"path"`
	Records int    `json:"records"`
}

type ExportEnvelope struct {
	Version int `json:"version"`
	Data    any `json:"data"`
}

// ExportDecision is the stable per-row shape for the decisions export. Pointer
// fields are serialized as explicit JSON null when nil — omitempty is avoided
// so the envelope shape is identical for every row regardless of NULLs in the
// source table. CSV rendering maps nil → empty string.
type ExportDecision struct {
	DecisionID   int64   `json:"decision_id"`
	RunID        string  `json:"run_id"`
	SceneID      *string `json:"scene_id"`
	TargetItem   string  `json:"target_item"`
	DecisionType string  `json:"decision_type"`
	CreatedAt    string  `json:"created_at"`
	Note         *string `json:"note"`
	SupersededBy *int64  `json:"superseded_by"`
}

type ExportArtifact struct {
	ArtifactType string `json:"artifact_type"`
	SceneIndex   *int   `json:"scene_index,omitempty"`
	ShotIndex    *int   `json:"shot_index,omitempty"`
	Path         string `json:"path"`
}

type ExportService struct {
	runs      ExportRunStore
	decisions ExportDecisionStore
	segments  ExportSegmentStore
	outputDir string
}

func NewExportService(
	runs ExportRunStore,
	decisions ExportDecisionStore,
	segments ExportSegmentStore,
	outputDir string,
) *ExportService {
	return &ExportService{
		runs:      runs,
		decisions: decisions,
		segments:  segments,
		outputDir: outputDir,
	}
}

func (s *ExportService) Export(ctx context.Context, req ExportRequest) (*ExportResult, error) {
	if strings.TrimSpace(req.RunID) == "" {
		return nil, fmt.Errorf("export: --run-id is required: %w", domain.ErrValidation)
	}
	if req.Type != ExportTypeDecisions && req.Type != ExportTypeArtifacts {
		return nil, fmt.Errorf("export: invalid --type %q: %w", req.Type, domain.ErrValidation)
	}
	if req.Format == "" {
		req.Format = ExportFormatJSON
	}
	if req.Format != ExportFormatJSON && req.Format != ExportFormatCSV {
		return nil, fmt.Errorf("export: invalid --format %q: %w", req.Format, domain.ErrValidation)
	}

	runDir, exportDir, err := safeExportDirs(s.outputDir, req.RunID)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}

	run, err := s.runs.GetExportRecord(ctx, req.RunID)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}

	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return nil, fmt.Errorf("export: create export dir: %w", err)
	}

	switch req.Type {
	case ExportTypeDecisions:
		rows, err := s.buildDecisionRows(ctx, req.RunID)
		if err != nil {
			return nil, err
		}
		filename := filepath.Join(exportDir, "decisions."+req.Format)
		if err := writeDecisionExport(filename, req.Format, rows); err != nil {
			return nil, err
		}
		return &ExportResult{RunID: req.RunID, Type: req.Type, Format: req.Format, Path: filename, Records: len(rows)}, nil
	case ExportTypeArtifacts:
		rows, err := s.buildArtifactRows(ctx, req.RunID, runDir, run)
		if err != nil {
			return nil, err
		}
		filename := filepath.Join(exportDir, "artifacts."+req.Format)
		if err := writeArtifactExport(filename, req.Format, rows); err != nil {
			return nil, err
		}
		return &ExportResult{RunID: req.RunID, Type: req.Type, Format: req.Format, Path: filename, Records: len(rows)}, nil
	default:
		return nil, fmt.Errorf("export: invalid --type %q: %w", req.Type, domain.ErrValidation)
	}
}

func (s *ExportService) buildDecisionRows(ctx context.Context, runID string) ([]ExportDecision, error) {
	rows, err := s.decisions.ListByRunIDForExport(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("export decisions for %s: %w", runID, err)
	}
	out := make([]ExportDecision, len(rows))
	for i, row := range rows {
		targetItem := "run"
		if row.SceneID != nil {
			targetItem = "scene:" + *row.SceneID
		}
		out[i] = ExportDecision{
			DecisionID:   row.ID,
			RunID:        row.RunID,
			SceneID:      row.SceneID,
			TargetItem:   targetItem,
			DecisionType: row.DecisionType,
			CreatedAt:    row.CreatedAt,
			Note:         row.Note,
			SupersededBy: row.SupersededBy,
		}
	}
	return out, nil
}

func (s *ExportService) buildArtifactRows(ctx context.Context, runID, runDir string, run *db.ExportRunRecord) ([]ExportArtifact, error) {
	segments, err := s.segments.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("export artifacts for %s: %w", runID, err)
	}

	out := make([]ExportArtifact, 0, 8)
	appendPath := func(artifactType string, sceneIndex, shotIndex *int, raw string) error {
		rel, ok, err := normalizeRunRelativePath(runDir, raw)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		out = append(out, ExportArtifact{
			ArtifactType: artifactType,
			SceneIndex:   sceneIndex,
			ShotIndex:    shotIndex,
			Path:         rel,
		})
		return nil
	}

	if run.ScenarioPath != nil {
		if err := appendPath("scenario", nil, nil, *run.ScenarioPath); err != nil {
			return nil, fmt.Errorf("export artifacts for %s: %w", runID, err)
		}
	}
	if run.OutputPath != nil {
		if err := appendPath("output", nil, nil, *run.OutputPath); err != nil {
			return nil, fmt.Errorf("export artifacts for %s: %w", runID, err)
		}
	}
	for _, name := range []string{"metadata.json", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err == nil {
			artifactType := strings.TrimSuffix(name, ".json")
			if err := appendPath(artifactType, nil, nil, name); err != nil {
				return nil, fmt.Errorf("export artifacts for %s: %w", runID, err)
			}
		}
	}

	for _, segment := range segments {
		sceneIndex := segment.SceneIndex
		if segment.TTSPath != nil {
			if err := appendPath("tts", &sceneIndex, nil, *segment.TTSPath); err != nil {
				return nil, fmt.Errorf("export artifacts for %s: %w", runID, err)
			}
		}
		if segment.ClipPath != nil {
			if err := appendPath("clip", &sceneIndex, nil, *segment.ClipPath); err != nil {
				return nil, fmt.Errorf("export artifacts for %s: %w", runID, err)
			}
		}
		for i, shot := range segment.Shots {
			shotIndex := i
			if err := appendPath("image", &sceneIndex, &shotIndex, shot.ImagePath); err != nil {
				return nil, fmt.Errorf("export artifacts for %s: %w", runID, err)
			}
		}
	}

	return out, nil
}

func safeExportDirs(outputDir, runID string) (string, string, error) {
	if outputDir == "" {
		return "", "", fmt.Errorf("output_dir is empty: %w", domain.ErrValidation)
	}
	if !isSafeRunID(runID) {
		return "", "", fmt.Errorf("invalid run_id %q: %w", runID, domain.ErrValidation)
	}
	base := filepath.Clean(outputDir)
	runDir := filepath.Join(base, runID)
	exportDir := filepath.Join(runDir, "export")
	return runDir, exportDir, nil
}

// isSafeRunID rejects values that would escape or collapse the per-run export
// directory. The guard excludes: empty strings, ".", "..", any leading dot
// (which would let the export land in the top-level output dir or a hidden
// subdirectory), path separators in either slash convention, and null bytes
// (which SQLite accepts in TEXT but the filesystem layer treats inconsistently
// across OSes).
func isSafeRunID(runID string) bool {
	if runID == "" || runID == "." || runID == ".." {
		return false
	}
	if runID[0] == '.' {
		return false
	}
	if strings.Contains(runID, "..") {
		return false
	}
	if strings.ContainsAny(runID, `/\`) {
		return false
	}
	if strings.ContainsRune(runID, 0) {
		return false
	}
	return true
}

func normalizeRunRelativePath(runDir, raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, nil
	}
	runDir = filepath.Clean(runDir)

	if filepath.IsAbs(raw) {
		rel, err := filepath.Rel(runDir, filepath.Clean(raw))
		if err != nil {
			return "", false, fmt.Errorf("make path relative: %w", err)
		}
		if rel == "." || rel == "" {
			return "", false, nil
		}
		if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return "", false, fmt.Errorf("path %q escapes run directory: %w", raw, domain.ErrValidation)
		}
		return filepath.ToSlash(rel), true, nil
	}

	clean := filepath.Clean(raw)
	if clean == "." || clean == "" {
		return "", false, nil
	}
	prefix := filepath.Base(runDir) + string(filepath.Separator)
	if strings.HasPrefix(clean, prefix) {
		clean = strings.TrimPrefix(clean, prefix)
	}
	joined := filepath.Join(runDir, clean)
	rel, err := filepath.Rel(runDir, joined)
	if err != nil {
		return "", false, fmt.Errorf("make path relative: %w", err)
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", false, fmt.Errorf("path %q escapes run directory: %w", raw, domain.ErrValidation)
	}
	return filepath.ToSlash(rel), true, nil
}

func writeDecisionExport(path, format string, rows []ExportDecision) error {
	switch format {
	case ExportFormatJSON:
		return writeJSONFile(path, rows)
	case ExportFormatCSV:
		records := make([][]string, 0, len(rows)+1)
		records = append(records, []string{
			"decision_id", "run_id", "scene_id", "target_item", "decision_type", "created_at", "note", "superseded_by",
		})
		for _, row := range rows {
			sceneID := ""
			if row.SceneID != nil {
				sceneID = *row.SceneID
			}
			note := ""
			if row.Note != nil {
				note = *row.Note
			}
			supersededBy := ""
			if row.SupersededBy != nil {
				supersededBy = fmt.Sprintf("%d", *row.SupersededBy)
			}
			records = append(records, []string{
				fmt.Sprintf("%d", row.DecisionID),
				row.RunID,
				sceneID,
				row.TargetItem,
				row.DecisionType,
				row.CreatedAt,
				note,
				supersededBy,
			})
		}
		return writeCSVFile(path, records)
	default:
		return fmt.Errorf("write decisions export: invalid format %q: %w", format, domain.ErrValidation)
	}
}

func writeArtifactExport(path, format string, rows []ExportArtifact) error {
	switch format {
	case ExportFormatJSON:
		return writeJSONFile(path, rows)
	case ExportFormatCSV:
		records := make([][]string, 0, len(rows)+1)
		records = append(records, []string{"artifact_type", "scene_index", "shot_index", "path"})
		for _, row := range rows {
			sceneIndex := ""
			if row.SceneIndex != nil {
				sceneIndex = fmt.Sprintf("%d", *row.SceneIndex)
			}
			shotIndex := ""
			if row.ShotIndex != nil {
				shotIndex = fmt.Sprintf("%d", *row.ShotIndex)
			}
			records = append(records, []string{row.ArtifactType, sceneIndex, shotIndex, row.Path})
		}
		return writeCSVFile(path, records)
	default:
		return fmt.Errorf("write artifacts export: invalid format %q: %w", format, domain.ErrValidation)
	}
}

func writeJSONFile(path string, rows any) error {
	payload, err := json.MarshalIndent(ExportEnvelope{
		Version: 1,
		Data:    rows,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("write export json %s: marshal: %w", path, err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write export json %s: %w", path, err)
	}
	return nil
}

func writeCSVFile(path string, records [][]string) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("write export csv %s: %w", path, err)
	}
	defer func() {
		// Surface a Close error (flush failure, NFS disconnect, etc.) only
		// when the write path itself succeeded; a prior write error is the
		// more informative signal and must not be shadowed.
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("write export csv %s: %w", path, cerr)
		}
	}()

	safe := make([][]string, len(records))
	for i, row := range records {
		cells := make([]string, len(row))
		for j, cell := range row {
			cells[j] = csvCellSafe(cell)
		}
		safe[i] = cells
	}

	w := csv.NewWriter(file)
	if werr := w.WriteAll(safe); werr != nil {
		return fmt.Errorf("write export csv %s: %w", path, werr)
	}
	return nil
}

// csvCellSafe neutralizes CSV formula injection (OWASP) by prefixing cells
// that start with a spreadsheet-evaluated leading character. Exported CSVs
// are routinely opened in Excel / Sheets / LibreOffice, where cells like
// `=HYPERLINK(...)` or `+cmd|...` would otherwise execute as formulas.
func csvCellSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
