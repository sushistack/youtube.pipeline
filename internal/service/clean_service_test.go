package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// testFixture wires up a database, RunStore, SegmentStore, and filesystem
// outputDir for Soft Archive tests. The returned helper methods add runs +
// artifacts with explicit timestamps so candidate selection is deterministic.
type testFixture struct {
	t         *testing.T
	database  *sql.DB
	runs      *db.RunStore
	segments  *db.SegmentStore
	outputDir string
	svc       *service.CleanService
}

func newFixture(t *testing.T) *testFixture {
	t.Helper()
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	outputDir := t.TempDir()
	runs := db.NewRunStore(database)
	segs := db.NewSegmentStore(database)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewCleanService(runs, segs, database, outputDir, logger)
	return &testFixture{
		t:         t,
		database:  database,
		runs:      runs,
		segments:  segs,
		outputDir: outputDir,
		svc:       svc,
	}
}

// seedRun inserts a run row directly so we can control updated_at + status.
// Also materializes a per-run output directory with known artifact files.
func (f *testFixture) seedRun(scpID string, status domain.Status, updatedAt string, withArtifacts bool) string {
	f.t.Helper()
	id := "scp-" + scpID + "-run-1"
	if _, err := f.database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status, scenario_path, output_path, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, scpID, string(domain.StageComplete), string(status),
		"scenario.json", "output.mp4", updatedAt,
	); err != nil {
		f.t.Fatalf("seed run %s: %v", id, err)
	}
	runDir := filepath.Join(f.outputDir, id)
	if withArtifacts {
		mustWriteFile(f.t, filepath.Join(runDir, "scenario.json"), `{"scene_count":1}`)
		mustWriteFile(f.t, filepath.Join(runDir, "output.mp4"), "video-bytes")
		mustWriteFile(f.t, filepath.Join(runDir, "metadata.json"), "{}")
		mustWriteFile(f.t, filepath.Join(runDir, "manifest.json"), "{}")
		mustWriteFile(f.t, filepath.Join(runDir, "tts", "scene_01.wav"), "wav-bytes")
		mustWriteFile(f.t, filepath.Join(runDir, "images", "scene_01", "shot_01.png"), "img-bytes")
		mustWriteFile(f.t, filepath.Join(runDir, "clips", "scene_01.mp4"), "clip-bytes")
	}
	return id
}

