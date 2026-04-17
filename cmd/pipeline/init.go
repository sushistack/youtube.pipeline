package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"gopkg.in/yaml.v3"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize project configuration, database, and directories",
		RunE:  runInit,
	}
}

func runInit(cmd *cobra.Command, _ []string) error {
	cfgDir := filepath.Dir(cfgPath)

	// 1. Create config directory.
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// 2. Write config.yaml if it doesn't exist (idempotent).
	if err := writeConfigIfNotExists(cfgPath); err != nil {
		return err
	}

	// 3. Write .env template if it doesn't exist (idempotent).
	envPath := config.DefaultEnvPath()
	if filepath.Dir(cfgPath) != config.DefaultConfigDir() {
		envPath = filepath.Join(filepath.Dir(cfgPath), ".env")
	}
	if err := writeEnvIfNotExists(envPath); err != nil {
		return err
	}

	// 4. Load the config to get paths.
	cfg, err := config.Load(cfgPath, envPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 5. Create output directory.
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// 6. Initialize SQLite database.
	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	database.Close()

	renderer := newRenderer(cmd.OutOrStdout())
	initOutput := InitOutput{
		Config:   cfgPath,
		Env:      envPath,
		Database: cfg.DBPath,
		Output:   cfg.OutputDir,
	}
	renderer.RenderSuccess(&initOutput)
	return nil
}

func writeConfigIfNotExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}

	cfg := domain.DefaultConfig()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func writeEnvIfNotExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}

	const envTemplate = `# youtube.pipeline — API keys (secrets)
# Fill in your API keys below.

DASHSCOPE_API_KEY=
DEEPSEEK_API_KEY=
GEMINI_API_KEY=
`
	if err := os.WriteFile(path, []byte(envTemplate), 0600); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	return nil
}
