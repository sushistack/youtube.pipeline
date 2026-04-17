package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [run-id]",
		Short: "Show pipeline run status (all runs, or a specific run)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
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
	svc := service.NewRunService(store, nil)
	renderer := newRenderer(cmd.OutOrStdout())

	if len(args) == 1 {
		// Single run detail view.
		runID := args[0]
		run, err := svc.Get(cmd.Context(), runID)
		if err != nil {
			renderer.RenderError(err)
			return &silentErr{err}
		}
		out := &RunOutput{
			ID:        run.ID,
			SCPID:     run.SCPID,
			Stage:     string(run.Stage),
			Status:    string(run.Status),
			CreatedAt: run.CreatedAt,
			UpdatedAt: run.UpdatedAt,
		}
		renderer.RenderSuccess(out)
		return nil
	}

	// All runs list view.
	runs, err := svc.List(cmd.Context())
	if err != nil {
		renderer.RenderError(err)
		return &silentErr{err}
	}

	items := make([]RunOutput, len(runs))
	for i, r := range runs {
		items[i] = RunOutput{
			ID:        r.ID,
			SCPID:     r.SCPID,
			Stage:     string(r.Stage),
			Status:    string(r.Status),
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}
	renderer.RenderSuccess(&RunListOutput{Runs: items, Total: len(items)})
	return nil
}
