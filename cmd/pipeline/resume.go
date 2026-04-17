package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func newResumeCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "resume <run-id>",
		Short: "Resume a failed or waiting pipeline run from its last successful stage",
		Long: `Resume re-enters the failed (or HITL-waiting) stage of a run after
verifying filesystem/DB consistency and cleaning stage-scoped partial artifacts.

Phase B resume (image/tts) deletes all segments for the run and starts
clean-slate. Other stages preserve segments.

Use --force to proceed even when filesystem/DB mismatches are detected.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResume(cmd, args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "proceed despite filesystem/DB inconsistencies")
	return cmd
}

func runResume(cmd *cobra.Command, runID string, force bool) error {
	cfg, err := config.Load(cfgPath, config.DefaultEnvPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	store := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	engine := pipeline.NewEngine(store, segStore, clock.RealClock{}, cfg.OutputDir, logger)
	svc := service.NewRunService(store, engine)

	run, report, err := svc.Resume(cmd.Context(), runID, force)
	if err != nil {
		renderer := newRenderer(cmd.ErrOrStderr())
		renderer.RenderError(err)
		return &silentErr{err}
	}

	out := &ResumeOutput{
		Run: RunOutput{
			ID:        run.ID,
			SCPID:     run.SCPID,
			Stage:     string(run.Stage),
			Status:    string(run.Status),
			CreatedAt: run.CreatedAt,
			UpdatedAt: run.UpdatedAt,
		},
		Warnings: mismatchLines(report),
	}
	renderer := newRenderer(cmd.OutOrStdout())
	renderer.RenderSuccess(out)
	return nil
}

func mismatchLines(report *domain.InconsistencyReport) []string {
	if report == nil || len(report.Mismatches) == 0 {
		return nil
	}
	out := make([]string, 0, len(report.Mismatches))
	for _, m := range report.Mismatches {
		line := m.Kind + "@" + m.Path
		if m.Detail != "" {
			line += ": " + m.Detail
		}
		out = append(out, line)
	}
	return out
}
