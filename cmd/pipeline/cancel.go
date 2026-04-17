package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func newCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a running pipeline run",
		Args:  cobra.ExactArgs(1),
		RunE:  runCancel,
	}
}

func runCancel(cmd *cobra.Command, args []string) error {
	runID := args[0]

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

	if err := svc.Cancel(cmd.Context(), runID); err != nil {
		renderer := newRenderer(cmd.ErrOrStderr())
		renderer.RenderError(err)
		return &silentErr{err}
	}

	renderer := newRenderer(cmd.OutOrStdout())
	renderer.RenderSuccess(&CancelOutput{RunID: runID, Status: "cancelled"})
	return nil
}
