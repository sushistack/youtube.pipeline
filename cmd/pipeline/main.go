package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
)

var (
	cfgPath    string
	jsonOutput bool
)

func newRenderer(w io.Writer) Renderer {
	if jsonOutput {
		return NewJSONRenderer(w)
	}
	return NewHumanRenderer(w)
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "pipeline",
		Short: "youtube.pipeline — SCP video generation tool",
	}

	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", config.DefaultConfigPath(), "path to config.yaml")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if jsonOutput {
			cmd.Root().SilenceErrors = true
			cmd.Root().SilenceUsage = true
		}
		return nil
	}

	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.AddCommand(newCreateCmd())
	rootCmd.AddCommand(newCancelCmd())
	rootCmd.AddCommand(newResumeCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newMetricsCmd())
	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newGoldenCmd())

	if err := rootCmd.Execute(); err != nil {
		// silentErr means the command already rendered output; skip re-rendering.
		var se *silentErr
		if errors.As(err, &se) {
			os.Exit(1)
		}
		if jsonOutput {
			renderer := NewJSONRenderer(os.Stderr)
			renderer.RenderError(err)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
