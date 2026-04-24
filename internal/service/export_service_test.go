package service_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func seedExportFixtures(t *testing.T, outputDir string) (*db.RunStore, *db.DecisionStore, *db.SegmentStore, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	decisionStore := db.NewDecisionStore(database)
	segmentStore := db.NewSegmentStore(database)

	runID := "scp-049-run-3"
	runDir := filepath.Join(outputDir, runID)
	if err := os.MkdirAll(filepath.Join(runDir, "images", "scene_02"), 0o755); err != nil {
		t.Fatalf("mkdir images: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "tts"), 0o755); err != nil {
		t.Fatalf("mkdir tts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "clips"), 0o755); err != nil {
		t.Fatalf("mkdir clips: %v", err)
	}

	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, scenario_path, output_path)
		VALUES (?, '049', 'scenario.json', ?)`,
		runID, filepath.Join(runDir, "output.mp4")); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, note, created_at, superseded_by) VALUES
		  (41, ?, '0', 'reject', 'needs rewrite', '2026-04-24T12:34:56Z', 42),
		  (42, ?, '0', 'approve', NULL, '2026-04-24T12:35:56Z', NULL)`,
		runID, runID,
	); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	shots := `[{"image_path":"images/scene_02/shot_01.png","duration_s":3,"transition":"cut","visual_descriptor":"desc"},{"image_path":"` + filepath.Join(runDir, `images/scene_02/shot_02.png`) + `","duration_s":3,"transition":"cut","visual_descriptor":"desc"}]`
	if _, err := database.Exec(`
		INSERT INTO segments (run_id, scene_index, shot_count, shots, tts_path, clip_path, status)
		VALUES (?, 1, 2, ?, ?, ?, 'completed')`,
		runID, shots, "tts/scene_02.wav", filepath.Join(runDir, "clips", "scene_02.mp4"),
	); err != nil {
		t.Fatalf("seed segments: %v", err)
	}

	for _, rel := range []string{
		"scenario.json",
		"output.mp4",
		"metadata.json",
		"manifest.json",
		filepath.Join("images", "scene_02", "shot_01.png"),
		filepath.Join("images", "scene_02", "shot_02.png"),
		filepath.Join("tts", "scene_02.wav"),
		filepath.Join("clips", "scene_02.mp4"),
	} {
		path := filepath.Join(runDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir parent for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	return runStore, decisionStore, segmentStore, runID
}

func TestExportService_DecisionsJSON_IncludesEnvelopeAndSupersededBy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	runs, decisions, segments, runID := seedExportFixtures(t, outputDir)
	svc := service.NewExportService(runs, decisions, segments, outputDir)

	result, err := svc.Export(context.Background(), service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeDecisions,
		Format: service.ExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Path != filepath.Join(outputDir, runID, "export", "decisions.json") {
		t.Fatalf("path = %q", result.Path)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	var env struct {
		Version int                      `json:"version"`
		Data    []service.ExportDecision `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
	if len(env.Data) != 2 {
		t.Fatalf("rows = %d, want 2", len(env.Data))
	}
	if env.Data[0].SupersededBy == nil || *env.Data[0].SupersededBy != 42 {
		t.Fatalf("superseded_by missing: %+v", env.Data[0])
	}
	if env.Data[0].TargetItem != "scene:0" {
		t.Fatalf("target_item = %q, want scene:0", env.Data[0].TargetItem)
	}
}

func TestExportService_ArtifactsCSV_UsesRunRelativePaths(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	runs, decisions, segments, runID := seedExportFixtures(t, outputDir)
	svc := service.NewExportService(runs, decisions, segments, outputDir)

	result, err := svc.Export(context.Background(), service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeArtifacts,
		Format: service.ExportFormatCSV,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	file, err := os.Open(result.Path)
	if err != nil {
		t.Fatalf("open export: %v", err)
	}
	defer file.Close()

	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header + rows, got %v", rows)
	}
	wantHeader := []string{"artifact_type", "scene_index", "shot_index", "path"}
	for i := range wantHeader {
		if rows[0][i] != wantHeader[i] {
			t.Fatalf("header[%d] = %q, want %q", i, rows[0][i], wantHeader[i])
		}
	}

	seen := map[string]bool{}
	for _, row := range rows[1:] {
		seen[row[3]] = true
		if filepath.IsAbs(row[3]) {
			t.Fatalf("expected relative path, got %q", row[3])
		}
	}
	for _, want := range []string{
		"scenario.json",
		"output.mp4",
		"metadata.json",
		"manifest.json",
		"images/scene_02/shot_01.png",
		"images/scene_02/shot_02.png",
		"tts/scene_02.wav",
		"clips/scene_02.mp4",
	} {
		if !seen[want] {
			t.Fatalf("missing exported path %q in %v", want, seen)
		}
	}
}

// TestExportService_DecisionsJSON_EmitsExplicitNullForNonSuperseded pins the
// wire-level representation that the spec requires: for a non-superseded
// decision, `superseded_by` must appear as an explicit JSON null, not be
// absent. Struct unmarshal hides this distinction (both produce *int64=nil),
// so we scan the raw bytes.
func TestExportService_DecisionsJSON_EmitsExplicitNullForNonSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	runs, decisions, segments, runID := seedExportFixtures(t, outputDir)
	svc := service.NewExportService(runs, decisions, segments, outputDir)

	result, err := svc.Export(context.Background(), service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeDecisions,
		Format: service.ExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}

	// Decision 42 is the non-superseded row (approve, superseded_by NULL, note NULL).
	// Its JSON object must include "superseded_by": null literally.
	rawStr := string(raw)
	if !strings.Contains(rawStr, `"superseded_by": null`) {
		t.Fatalf("expected explicit null for superseded_by in JSON, got:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, `"note": null`) {
		t.Fatalf("expected explicit null for note in non-noted row, got:\n%s", rawStr)
	}
	// superseded_by must still serialize the integer for the superseded row.
	if !strings.Contains(rawStr, `"superseded_by": 42`) {
		t.Fatalf("expected superseded_by: 42 on the superseded row, got:\n%s", rawStr)
	}
}

// TestExportService_DecisionsJSON_EmitsExplicitNullForRunLevelSceneID covers
// the run-level (scene_id IS NULL) decision case so the envelope shape stays
// uniform across decision kinds.
func TestExportService_DecisionsJSON_EmitsExplicitNullForRunLevelSceneID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	decisionStore := db.NewDecisionStore(database)
	segmentStore := db.NewSegmentStore(database)
	runID := "scp-049-run-runlevel"

	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES (?, '049')`, runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at)
		VALUES (101, ?, NULL, 'hitl_resume', '2026-04-24T13:00:00Z')`, runID); err != nil {
		t.Fatalf("seed decision: %v", err)
	}

	svc := service.NewExportService(runStore, decisionStore, segmentStore, outputDir)
	result, err := svc.Export(context.Background(), service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeDecisions,
		Format: service.ExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(raw), `"scene_id": null`) {
		t.Fatalf("expected explicit null for scene_id on run-level decision, got:\n%s", raw)
	}
}

// TestExportService_RejectsDangerousRunIDs pins the guarantees of
// safeExportDirs: the export directory must never land outside a well-formed
// per-run subdirectory, regardless of how the caller attempts to coerce it.
func TestExportService_RejectsDangerousRunIDs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	database := testutil.NewTestDB(t)
	svc := service.NewExportService(
		db.NewRunStore(database),
		db.NewDecisionStore(database),
		db.NewSegmentStore(database),
		outputDir,
	)

	cases := []string{
		".",
		"..",
		".hidden",
		"foo/bar",
		`foo\bar`,
		"foo\x00evil",
		"",
	}
	for _, runID := range cases {
		t.Run(runID, func(t *testing.T) {
			_, err := svc.Export(context.Background(), service.ExportRequest{
				RunID:  runID,
				Type:   service.ExportTypeDecisions,
				Format: service.ExportFormatJSON,
			})
			if err == nil {
				t.Fatalf("expected rejection for runID=%q", runID)
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("expected domain.ErrValidation for runID=%q, got %v", runID, err)
			}
		})
	}
}

// TestExportService_DecisionsJSON_EmptyRunEmitsVersion1AndEmptyArray pins two
// envelope-shape invariants that are easy to regress:
//   - `version` is the JSON integer 1 (not "1", not 1.0)
//   - `data` serializes as `[]` (not `null`) for a run with zero decisions
//
// A `var rows []ExportDecision` + `json.Marshal` would flip `data` to `null`
// without touching any test that unmarshals into a slice, so we scan bytes.
func TestExportService_DecisionsJSON_EmptyRunEmitsVersion1AndEmptyArray(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	decisionStore := db.NewDecisionStore(database)
	segmentStore := db.NewSegmentStore(database)
	runID := "scp-049-run-empty"

	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES (?, '049')`, runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	svc := service.NewExportService(runStore, decisionStore, segmentStore, outputDir)
	result, err := svc.Export(context.Background(), service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeDecisions,
		Format: service.ExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Records != 0 {
		t.Fatalf("records = %d, want 0", result.Records)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, `"version": 1`) {
		t.Fatalf("expected version as integer 1, got:\n%s", rawStr)
	}
	if strings.Contains(rawStr, `"version": "1"`) || strings.Contains(rawStr, `"version": 1.0`) {
		t.Fatalf("version must serialize as integer 1, got:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, `"data": []`) {
		t.Fatalf("expected empty data array `[]` (not null), got:\n%s", rawStr)
	}
}

// TestExportService_DecisionsCSV_EscapesFormulaInjection pins the OWASP CSV
// formula-injection guard: a decision note that begins with `=`, `+`, `-`,
// `@`, tab, or CR must be prefixed with a single quote so that spreadsheet
// apps (Excel, Sheets, LibreOffice) treat it as text instead of evaluating
// it as a formula.
func TestExportService_DecisionsCSV_EscapesFormulaInjection(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	decisionStore := db.NewDecisionStore(database)
	segmentStore := db.NewSegmentStore(database)
	runID := "scp-049-run-csv-inject"

	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES (?, '049')`, runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, note, created_at) VALUES
		  (201, ?, '0', 'reject', '=HYPERLINK("http://evil","click")', '2026-04-24T14:00:00Z'),
		  (202, ?, '0', 'reject', '+cmd|calc',                         '2026-04-24T14:01:00Z'),
		  (203, ?, '0', 'reject', '@SUM(A1)',                          '2026-04-24T14:02:00Z')`,
		runID, runID, runID,
	); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	svc := service.NewExportService(runStore, decisionStore, segmentStore, outputDir)
	result, err := svc.Export(context.Background(), service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeDecisions,
		Format: service.ExportFormatCSV,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	f, err := os.Open(result.Path)
	if err != nil {
		t.Fatalf("open export: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4 (header + 3 decisions)", len(rows))
	}
	notes := map[string]string{}
	for _, row := range rows[1:] {
		notes[row[0]] = row[6]
	}
	for id, want := range map[string]string{
		"201": `'=HYPERLINK("http://evil","click")`,
		"202": `'+cmd|calc`,
		"203": `'@SUM(A1)`,
	} {
		if notes[id] != want {
			t.Fatalf("decision %s note = %q, want %q (formula injection guard)", id, notes[id], want)
		}
	}
}

func TestExportService_ArtifactsJSON_ToleratesMissingOptionalFiles(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	outputDir := t.TempDir()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	decisionStore := db.NewDecisionStore(database)
	segmentStore := db.NewSegmentStore(database)
	runID := "scp-049-run-4"

	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, scenario_path) VALUES (?, '049', 'scenario.json')`, runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO segments (run_id, scene_index, shot_count, shots, status)
		VALUES (?, 0, 0, '[]', 'pending')`, runID); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	runDir := filepath.Join(outputDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "scenario.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	svc := service.NewExportService(runStore, decisionStore, segmentStore, outputDir)
	result, err := svc.Export(context.Background(), service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeArtifacts,
		Format: service.ExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	var env struct {
		Version int                      `json:"version"`
		Data    []service.ExportArtifact `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
	if len(env.Data) != 1 || env.Data[0].Path != "scenario.json" {
		t.Fatalf("unexpected rows: %+v", env.Data)
	}
}
