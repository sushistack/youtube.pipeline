package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func newCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <scp-id>",
		Short: "Create a new pipeline run for the given SCP ID",
		Args:  cobra.ExactArgs(1),
		RunE:  runCreate,
	}
}

func runCreate(cmd *cobra.Command, args []string) error {
	scpID := args[0]

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

	run, err := svc.Create(cmd.Context(), scpID, cfg.OutputDir)
	if err != nil {
		renderer := newRenderer(cmd.ErrOrStderr())
		renderer.RenderError(err)
		return &silentErr{err}
	}

	out := &RunOutput{
		ID:        run.ID,
		SCPID:     run.SCPID,
		Stage:     string(run.Stage),
		Status:    string(run.Status),
		CreatedAt: run.CreatedAt,
		OutputDir: fmt.Sprintf("%s/%s", cfg.OutputDir, run.ID),
	}
	renderer := newRenderer(cmd.OutOrStdout())
	renderer.RenderSuccess(out)
	return nil
}
