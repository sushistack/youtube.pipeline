package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// VacuumState describes the post-archive VACUUM outcome in the clean summary.
type VacuumState string

const (
	VacuumRan              VacuumState = "ran"
	VacuumSkippedActive    VacuumState = "skipped_active_runs"
	VacuumFailed           VacuumState = "failed"
)

// CleanSummary is the Story 10.3 AC-1 output contract for `pipeline clean`.
// It is the data payload rendered into both the human and JSON envelopes.
type CleanSummary struct {
	RetentionDays int           `json:"retention_days"`
	CutoffUTC     string        `json:"cutoff_utc"`
	RunsScanned   int           `json:"runs_scanned"`
	RunsArchived  int           `json:"runs_archived"`
	FilesDeleted  int           `json:"files_deleted"`
	DBRefsCleared int           `json:"db_refs_cleared"`
	Vacuum        VacuumState   `json:"vacuum"`
	VacuumError   string        `json:"vacuum_error,omitempty"`
	ArchivedRuns  []ArchivedRun `json:"archived_runs,omitempty"`
}

// ArchivedRun is one row in the CleanSummary's per-run details slice.
type ArchivedRun struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	UpdatedAt     string `json:"updated_at"`
	FilesDeleted  int    `json:"files_deleted"`
	DBRefsCleared int    `json:"db_refs_cleared"`
}

// CleanRunStore is the narrow RunStore surface CleanService needs. *db.RunStore
// satisfies it structurally. Declared here to keep service→db dependency
// one-way and to let tests supply doubles without importing the full store.
type CleanRunStore interface {
	ListArchiveCandidates(ctx context.Context, cutoff time.Time) ([]db.ArchiveCandidate, error)
	ClearRunArtifactPaths(ctx context.Context, runID string) error
	HasActiveRuns(ctx context.Context) (bool, error)
}

// CleanSegmentStore is the narrow SegmentStore surface CleanService needs.
// *db.SegmentStore satisfies it structurally.
type CleanSegmentStore interface {
	ClearImageArtifactsByRunID(ctx context.Context, runID string) (int64, error)
	ClearTTSArtifactsByRunID(ctx context.Context, runID string) (int64, error)
	ClearClipPathsByRunID(ctx context.Context, runID string) (int64, error)
}

// CleanService orchestrates Story 10.3 Soft Archive. It iterates eligible
// runs (terminal + older than cutoff), deletes artifact files under each
// run's output directory, nulls the DB path references on runs/segments,
// and — when the system is idle — runs SQLite VACUUM to reclaim free pages.
// No run rows are deleted: NFR-O2 retention stays binding.
type CleanService struct {
	runs      CleanRunStore
	segments  CleanSegmentStore
	rawDB     *sql.DB
	outputDir string
	logger    *slog.Logger
}

// NewCleanService constructs a CleanService. rawDB is the underlying *sql.DB
// used to execute VACUUM; it cannot run through the store abstraction because
// VACUUM must not be inside a transaction. outputDir is the base run tree
// (cfg.OutputDir); per-run directories are joined by run ID.
func NewCleanService(
	runs CleanRunStore,
	segments CleanSegmentStore,
	rawDB *sql.DB,
	outputDir string,
	logger *slog.Logger,
) *CleanService {
	if logger == nil {
		logger = slog.Default()
	}
	return &CleanService{
		runs:      runs,
		segments:  segments,
		rawDB:     rawDB,
		outputDir: outputDir,
		logger:    logger,
	}
}

// Clean performs Soft Archive using the given retention window and current
// time. Callers supply now so tests can use a FakeClock. The returned summary
// always reflects reality: a partial failure mid-run still surfaces the runs
// that were archived successfully, and VacuumError is populated if the final
// reclaim failed. The command's exit code is the caller's job — this service
// returns an error only for fatal pre-archive failures (DB down, config
// wrong). Per-run errors are logged and counted but do not halt the sweep.
func (s *CleanService) Clean(ctx context.Context, retentionDays int, now time.Time) (*CleanSummary, error) {
	if retentionDays < 1 {
		return nil, fmt.Errorf("clean: artifact_retention_days must be >= 1, got %d: %w",
			retentionDays, domain.ErrValidation)
	}

	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	summary := &CleanSummary{
		RetentionDays: retentionDays,
		CutoffUTC:     cutoff.Format(time.RFC3339),
	}

	candidates, err := s.runs.ListArchiveCandidates(ctx, cutoff)
	if err != nil {
		return nil, fmt.Errorf("clean: list candidates: %w", err)
	}
	summary.RunsScanned = len(candidates)

	for _, c := range candidates {
		detail, perRunErr := s.archiveRun(ctx, c)
		if perRunErr != nil {
			// Per-run failure is logged but does not abort the sweep.
			// The run remains eligible on the next invocation.
			s.logger.Warn("clean: per-run archive failed",
				"run_id", c.ID, "error", perRunErr.Error())
			continue
		}
		summary.RunsArchived++
		summary.FilesDeleted += detail.FilesDeleted
		summary.DBRefsCleared += detail.DBRefsCleared
		summary.ArchivedRuns = append(summary.ArchivedRuns, detail)
	}

	s.applyVacuum(ctx, summary)

	return summary, nil
}

