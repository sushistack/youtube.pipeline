package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func newExportCmd() *cobra.Command {
	var runID string
	var exportType string
	var format string

	cmd := &cobra.Command{
		Use:          "export",
		Short:        "Export run decisions or artifact metadata to JSON or CSV",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cfgPath, config.DefaultEnvPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			database, err := db.OpenDB(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer database.Close()

			renderer := newRenderer(cmd.OutOrStdout())
			exportSvc := service.NewExportService(
				db.NewRunStore(database),
				db.NewDecisionStore(database),
				db.NewSegmentStore(database),
				cfg.OutputDir,
			)

			result, err := exportSvc.Export(cmd.Context(), service.ExportRequest{
				RunID:  runID,
				Type:   exportType,
				Format: format,
			})
			if err != nil {
				renderer.RenderError(err)
				return &silentErr{err}
			}

			renderer.RenderSuccess(&ExportOutput{
				RunID:   result.RunID,
				Type:    result.Type,
				Format:  result.Format,
				Path:    result.Path,
				Records: result.Records,
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&runID, "run-id", "", "run ID to export")
	cmd.Flags().StringVar(&exportType, "type", "", "export type: decisions or artifacts")
	cmd.Flags().StringVar(&format, "format", service.ExportFormatJSON, "export format: json or csv")
	_ = cmd.MarkFlagRequired("run-id")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}