// seedSegment inserts a segment row with TTS/clip/image fields populated so
// the cleaner has DB refs to null and the tests can assert counts.
func (f *testFixture) seedSegment(runID string, sceneIndex int) {
	f.t.Helper()
	shotsJSON := `[{"image_path":"images/scene_01/shot_01.png","duration_s":2.0,"transition":"cut","visual_descriptor":"desc"}]`
	if _, err := f.database.ExecContext(context.Background(),
		`INSERT INTO segments (run_id, scene_index, shot_count, shots, tts_path, tts_duration_ms, clip_path, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, sceneIndex, 1, shotsJSON,
		"tts/scene_01.wav", 2000, "clips/scene_01.mp4",
		"pending",
	); err != nil {
		f.t.Fatalf("seed segment: %v", err)
	}
}

// seedDecision inserts a decision row so preservation tests can assert that
// decisions survive archive per AC-3 "no rows are deleted from decisions".
func (f *testFixture) seedDecision(runID string) {
	f.t.Helper()
	if _, err := f.database.ExecContext(context.Background(),
		`INSERT INTO decisions (run_id, decision_type) VALUES (?, ?)`,
		runID, "scene_approved",
	); err != nil {
		f.t.Fatalf("seed decision: %v", err)
	}
}

func countRows(t *testing.T, database *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustStatMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be missing, stat err=%v", path, err)
	}
}

func mustStatPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist, stat err=%v", path, err)
	}
}

// --- AC tests ---

func TestCleanService_Clean_RejectsInvalidRetention(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Clean(context.Background(), 0, time.Now())
	if err == nil {
		t.Fatal("expected validation error for retention_days=0")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestCleanService_Clean_ArchivesOnlyTerminalOldRuns(t *testing.T) {
	f := newFixture(t)

	// 2026-04-24 minus 30 days = 2026-03-25. Old run: 2026-01-01. Recent run: 2026-04-20.
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	oldCompleted := f.seedRun("049", domain.StatusCompleted, "2026-01-01 00:00:00", true)
	oldFailed := f.seedRun("050", domain.StatusFailed, "2026-01-15 00:00:00", true)
	recentCompleted := f.seedRun("051", domain.StatusCompleted, "2026-04-20 00:00:00", true)
	oldRunning := f.seedRun("052", domain.StatusRunning, "2025-12-01 00:00:00", true)

	summary, err := f.svc.Clean(context.Background(), 30, now)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if summary.RunsScanned != 2 {
		t.Errorf("RunsScanned: got %d want 2", summary.RunsScanned)
	}
	if summary.RunsArchived != 2 {
		t.Errorf("RunsArchived: got %d want 2", summary.RunsArchived)
	}

	// Archived runs: artifacts gone, scenario_path + output_path nulled.
	for _, id := range []string{oldCompleted, oldFailed} {
		mustStatMissing(t, filepath.Join(f.outputDir, id, "scenario.json"))
		mustStatMissing(t, filepath.Join(f.outputDir, id, "output.mp4"))
		row := f.database.QueryRow(
			"SELECT scenario_path, output_path FROM runs WHERE id = ?", id)
		var scn, out sql.NullString
		if err := row.Scan(&scn, &out); err != nil {
			t.Fatalf("scan %s: %v", id, err)
		}
		if scn.Valid {
			t.Errorf("%s scenario_path: want NULL, got %q", id, scn.String)
		}
		if out.Valid {
			t.Errorf("%s output_path: want NULL, got %q", id, out.String)
		}
	}

	// Recent + active runs must still have artifacts and DB refs.
	for _, id := range []string{recentCompleted, oldRunning} {
		mustStatPresent(t, filepath.Join(f.outputDir, id, "scenario.json"))
		row := f.database.QueryRow(
			"SELECT scenario_path FROM runs WHERE id = ?", id)
		var scn sql.NullString
		if err := row.Scan(&scn); err != nil {
			t.Fatalf("scan %s: %v", id, err)
		}
		if !scn.Valid {
			t.Errorf("%s scenario_path: want preserved, got NULL", id)
		}
	}
}

func TestCleanService_Clean_PreservesAllRowsAndSegmentMetadata(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	archivedID := f.seedRun("049", domain.StatusCompleted, "2026-01-01 00:00:00", true)
	untouchedID := f.seedRun("050", domain.StatusCompleted, "2026-04-23 00:00:00", true)
	f.seedSegment(archivedID, 0)
	f.seedSegment(archivedID, 1)
	f.seedSegment(untouchedID, 0)
	f.seedDecision(archivedID)
	f.seedDecision(untouchedID)

	runsBefore := countRows(t, f.database, "runs")
	segsBefore := countRows(t, f.database, "segments")
	decsBefore := countRows(t, f.database, "decisions")

	summary, err := f.svc.Clean(context.Background(), 30, now)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if summary.RunsArchived != 1 {
		t.Errorf("RunsArchived: got %d want 1", summary.RunsArchived)
	}

	if countRows(t, f.database, "runs") != runsBefore {
		t.Error("runs row count changed — NFR-O2 violated")
	}
	if countRows(t, f.database, "segments") != segsBefore {
		t.Error("segments row count changed — AC-3 violated")
	}
	if countRows(t, f.database, "decisions") != decsBefore {
		t.Error("decisions row count changed — NFR-O2 violated")
	}

	// Archived segments: tts_path / clip_path NULL; shots image_path cleared.
	rows, err := f.database.Query(
		"SELECT tts_path, clip_path, shots FROM segments WHERE run_id = ?", archivedID)
	if err != nil {
		t.Fatalf("query archived segments: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var tts, clip, shotsJSON sql.NullString
		if err := rows.Scan(&tts, &clip, &shotsJSON); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if tts.Valid {
			t.Errorf("tts_path: want NULL, got %q", tts.String)
		}
		if clip.Valid {
			t.Errorf("clip_path: want NULL, got %q", clip.String)
		}
		if !shotsJSON.Valid {
			continue
		}
		// image_path inside shots JSON must be empty string.
		var shots []domain.Shot
		if err := json.Unmarshal([]byte(shotsJSON.String), &shots); err != nil {
			t.Fatalf("decode shots: %v", err)
		}
		for _, s := range shots {
			if s.ImagePath != "" {
				t.Errorf("shot.image_path: want empty, got %q", s.ImagePath)
			}
		}
	}
	if seen != 2 {
		t.Errorf("expected 2 archived segment rows, saw %d", seen)
	}

	// Untouched segment: all path refs still present.
	row := f.database.QueryRow(
		"SELECT tts_path, clip_path FROM segments WHERE run_id = ?", untouchedID)
	var ttsU, clipU sql.NullString
	if err := row.Scan(&ttsU, &clipU); err != nil {
		t.Fatalf("scan untouched: %v", err)
	}
	if !ttsU.Valid || ttsU.String == "" {
		t.Error("untouched run's tts_path was cleared — run-scope violation")
	}
	if !clipU.Valid || clipU.String == "" {
		t.Error("untouched run's clip_path was cleared — run-scope violation")
	}
}

func TestCleanService_Clean_VacuumGatedOnActiveRuns(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	// Old terminal run is archivable.
	f.seedRun("049", domain.StatusCompleted, "2026-01-01 00:00:00", true)
	// Active run keeps the system non-idle.
	f.seedRun("050", domain.StatusWaiting, "2026-04-23 00:00:00", true)

	summary, err := f.svc.Clean(context.Background(), 30, now)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if summary.RunsArchived != 1 {
		t.Errorf("RunsArchived: got %d want 1", summary.RunsArchived)
	}
	if summary.Vacuum != service.VacuumSkippedActive {
		t.Errorf("Vacuum: got %q want %q", summary.Vacuum, service.VacuumSkippedActive)
	}
}

func TestCleanService_Clean_VacuumRunsWhenIdle(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	f.seedRun("049", domain.StatusCompleted, "2026-01-01 00:00:00", true)

	// Create some garbage rows on another table and delete them to produce
	// free pages, so VACUUM has something to reclaim.
	for i := 0; i < 500; i++ {
		if _, err := f.database.Exec(
			`INSERT INTO calibration_snapshots (id, window, threshold, kappa) VALUES (?, ?, ?, ?)`,
			i, 10, 0.5, 0.4,
		); err != nil {
			// Table may not exist in the schema; this is best-effort and
			// not critical for the Vacuum=ran check.
			break
		}
	}
	_, _ = f.database.Exec(`DELETE FROM calibration_snapshots`)

	// Capture freelist_count BEFORE archive so we can verify VACUUM reclaimed.
	var preFreelist int
	_ = f.database.QueryRow("PRAGMA freelist_count").Scan(&preFreelist)

	summary, err := f.svc.Clean(context.Background(), 30, now)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if summary.Vacuum != service.VacuumRan {
		t.Errorf("Vacuum: got %q (err=%s) want %q",
			summary.Vacuum, summary.VacuumError, service.VacuumRan)
	}

	var postFreelist int
	if err := f.database.QueryRow("PRAGMA freelist_count").Scan(&postFreelist); err != nil {
		t.Fatalf("post-vacuum freelist_count: %v", err)
	}
	if postFreelist != 0 {
		t.Errorf("post-vacuum freelist_count: got %d want 0", postFreelist)
	}
}

func TestCleanService_Clean_EmptyDBProducesIdleVacuum(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	summary, err := f.svc.Clean(context.Background(), 30, now)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if summary.RunsScanned != 0 {
		t.Errorf("RunsScanned: got %d want 0", summary.RunsScanned)
	}
	if summary.RunsArchived != 0 {
		t.Errorf("RunsArchived: got %d want 0", summary.RunsArchived)
	}
	if summary.Vacuum != service.VacuumRan {
		t.Errorf("Vacuum: got %q want %q (idle system)", summary.Vacuum, service.VacuumRan)
	}
}

func TestCleanService_Clean_IdempotentOnAlreadyArchivedRun(t *testing.T) {
	f := newFixture(t)

	f.seedRun("049", domain.StatusCompleted, "2026-01-01 00:00:00", true)

	// First pass uses a near-term "now" so the 2026-01-01 run falls past the
	// 30-day cutoff and gets archived.
	firstNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	if _, err := f.svc.Clean(context.Background(), 30, firstNow); err != nil {
		t.Fatalf("first Clean: %v", err)
	}

	// The archive mutations advanced updated_at via the runs_updated_at
	// trigger to the current wall-clock time. Use a far-future "now" so the
	// already-archived run again falls past the cutoff and re-enters the
	// candidate set. The second pass must succeed without error and be a
	// file-system no-op (nothing left to delete).
	secondNow := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	summary, err := f.svc.Clean(context.Background(), 30, secondNow)
	if err != nil {
		t.Fatalf("second Clean: %v", err)
	}
	if summary.RunsScanned != 1 {
		t.Errorf("second RunsScanned: got %d want 1", summary.RunsScanned)
	}
	// files_deleted must be 0 on the second pass — nothing to remove.
	if summary.FilesDeleted != 0 {
		t.Errorf("second FilesDeleted: got %d want 0", summary.FilesDeleted)
	}
}

func TestCleanService_Clean_ToleratesMissingRunDir(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	// seed without artifact files or directory.
	id := f.seedRun("049", domain.StatusCompleted, "2026-01-01 00:00:00", false)

	summary, err := f.svc.Clean(context.Background(), 30, now)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if summary.RunsArchived != 1 {
		t.Errorf("RunsArchived: got %d want 1", summary.RunsArchived)
	}
	// DB paths cleared even when the runDir never existed.
	row := f.database.QueryRow(
		"SELECT scenario_path FROM runs WHERE id = ?", id)
	var scn sql.NullString
	if err := row.Scan(&scn); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scn.Valid {
		t.Errorf("scenario_path: want NULL after archive, got %q", scn.String)
	}
}
