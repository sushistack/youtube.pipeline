package testutil

import (
	"bytes"
	"log/slog"
	"testing"
)

// CaptureLog creates a *slog.Logger that writes JSON to a buffer.
// The buffer and logger are returned; the buffer can be inspected after the test.
func CaptureLog(t testing.TB) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, buf
}
