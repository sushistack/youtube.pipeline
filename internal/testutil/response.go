package testutil

import (
	"encoding/json"
	"io"
	"testing"
)

// ReadJSON decodes JSON from r into a value of type T.
// Fails the test immediately if decoding fails.
func ReadJSON[T any](t testing.TB, r io.Reader) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	return v
}
