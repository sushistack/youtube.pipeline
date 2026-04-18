package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"

	_ "github.com/ncruces/go-sqlite3/driver"
)

var (
	metricsWindow               int
	metricsCalibrationThreshold float64
	metricsRegressionFile       string
	metricsIdempotencyFile      string
	metricsClock   clock.Clock  = clock.RealClock{}
)

type metricsStoreAdapter struct {
	*db.RunStore
	*db.DecisionStore
}

func newMetricsStoreAdapter(runStore *db.RunStore, decisionStore *db.DecisionStore) *metricsStoreAdapter {
	return &metricsStoreAdapter{RunStore: runStore, DecisionStore: decisionStore}
}

func newMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Report rolling-window pipeline metrics (Day-90 gate).",
		Args:  cobra.NoArgs,
		RunE:  runMetrics,
	}
	cmd.Flags().IntVar(&metricsWindow, "window", 25, "rolling-window size (number of most-recent completed runs)")
	cmd.Flags().Float64Var(&metricsCalibrationThreshold, "calibration-threshold", 0.70, "Critic pass cutoff used for kappa + defect-escape classification")
	cmd.Flags().StringVar(&metricsRegressionFile, "regression-rate", "", "path to a text file containing a single float — the Golden-eval detection rate; when omitted the metric reports Unavailable")
	cmd.Flags().StringVar(&metricsIdempotencyFile, "idempotency-rate", "", "path to a text file containing a single float — the stage-level resume idempotency rate; when omitted the metric reports Unavailable")
	return cmd
}

func runMetrics(cmd *cobra.Command, _ []string) error {
	if metricsWindow > 1000 {
		renderer := newRenderer(cmd.OutOrStdout())
		err := fmt.Errorf("--window must be ≤ 1000, got %d (PRD NFR-O4 performance ceiling)", metricsWindow)
		renderer.RenderError(err)
		return &silentErr{err}
	}
	cfg, err := config.Load(cfgPath, config.DefaultEnvPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	runStore := db.NewRunStore(database)
	decisionStore := db.NewDecisionStore(database)
	svc := service.NewMetricsService(newMetricsStoreAdapter(runStore, decisionStore), metricsClock)
	renderer := newRenderer(cmd.OutOrStdout())

	regression, err := readOptionalFloatFile(metricsRegressionFile)
	if err != nil {
		renderer.RenderError(err)
		return &silentErr{err}
	}
	idempotency, err := readOptionalFloatFile(metricsIdempotencyFile)
	if err != nil {
		renderer.RenderError(err)
		return &silentErr{err}
	}

	report, err := svc.Report(cmd.Context(), metricsWindow, metricsCalibrationThreshold, regression, idempotency)
	if err != nil {
		renderer.RenderError(err)
		return &silentErr{err}
	}
	renderer.RenderSuccess(report)
	return nil
}

func readOptionalFloatFile(path string) (*float64, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Strip UTF-8 BOM (written by some Windows editors) before trimming whitespace.
	s := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("parse %s: file is empty or contains only whitespace", path)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("parse %s: expected a decimal number, got %q", path, s)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("parse %s: NaN and Inf are not valid rates", path)
	}
	if v < 0 || v > 1 {
		return nil, fmt.Errorf("parse %s: rate must be in [0, 1], got %g", path, v)
	}
	return &v, nil
}
