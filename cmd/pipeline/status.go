package main

import (
	"fmt"
	"log/slog"
	"os"

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
	decisionStore := db.NewDecisionStore(database)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	hitlSvc := service.NewHITLService(store, decisionStore, logger)
	renderer := newRenderer(cmd.OutOrStdout())

	if len(args) == 1 {
		// Single run detail view — includes HITL pause/diff (Story 2.6).
		runID := args[0]
		payload, err := hitlSvc.BuildStatus(cmd.Context(), runID)
		if err != nil {
			renderer.RenderError(err)
			return &silentErr{err}
		}
		out := runOutputFromStatusPayload(payload)
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

// runOutputFromStatusPayload maps service.StatusPayload to RunOutput,
// including Story 2.6's paused_position / decisions_summary / summary /
// changes_since_last_interaction fields. All HITL fields are elided via
// omitempty when the run is not in a HITL wait state.
func runOutputFromStatusPayload(p *service.StatusPayload) *RunOutput {
	out := &RunOutput{
		ID:        p.Run.ID,
		SCPID:     p.Run.SCPID,
		Stage:     string(p.Run.Stage),
		Status:    string(p.Run.Status),
		CreatedAt: p.Run.CreatedAt,
		UpdatedAt: p.Run.UpdatedAt,
		Summary:   p.Summary,
	}
	if p.PausedPosition != nil {
		out.PausedPosition = &PausedPositionOutput{
			Stage:                    string(p.PausedPosition.Stage),
			SceneIndex:               p.PausedPosition.SceneIndex,
			LastInteractionTimestamp: p.PausedPosition.LastInteractionTimestamp,
		}
	}
	if p.DecisionsSummary != nil {
		out.DecisionsSummary = &DecisionSummaryOutput{
			ApprovedCount: p.DecisionsSummary.ApprovedCount,
			RejectedCount: p.DecisionsSummary.RejectedCount,
			PendingCount:  p.DecisionsSummary.PendingCount,
		}
	}
	if len(p.ChangesSince) > 0 {
		out.ChangesSince = make([]ChangeOutput, len(p.ChangesSince))
		for i, c := range p.ChangesSince {
			out.ChangesSince[i] = ChangeOutput{
				Kind:      string(c.Kind),
				SceneID:   c.SceneID,
				Before:    c.Before,
				After:     c.After,
				Timestamp: c.Timestamp,
			}
		}
	}
	return out
}

