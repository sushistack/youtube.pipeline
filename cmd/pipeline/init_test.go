package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func TestInitCmd_CreatesDirectoryTree(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")

	cfgPath = configPath
	cmd := newInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify config.yaml created.
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.yaml not created: %v", err)
	}

	// Verify .env created.
	envPath := filepath.Join(tmp, ".env")
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf(".env not created: %v", err)
	}

	// Verify .env has correct permissions (0600).
	info, err := os.Stat(envPath)
	if err == nil && info.Mode().Perm() != 0600 {
		t.Errorf(".env permissions = %o, want 0600", info.Mode().Perm())
	}

	// Verify output includes expected paths.
	out := buf.String()
	if !strings.Contains(out, "Initialized youtube.pipeline") {
		t.Errorf("missing initialization message, got: %s", out)
	}
	if !strings.Contains(out, configPath) {
		t.Errorf("output should mention config path, got: %s", out)
	}
}

func TestInitCmd_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")

	// Write a custom config first.
	custom := []byte("writer_provider: custom-provider\n")
	if err := os.WriteFile(configPath, custom, 0644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	// Write a custom .env.
	envPath := filepath.Join(tmp, ".env")
	customEnv := []byte("DASHSCOPE_API_KEY=my-secret\n")
	if err := os.WriteFile(envPath, customEnv, 0600); err != nil {
		t.Fatalf("write custom env: %v", err)
	}

	cfgPath = configPath
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify config.yaml was NOT overwritten.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "custom-provider") {
		t.Error("config.yaml was overwritten — init must be idempotent")
	}

	// Verify .env was NOT overwritten.
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if !strings.Contains(string(envData), "my-secret") {
		t.Error(".env was overwritten — init must be idempotent")
	}
}

func TestInitCmd_DatabaseCreated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")

	// Write a config that uses temp paths.
	cfg := `output_dir: "` + filepath.Join(tmp, "output") + `"
db_path: "` + filepath.Join(tmp, "pipeline.db") + `"
data_dir: "` + filepath.Join(tmp, "data") + `"
`
	if err := os.WriteFile(configPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgPath = configPath
	cmd := newInitCmd()
	cmd.SetOut(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify database file exists.
	dbPath := filepath.Join(tmp, "pipeline.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("database not created: %v", err)
	}

	// Verify output directory exists.
	outputDir := filepath.Join(tmp, "output")
	if _, err := os.Stat(outputDir); err != nil {
		t.Errorf("output dir not created: %v", err)
	}
}
