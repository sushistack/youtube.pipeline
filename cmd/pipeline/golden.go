package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
)

// goldenProjectRoot can be overridden in tests to point at a temp directory.
// When empty, findProjectRoot() is called at command run time.
var goldenProjectRoot string

func resolveGoldenRoot() (string, error) {
	if goldenProjectRoot != "" {
		return goldenProjectRoot, nil
	}
	return findProjectRoot()
}

func newGoldenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "golden",
		Short:        "Manage Golden eval set for the Critic",
		SilenceUsage: true,
	}
	cmd.AddCommand(newGoldenAddCmd())
	cmd.AddCommand(newGoldenListCmd())
	return cmd
}

func newGoldenAddCmd() *cobra.Command {
	var positivePath string
	var negativePath string

	cmd := &cobra.Command{
		Use:          "add",
		Short:        "Add a positive/negative fixture pair to the Golden set",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			renderer := newRenderer(cmd.OutOrStdout())
			root, err := resolveGoldenRoot()
			if err != nil {
				return fmt.Errorf("find project root: %w", err)
			}

			meta, err := eval.AddPair(root, positivePath, negativePath, time.Now().UTC())
			if err != nil {
				renderer.RenderError(err)
				return &silentErr{err}
			}

			pairs, err := eval.ListPairs(root)
			if err != nil {
				return fmt.Errorf("list pairs: %w", err)
			}

			renderer.RenderSuccess(&GoldenAddOutput{
				Index:        meta.Index,
				CreatedAt:    meta.CreatedAt.Format(time.RFC3339),
				PositivePath: meta.PositivePath,
				NegativePath: meta.NegativePath,
				PairCount:    len(pairs),
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&positivePath, "positive", "", "path to the positive fixture JSON")
	cmd.Flags().StringVar(&negativePath, "negative", "", "path to the negative fixture JSON")
	return cmd
}

func newGoldenListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "List all Golden eval pairs",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			renderer := newRenderer(cmd.OutOrStdout())
			root, err := resolveGoldenRoot()
			if err != nil {
				return fmt.Errorf("find project root: %w", err)
			}

			pairs, err := eval.ListPairs(root)
			if err != nil {
				renderer.RenderError(err)
				return &silentErr{err}
			}

			rows := make([]GoldenPairRow, len(pairs))
			for i, p := range pairs {
				rows[i] = GoldenPairRow{
					Index:        p.Index,
					CreatedAt:    p.CreatedAt.Format(time.RFC3339),
					PositivePath: p.PositivePath,
					NegativePath: p.NegativePath,
				}
			}
			renderer.RenderSuccess(&GoldenListOutput{Pairs: rows})
			return nil
		},
	}
}

// findProjectRoot walks upward from the working directory until it finds
// a go.mod file. Production-safe; does not import internal/testutil.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("project root not found (no go.mod in any parent directory)")
		}
		dir = parent
	}
}
