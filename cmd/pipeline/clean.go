package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// cleanClock is injectable for tests so candidate-selection cutoffs are
// deterministic. Production uses clock.RealClock; tests override it via the
// package-level variable to point at a fake.
var cleanClock clock.Clock = clock.RealClock{}

func newCleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Soft-archive old run artifacts and vacuum the database",
		Long: `Clean deletes artifact files for terminal runs whose updated_at is older
than artifact_retention_days and nulls their DB path references, while
preserving every runs/segments/decisions row (NFR-O2). After archive,
VACUUM is executed to reclaim free pages when no active runs are present.

The command is explicit/manual in V1 — no background scheduler runs it.`,
		RunE: runClean,
	}
}

func runClean(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgPath, config.DefaultEnvPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	runs := db.NewRunStore(database)
	segs := db.NewSegmentStore(database)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc := service.NewCleanService(runs, segs, database, cfg.OutputDir, logger)

	summary, err := svc.Clean(cmd.Context(), cfg.ArtifactRetentionDays, cleanClock.Now())
	if err != nil {
		renderer := newRenderer(cmd.ErrOrStderr())
		renderer.RenderError(err)
		return &silentErr{err}
	}

	out := cleanOutputFromSummary(summary)
	renderer := newRenderer(cmd.OutOrStdout())
	renderer.RenderSuccess(out)
	// AC-5: VACUUM failure must be surfaced, not silently swallowed.
	// The summary (already rendered above) carries vacuum_error; the non-zero
	// exit code signals failure to automation pipelines that check $?.
	if summary.Vacuum == service.VacuumFailed {
		return &silentErr{fmt.Errorf("vacuum failed: %s", summary.VacuumError)}
	}
	return nil
}

// cleanOutputFromSummary maps service.CleanSummary to the CLI CleanOutput.
// Both share identical shape today; this indirection keeps the service type
// free to evolve without spraying changes into the CLI envelope.
func cleanOutputFromSummary(s *service.CleanSummary) *CleanOutput {
	out := &CleanOutput{
		RetentionDays: s.RetentionDays,
		CutoffUTC:     s.CutoffUTC,
		RunsScanned:   s.RunsScanned,
		RunsArchived:  s.RunsArchived,
		FilesDeleted:  s.FilesDeleted,
		DBRefsCleared: s.DBRefsCleared,
		Vacuum:        string(s.Vacuum),
		VacuumError:   s.VacuumError,
	}
	if len(s.ArchivedRuns) > 0 {
		out.ArchivedRuns = make([]CleanArchivedRun, len(s.ArchivedRuns))
		for i, a := range s.ArchivedRuns {
			out.ArchivedRuns[i] = CleanArchivedRun{
				ID:            a.ID,
				Status:        a.Status,
				UpdatedAt:     a.UpdatedAt,
				FilesDeleted:  a.FilesDeleted,
				DBRefsCleared: a.DBRefsCleared,
			}
		}
	}
	return out
}

