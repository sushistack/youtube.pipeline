package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheEnvelopeVersion is the on-disk wire-format version for cached
// deterministic-agent payloads. Bump only when the envelope itself
// changes shape; payload schema bumps live in FingerprintInputs.
const CacheEnvelopeVersion = 1

// CacheEnvelope wraps a deterministic-agent payload with the metadata
// needed to invalidate it when prompts, fewshots, models, providers, or
// schema versions change. The envelope is written atomically (tmp →
// rename) under {runDir}/_cache/{stage}_cache.json.
//
// Inputs is recorded alongside Fingerprint so a stale-cache load can
// diagnose *which* of the six factors changed without having to guess.
// This is the diff surface the operator sees in GET /cache responses.
type CacheEnvelope struct {
	EnvelopeVersion int               `json:"envelope_version"`
	Fingerprint     string            `json:"fingerprint"`    // 64-char lowercase sha256 hex of Inputs
	SourceVersion   string            `json:"source_version"` // mirrors Inputs.SourceVersion for fast probe
	Inputs          FingerprintInputs `json:"inputs"`
	WrittenAt       string            `json:"written_at"` // RFC3339Nano
	Payload         json.RawMessage   `json:"payload"`
}

// FingerprintInputs is the set of factors that determine cache validity.
// Any change to one of these fields invalidates a stale cache hit. The
// inputs are JSON-marshaled in struct-field-declaration order, then
// sha256-hashed; use ComputeFingerprint to produce the canonical hex.
//
// SchemaVersion is the envelope-layer stable identifier for the payload
// struct shape — distinct from SourceVersion which gates payload
// semantics. Bump SchemaVersion when the Go struct layout changes;
// SourceVersion when the agent's behavior changes.
type FingerprintInputs struct {
	SourceVersion     string `json:"source_version"`
	PromptTemplateSHA string `json:"prompt_template_sha"`
	FewshotSHA        string `json:"fewshot_sha"`
	Model             string `json:"model"`
	Provider          string `json:"provider"`
	SchemaVersion     string `json:"schema_version"`
}