// archiveRun performs Soft Archive for a single candidate. Order matters:
// filesystem cleanup runs first, DB nulling second, so a partial failure
// mid-archive does not leave the DB pointing at files that were just deleted
// (which would look like an FS/DB mismatch to the resume consistency check).
func (s *CleanService) archiveRun(ctx context.Context, c db.ArchiveCandidate) (ArchivedRun, error) {
	runDir := filepath.Join(s.outputDir, c.ID)

	filesDeleted, err := pipeline.ArchiveRunArtifacts(runDir)
	if err != nil {
		return ArchivedRun{}, fmt.Errorf("archive artifacts for %s: %w", c.ID, err)
	}

	refsCleared := 0

	// Image refs live inside segments.shots JSON; clear them first — the
	// helper is transactional so partial failure leaves segment rows intact.
	nImg, err := s.segments.ClearImageArtifactsByRunID(ctx, c.ID)
	if err != nil {
		return ArchivedRun{}, fmt.Errorf("clear image artifacts for %s: %w", c.ID, err)
	}
	refsCleared += int(nImg)

	nTTS, err := s.segments.ClearTTSArtifactsByRunID(ctx, c.ID)
	if err != nil {
		return ArchivedRun{}, fmt.Errorf("clear tts artifacts for %s: %w", c.ID, err)
	}
	refsCleared += int(nTTS)

	nClip, err := s.segments.ClearClipPathsByRunID(ctx, c.ID)
	if err != nil {
		return ArchivedRun{}, fmt.Errorf("clear clip paths for %s: %w", c.ID, err)
	}
	refsCleared += int(nClip)

	// Run-level scenario_path + output_path are nulled in one UPDATE.
	// ClearRunArtifactPaths returns ErrNotFound only if the row vanished
	// between the LIST and this UPDATE — treat that as a no-op and let the
	// sweep continue without counting it as archived.
	if err := s.runs.ClearRunArtifactPaths(ctx, c.ID); err != nil {
		return ArchivedRun{}, fmt.Errorf("clear run artifact paths for %s: %w", c.ID, err)
	}
	// Only count a ref as "cleared" when it was actually non-null before
	// archiving. Unconditionally adding 2 would overstate the count on
	// re-runs where scenario_path and output_path were already NULL.
	if c.ScenarioPath != nil {
		refsCleared++
	}
	if c.OutputPath != nil {
		refsCleared++
	}

	return ArchivedRun{
		ID:            c.ID,
		Status:        string(c.Status),
		UpdatedAt:     c.UpdatedAt,
		FilesDeleted:  filesDeleted,
		DBRefsCleared: refsCleared,
	}, nil
}

// applyVacuum executes SQLite VACUUM when the system is idle. It fills in the
// summary's Vacuum + VacuumError fields and never propagates VACUUM failures
// as fatal errors — the archive phase already succeeded by the time we get
// here, and the operator needs to see both outcomes in the same summary.
func (s *CleanService) applyVacuum(ctx context.Context, summary *CleanSummary) {
	active, err := s.runs.HasActiveRuns(ctx)
	if err != nil {
		summary.Vacuum = VacuumFailed
		summary.VacuumError = fmt.Sprintf("check active runs: %v", err)
		return
	}
	if active {
		summary.Vacuum = VacuumSkippedActive
		return
	}

	// VACUUM must run outside any transaction. CleanService intentionally
	// does not wrap archive work in a single transaction (each helper is
	// already scoped per-run), so we can issue VACUUM directly here.
	if _, err := s.rawDB.ExecContext(ctx, "VACUUM"); err != nil {
		summary.Vacuum = VacuumFailed
		summary.VacuumError = err.Error()
		s.logger.Warn("clean: vacuum failed", "error", err.Error())
		return
	}
	summary.Vacuum = VacuumRan
}
