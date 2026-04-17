package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
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

	renderer.RenderSuccess(&doctorOutput)

	if !doctorOutput.Passed {
		return &silentErr{errDoctorFailed}
	}
	return nil
}