// ComputeFingerprint produces the canonical sha256 hex fingerprint for
// the given inputs. JSON ordering is fixed by struct-field declaration
// order in Go, so the hash is deterministic across machines.
func ComputeFingerprint(in FingerprintInputs) string {
	data, err := json.Marshal(in)
	if err != nil {
		// Marshal of a plain struct of strings cannot fail; sentinel
		// keeps Phase A robust against hypothetical refactors that
		// break the field set — a "##error##" fingerprint can never
		// match a real one, so caches always miss instead of crashing.
		return "##fingerprint-marshal-error##"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// StalenessReason classifies why a CacheEnvelope was rejected on load.
// "" means the envelope was fresh (cache hit). Non-empty values are
// surfaced via GET /api/runs/{id}/cache so the operator can see *why*
// a stale envelope can no longer be reused.
type StalenessReason string

const (
	StaleSourceVersionMismatch StalenessReason = "source_version_mismatch"
	StalePromptTemplateChanged StalenessReason = "prompt_template_changed"
	StaleFewshotChanged        StalenessReason = "fewshot_changed"
	StaleModelChanged          StalenessReason = "model_changed"
	StaleProviderChanged       StalenessReason = "provider_changed"
	StaleSchemaChanged         StalenessReason = "schema_changed"
	StaleEnvelopeCorrupt       StalenessReason = "envelope_corrupt"
)

// LoadEnvelope reads an envelope file from disk and returns it alongside
// a typed staleness reason. The reason is "" iff every fingerprint input
// matches. Returns (nil, "", os.ErrNotExist) for a missing file so
// callers can distinguish "no cache" from "stale cache".
//
// A file that exists but lacks an envelope_version field (legacy flat
// payload) is reported as StaleEnvelopeCorrupt — the spec policy is
// "silent miss, no migration code", so callers should treat any non-empty
// reason as a cache miss.
func LoadEnvelope(path string, expected FingerprintInputs) (*CacheEnvelope, StalenessReason, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var env CacheEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, StaleEnvelopeCorrupt, nil
	}
	// EnvelopeVersion==0 means the file lacks the field (zero value) —
	// treat as legacy envelopeless payload, silent miss.
	if env.EnvelopeVersion == 0 {
		return nil, StaleEnvelopeCorrupt, nil
	}
	if env.EnvelopeVersion != CacheEnvelopeVersion {
		return &env, StaleEnvelopeCorrupt, nil
	}
	if !isHexFingerprint(env.Fingerprint) {
		return &env, StaleEnvelopeCorrupt, nil
	}

	if env.Fingerprint == ComputeFingerprint(expected) {
		return &env, "", nil
	}
	return &env, classifyMismatch(env.Inputs, expected), nil
}

// classifyMismatch returns the typed staleness reason for two
// FingerprintInputs whose canonical hashes do not match. Field order is
// the operator's natural reading order: payload semantic version first
// (SourceVersion / SchemaVersion), then prompt iteration surfaces
// (template / fewshot), then infra knobs (model / provider). When more
// than one field has changed we report the highest-priority change so
// the operator's diagnostic mental model isn't fragmented.
func classifyMismatch(recorded, expected FingerprintInputs) StalenessReason {
	switch {
	case recorded.SourceVersion != expected.SourceVersion:
		return StaleSourceVersionMismatch
	case recorded.SchemaVersion != expected.SchemaVersion:
		return StaleSchemaChanged
	case recorded.PromptTemplateSHA != expected.PromptTemplateSHA:
		return StalePromptTemplateChanged
	case recorded.FewshotSHA != expected.FewshotSHA:
		return StaleFewshotChanged
	case recorded.Model != expected.Model:
		return StaleModelChanged
	case recorded.Provider != expected.Provider:
		return StaleProviderChanged
	}
	// Hash mismatch with all known fields equal — implies an unknown
	// field drift (e.g. someone added a field to FingerprintInputs but
	// didn't update this switch). Treat as corrupt so we regenerate.
	return StaleEnvelopeCorrupt
}

// WriteEnvelope serializes payload as a CacheEnvelope and writes it
// atomically (tmp → fsync → rename) to path. Creates parent directories
// as needed. The fsync step matches finalize_phase_a.go:55-86 — without
// it a power loss between the kernel buffer flush and the disk write can
// leave a zero-length tmp file that survives the rename, silently
// corrupting the resume cache. Errors are returned (not logged) so
// callers can decide fatal-vs-warn — the existing Phase A policy is to
// log and continue.
func WriteEnvelope(path string, inputs FingerprintInputs, payload any, now time.Time) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cache envelope: marshal payload: %w", err)
	}
	env := CacheEnvelope{
		EnvelopeVersion: CacheEnvelopeVersion,
		Fingerprint:     ComputeFingerprint(inputs),
		SourceVersion:   inputs.SourceVersion,
		Inputs:          inputs,
		WrittenAt:       now.Format(time.RFC3339Nano),
		Payload:         raw,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("cache envelope: marshal envelope: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cache envelope: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cache envelope: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("cache envelope: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("cache envelope: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("cache envelope: close: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("cache envelope: chmod: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("cache envelope: rename: %w", err)
	}
	return nil
}

// CacheStageFile returns the canonical on-disk path for a deterministic
// stage's cache envelope under runDir. Centralized so callers (Phase A
// runner, API handler, rewind cleanup) cannot drift on the layout.
func CacheStageFile(runDir, stageFilename string) string {
	return filepath.Join(runDir, "_cache", stageFilename)
}

// CacheDir returns the resume cache directory for a run.
func CacheDir(runDir string) string {
	return filepath.Join(runDir, "_cache")
}

// TracesDir returns the per-attempt LLM trace directory for a run. Only
// populated when observability.debug_traces is on; callers must create
// the directory lazily (FileTraceWriter does this on first write).
func TracesDir(runDir string) string {
	return filepath.Join(runDir, "traces")
}

// isHexFingerprint reports whether s is a 64-char lowercase hex string —
// the shape ComputeFingerprint produces. Anything else is treated as a
// corrupt envelope.
func isHexFingerprint(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
