// Package fi provides fault-injection adapters for pipeline write paths.
// Adapters are constructor-injected via test wiring and have no production
// callers — they exist solely to drive reliability scenarios such as
// SMOKE-05 (metadata+manifest pair atomicity) without modifying the
// production code under test.
package fi

import (
	"sync"

	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// FailDecision is the per-call hook used by FaultingFileWriter. It receives
// the destination path and a 1-based attempt counter for that path. Returning
// a non-nil error short-circuits the call without invoking the underlying
// writer; returning nil delegates to the wrapped writer.
type FailDecision func(path string, attempt int) error

// NewFaultingFileWriter wraps delegate with the supplied fault hook. Pass
// pipeline.DefaultAtomicWriter as delegate for production-shaped behavior on
// the success path. The returned FileWriter is safe for concurrent use.
func NewFaultingFileWriter(delegate pipeline.FileWriter, decide FailDecision) pipeline.FileWriter {
	if delegate == nil {
		delegate = pipeline.DefaultAtomicWriter
	}
	var mu sync.Mutex
	attempts := map[string]int{}
	return func(path string, payload []byte) error {
		mu.Lock()
		attempts[path]++
		n := attempts[path]
		mu.Unlock()
		if decide != nil {
			if err := decide(path, n); err != nil {
				return err
			}
		}
		return delegate(path, payload)
	}
}
