package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
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

Phase B resume (image/tts) preserves successful sibling-track artifacts on
mixed failure when safe, otherwise falls back to a clean-slate rerun.
Other stages preserve segments.

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
	decisionStore := db.NewDecisionStore(database)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	engine := pipeline.NewEngine(store, segStore, decisionStore, clock.RealClock{}, cfg.OutputDir, logger)
	engine.SetHITLSessionStore(newHITLSessionStoreAdapter(decisionStore))
	limiterFactory, limiterErr := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 10, MaxConcurrent: 2, AcquireTimeout: 30 * time.Second},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 30 * time.Second},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 30 * time.Second},
	}, clock.RealClock{})
	if limiterErr != nil {
		return fmt.Errorf("build limiter factory: %w", limiterErr)
	}
	// CharacterResolver is required for image_track Edit calls on character
	// shots. resume.go wires it identically to serve.go so a resume at
	// StageImage produces visually-consistent regenerations.
	characterCache := db.NewCharacterCacheStore(database)
	characterClient := service.NewDuckDuckGoClient(nil)
	characterSvc := service.NewCharacterService(store, characterCache, characterClient)

	if phaseBRunner, err := buildPhaseBRunner(cfg, os.Getenv("DASHSCOPE_API_KEY"), limiterFactory, store, segStore, characterSvc, logger); err == nil {
		engine.SetPhaseBExecutor(phaseBRunner)
	} else {
		logger.Warn("phase b runner unavailable (resume retries disabled for phase b)", "error", err.Error())
	}

	// Phase C has no LLM dependency, so wiring is unconditional. A resume at
	// StageAssemble or StageMetadataAck must be able to re-run assembly and
	// regenerate compliance files; without these the engine would reject the
	// retry with a validation error.
	if phaseCRunner, metaBuilder, err := buildPhaseCRuntime(cfg, store, segStore, logger); err == nil {
		engine.SetPhaseCRunner(phaseCRunner)
		engine.SetPhaseCMetadataBuilder(metaBuilder)
	} else {
		logger.Warn("phase c runtime unavailable (resume retries disabled for phase c)", "error", err.Error())
	}
	svc := service.NewRunService(store, engine)
	hitlSvc := service.NewHITLService(store, decisionStore, logger)

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

	// Story 2.6: surface the post-resume HITL summary + FR50 diff when
	// applicable. Non-HITL runs return zero-valued fields which stay elided
	// via omitempty.
	if payload, perr := hitlSvc.BuildStatus(cmd.Context(), runID); perr == nil && payload != nil {
		out.Summary = payload.Summary
		if payload.PausedPosition != nil {
			out.Run.PausedPosition = &PausedPositionOutput{
				Stage:                    string(payload.PausedPosition.Stage),
				SceneIndex:               payload.PausedPosition.SceneIndex,
				LastInteractionTimestamp: payload.PausedPosition.LastInteractionTimestamp,
			}
		}
		if len(payload.ChangesSince) > 0 {
			out.ChangesSince = make([]ChangeOutput, len(payload.ChangesSince))
			for i, c := range payload.ChangesSince {
				out.ChangesSince[i] = ChangeOutput{
					Kind:      string(c.Kind),
					SceneID:   c.SceneID,
					Before:    c.Before,
					After:     c.After,
					Timestamp: c.Timestamp,
				}
			}
		}
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
