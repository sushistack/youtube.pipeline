package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
)

// errDoctorFailed is returned when one or more doctor checks fail.
var errDoctorFailed = errors.New("doctor checks failed")

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "doctor",
		Short:        "Check prerequisites and configuration health",
		RunE:         runDoctor,
		SilenceUsage: true,
	}
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	renderer := newRenderer(cmd.OutOrStdout())
	envPath := config.DefaultEnvPath()

	cfg, err := config.Load(cfgPath, envPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	registry := config.DefaultRegistry()
	results := registry.RunAll(cfg)

	checks := make([]CheckResult, len(results))
	for i, r := range results {
		checks[i] = CheckResult{
			Name:    r.Name,
			Passed:  r.Passed,
			Message: r.Message,
		}
	}

	doctorOutput := DoctorOutput{
		Checks: checks,
		Passed: config.AllPassed(results),
	}

	// Golden staleness warnings are advisory — they do not affect exit code.
	if root, err := resolveGoldenRoot(); err == nil {
		thresholdDays := cfg.GoldenStalenessDays
		if thresholdDays < 1 {
			thresholdDays = 30
		}
		if status, err := eval.EvaluateFreshness(root, time.Now().UTC(), thresholdDays); err == nil {
			doctorOutput.Warnings = status.Warnings
		}
	}

	renderer.RenderSuccess(&doctorOutput)

	if !doctorOutput.Passed {
		return &silentErr{errDoctorFailed}
	}
	return nil
}
