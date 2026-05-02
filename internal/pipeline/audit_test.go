package pipeline_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

func TestFileAuditLogger_Log_AppendsNDJSON(t *testing.T) {
	outputDir := t.TempDir()
	logger := pipeline.NewFileAuditLogger(outputDir)
	ctx := context.Background()

	entry := domain.AuditEntry{
		RunID:     "test-run-1",
		EventType: domain.AuditEventTTSSynthesis,
		Stage:     "tts",
		Provider:  "dashscope",
		Model:     "qwen3-tts-flash",
		Prompt:    "hello world",
		CostUSD:   0.0012,
	}

	if err := logger.Log(ctx, entry); err != nil {
		t.Fatalf("first Log call: %v", err)
	}
	if err := logger.Log(ctx, entry); err != nil {
		t.Fatalf("second Log call: %v", err)
	}

	path := filepath.Join(outputDir, "test-run-1", "audit.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var decoded domain.AuditEntry
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		if decoded.EventType != domain.AuditEventTTSSynthesis {
			t.Errorf("line %d: expected event_type=tts_synthesis, got %q", i, decoded.EventType)
		}
	}
}

func TestFileAuditLogger_Log_FileNotTruncated(t *testing.T) {
	outputDir := t.TempDir()
	logger := pipeline.NewFileAuditLogger(outputDir)
	ctx := context.Background()

	// First entry.
	entry1 := domain.AuditEntry{
		RunID:     "test-run-2",
		EventType: domain.AuditEventTextGeneration,
		Stage:     "writer",
		Provider:  "deepseek",
		Model:     "deepseek-v4-flash",
		Prompt:    "write a story",
		CostUSD:   0.005,
	}
	if err := logger.Log(ctx, entry1); err != nil {
		t.Fatalf("first Log call: %v", err)
	}

	// Read file size after first entry.
	path := filepath.Join(outputDir, "test-run-2", "audit.log")
	fi1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first log: %v", err)
	}

	// Second entry.
	entry2 := domain.AuditEntry{
		RunID:     "test-run-2",
		EventType: domain.AuditEventImageGeneration,
		Stage:     "image",
		Provider:  "dashscope",
		Model:     "qwen-max-vl",
		Prompt:    "generate an image",
		CostUSD:   0.02,
	}
	if err := logger.Log(ctx, entry2); err != nil {
		t.Fatalf("second Log call: %v", err)
	}

	// File size should have grown, not shrunk (file not truncated).
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after second log: %v", err)
	}
	if fi2.Size() <= fi1.Size() {
		t.Errorf("file size did not grow: before=%d after=%d", fi1.Size(), fi2.Size())
	}

	// Verify both entries are valid NDJSON.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit.log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		var dec domain.AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &dec); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", lineCount, err)
		}
		lineCount++
	}
	if lineCount != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d", lineCount)
	}
}

func TestFileAuditLogger_Log_PromptTruncated(t *testing.T) {
	outputDir := t.TempDir()
	logger := pipeline.NewFileAuditLogger(outputDir)
	ctx := context.Background()

	longPrompt := strings.Repeat("a", 3000)
	entry := domain.AuditEntry{
		RunID:     "test-run-3",
		EventType: domain.AuditEventTextGeneration,
		Stage:     "writer",
		Provider:  "deepseek",
		Model:     "deepseek-v4-flash",
		Prompt:    longPrompt,
		CostUSD:   0.01,
	}

	if err := logger.Log(ctx, entry); err != nil {
		t.Fatalf("Log call: %v", err)
	}

	path := filepath.Join(outputDir, "test-run-3", "audit.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}

	var decoded domain.AuditEntry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Prompt) > 2048 {
		t.Fatalf("prompt length %d exceeds 2048 limit", len(decoded.Prompt))
	}
}
