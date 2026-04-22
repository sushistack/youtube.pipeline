package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// FileAuditLogger writes NDJSON audit entries to {outputDir}/{runID}/audit.log.
// It is safe for concurrent use by multiple goroutines (TTS and image tracks
// run concurrently in Phase B via errgroup).
type FileAuditLogger struct {
	outputDir string
	mu        sync.Mutex
}

// NewFileAuditLogger creates a FileAuditLogger that writes audit logs under
// the given base output directory. Per-run subdirectories are resolved at
// Log time from entry.RunID.
func NewFileAuditLogger(outputDir string) *FileAuditLogger {
	return &FileAuditLogger{outputDir: outputDir}
}

// Log appends a single NDJSON line to {outputDir}/{entry.RunID}/audit.log.
// The file is opened with O_APPEND|O_CREATE|O_WRONLY and is never truncated.
// entry.Prompt is truncated to 2048 runes before writing.
func (l *FileAuditLogger) Log(ctx context.Context, entry domain.AuditEntry) error {
	// Truncate prompt to 2048 runes to bound log size.
	entry.Prompt = truncatePrompt(entry.Prompt, 2048)

	l.mu.Lock()
	defer l.mu.Unlock()

	runDir := filepath.Join(l.outputDir, entry.RunID)
	path := filepath.Join(runDir, "audit.log")

	// Ensure the run directory exists (idempotent).
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(entry); err != nil {
		return err
	}
	return nil
}

// truncatePrompt rune-aware truncates s to at most n runes.
func truncatePrompt(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	var idx int
	for i := range s {
		if utf8.RuneCountInString(s[:i]) >= n {
			break
		}
		idx = i
	}
	return s[:idx]
}
