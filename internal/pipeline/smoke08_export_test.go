package pipeline_test

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestSMOKE_08_ExportRoundTrip pins two coupled invariants of the
// decisions export: (a) the CSV-injection guard prefixes every cell
// that begins with a spreadsheet-evaluated character (=, +, -, @, \t,
// \r) with a single quote, neutralizing OWASP CSV-injection payloads;
// (b) running the same export twice produces byte-identical output —
// no embedded timestamps, no nondeterministic ordering, no `.tmp`
// detritus left behind on either run.
//
// Step 3 §4 SMOKE-08 worded the payload as "scene narration starts with
// =SUM(A1)", but narration is not part of the decisions/artifacts export
// surface. This test injects the same payload into a free-text column
// that IS exported (decisions.note) so the same csvCellSafe path fires.
//
// Runtime budget: ≤ 5 s. No ffmpeg, no external HTTP.
func TestSMOKE_08_ExportRoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	const (
		runID                  = "scp-049-rt-1"
		decisionInjection      = "=SUM(A1)+1+cmd"
		artifactPathInjection  = "-bad.png" // leading-dash payload exercises the guard's `-` branch
	)

	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	decisionStore := db.NewDecisionStore(database)
	segmentStore := db.NewSegmentStore(database)

	outputDir := t.TempDir()
	runDir := filepath.Join(outputDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}

	// Seed a run with an output_path so GetExportRecord succeeds.
	ctx := context.Background()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id, scenario_path, output_path)
		 VALUES (?, '049', 'scenario.json', ?)`,
		runID, filepath.Join(runDir, "output.mp4"),
	); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	// Seed two decision rows: one carrying the CSV-injection payload in
	// note, and one benign control so the guard's "leave normal cells
	// unchanged" branch is also covered.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO decisions (id, run_id, scene_id, decision_type, note, created_at, superseded_by) VALUES
		  (1001, ?, '0', 'reject', ?, '2026-04-25T10:00:00Z', NULL),
		  (1002, ?, '0', 'approve', 'normal text', '2026-04-25T10:00:01Z', NULL)`,
		runID, decisionInjection, runID,
	); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	// Seed a segment whose Shots[].image_path begins with `-`, so the
	// artifacts export emits the artifact-side injection payload through
	// the same csvCellSafe path. The export reads the actual on-disk
	// shots JSON, so persist a minimal valid row.
	shotsJSON := `[{"image_path":"` + artifactPathInjection + `","duration_s":1,"transition":"ken_burns","visual_descriptor":"d"}]`
	if _, err := database.ExecContext(ctx, `
		INSERT INTO segments (run_id, scene_index, shot_count, shots, status)
		VALUES (?, 0, 1, ?, 'completed')`,
		runID, shotsJSON,
	); err != nil {
		t.Fatalf("seed segments: %v", err)
	}

	svc := service.NewExportService(runStore, decisionStore, segmentStore, outputDir)

	// ── Decisions export: idempotency + injection guard on note column. ──
	res1, err := svc.Export(ctx, service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeDecisions,
		Format: service.ExportFormatCSV,
	})
	if err != nil {
		t.Fatalf("first decisions Export: %v", err)
	}
	if res1.Records != 2 {
		t.Errorf("first export records = %d, want 2", res1.Records)
	}
	bytes1, err := os.ReadFile(res1.Path)
	if err != nil {
		t.Fatalf("read first decisions export: %v", err)
	}

	res2, err := svc.Export(ctx, service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeDecisions,
		Format: service.ExportFormatCSV,
	})
	if err != nil {
		t.Fatalf("second decisions Export: %v", err)
	}
	if res2.Path != res1.Path {
		t.Errorf("path drift across invocations: %q vs %q", res2.Path, res1.Path)
	}
	bytes2, err := os.ReadFile(res2.Path)
	if err != nil {
		t.Fatalf("read second decisions export: %v", err)
	}
	if sha256.Sum256(bytes1) != sha256.Sum256(bytes2) {
		t.Errorf("decisions export idempotency violated (sha256 mismatch)")
	}

	assertCellGuarded(t, res1.Path, "note", decisionInjection)

	// ── Artifacts export: same guard fires on `-` leading-dash payload. ──
	resArt, err := svc.Export(ctx, service.ExportRequest{
		RunID:  runID,
		Type:   service.ExportTypeArtifacts,
		Format: service.ExportFormatCSV,
	})
	if err != nil {
		t.Fatalf("artifacts Export: %v", err)
	}
	assertCellGuarded(t, resArt.Path, "path", artifactPathInjection)

	// No `.tmp` partial-write detritus may remain in the export dir. The
	// frozen contract is `*.tmp` only — dotfiles are not the concern.
	exportDir := filepath.Dir(res1.Path)
	entries, err := os.ReadDir(exportDir)
	if err != nil {
		t.Fatalf("read export dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("unexpected partial-write leftover %q in export dir", e.Name())
		}
	}
}

// assertCellGuarded loads csvPath, locates the column named colName, and
// fails the test unless exactly one data row carries `'<payload>` in
// that column AND no row carries the bare unguarded `<payload>`. A
// missing match (e.g., the row was dropped silently or the prefix
// differs) hard-fails — the previous switch-fall-through pattern would
// have passed silently in that scenario.
func assertCellGuarded(t *testing.T, csvPath, colName, payload string) {
	t.Helper()
	rows := parseCSV(t, csvPath)
	if len(rows) < 2 {
		t.Fatalf("csv %s has %d rows, want at least 2 (header + data)", csvPath, len(rows))
	}
	colIdx := -1
	for i, h := range rows[0] {
		if h == colName {
			colIdx = i
			break
		}
	}
	if colIdx < 0 {
		t.Fatalf("column %q missing in header of %s: %v", colName, csvPath, rows[0])
	}

	guarded := "'" + payload
	var matches int
	for _, row := range rows[1:] {
		if len(row) <= colIdx {
			continue
		}
		switch row[colIdx] {
		case guarded:
			matches++
		case payload:
			t.Errorf("%s column %q carried VERBATIM injection payload %q (guard regression)",
				csvPath, colName, payload)
		}
	}
	if matches != 1 {
		t.Errorf("expected exactly 1 row with guarded %q in %s column %q, got %d",
			guarded, csvPath, colName, matches)
	}
}

func parseCSV(t testing.TB, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open csv: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return rows
}
