# Story 9.3: Compliance Audit Logging

Status: done

## Story

As a developer,
I want a permanent record of all LLM and media provider interactions,
so that I can verify compliance and debug provider shifts.

## Acceptance Criteria

### AC-1: Audit log captures every media generation / narration call

**Given** any media generation or narration task fires (Phase A text generation, Phase B image generation, Phase B TTS synthesis)
**When** the call completes (success or after retries)
**Then** an audit entry is appended to `{run_dir}/audit.log` containing:
- `timestamp` (RFC3339Nano)
- `event_type` (`text_generation` | `image_generation` | `tts_synthesis`)
- `run_id`
- `stage` (e.g., `"tts"`, `"image"`, `"writer"`, `"critic"`)
- `provider` (e.g., `"dashscope"`, `"deepseek"`, `"gemini"`)
- `model` (model identifier string)
- `prompt` (truncated to 2048 chars to bound log size)
- `cost_usd` (float64)

**And** entries are NDJSON (one JSON object per line, UTF-8)
**And** the file is opened with `O_APPEND|O_CREATE|O_WRONLY` — never truncated on resume

**Tests:**
- Integration — after a full Phase B TTS run, `audit.log` exists in run dir and contains one entry per scene with correct fields
- Integration — after a Phase B image run, `audit.log` has `image_generation` entries with correct provider/model/cost
- Unit — `FileAuditLogger.Log` appends NDJSON line; second call adds second line; file is not truncated

---

### AC-2: Blocked voice-ID rejection before any API call

**Given** a TTS request where `cfg.TTSVoice` is present in `PipelineConfig.BlockedVoiceIDs`
**When** the TTS track's per-scene synthesis loop reaches `invokeTTSProvider`
**Then** the call is rejected **before** any external API call is made
**And** a `voice_blocked` audit entry is appended to `audit.log` with `blocked_id` set to the rejected voice ID
**And** the stage fails with error: `"Voice profile '<id>' is blocked by compliance policy"`
**And** the error wraps `domain.ErrValidation` so the pipeline engine classifies it as operator-fixable (not a transient error)

**Given** an operator updates `config.yaml` to remove the voice ID from `blocked_voice_ids` (or change `tts_voice`)
**When** the run is resumed via `POST /api/runs/{id}/resume`
**Then** the TTS stage re-runs and succeeds (existing TTS artifacts were cleaned by `CleanStageArtifacts` on the prior failure)

**Tests:**
- Unit — `runTTSTrack` with a matching `BlockedVoiceIDs` entry: `TTSSynthesizer.Synthesize` is NEVER called; error message contains exact phrase `"Voice profile '<id>' is blocked by compliance policy"`; `audit.log` has a `voice_blocked` entry
- Integration — resume flow after voice-ID fix: TTS stage completes, `audit.log` contains the original `voice_blocked` entry AND the new `tts_synthesis` entries (file is append-only, old entry preserved)

---

## Tasks / Subtasks

- [ ] Task 1: Define `AuditEntry` + `AuditLogger` interface in domain (AC: 1, 2)
  - [ ] 1.1 Create `internal/domain/audit.go` with `AuditEventType` constants, `AuditEntry` struct, `AuditLogger` interface
  - [ ] 1.2 Add `BlockedVoiceIDs []string` to `PipelineConfig` in `internal/domain/config.go`

- [ ] Task 2: Implement `FileAuditLogger` (AC: 1)
  - [ ] 2.1 Create `internal/pipeline/audit.go` with `FileAuditLogger` struct
  - [ ] 2.2 `NewFileAuditLogger(outputDir string)` constructor; `Log(ctx, entry) error` method writes NDJSON to `{outputDir}/{entry.RunID}/audit.log`
  - [ ] 2.3 Protect file open+write with `sync.Mutex` (TTS and image tracks run concurrently in `errgroup`)
  - [ ] 2.4 Write unit tests in `internal/pipeline/audit_test.go`

- [ ] Task 3: Voice blocklist check + audit in TTS track (AC: 1, 2)
  - [ ] 3.1 Add `BlockedVoiceIDs []string` and `AuditLogger domain.AuditLogger` to `TTSTrackConfig`
  - [ ] 3.2 In `runTTSTrack`, before the scene loop calls `invokeTTSProvider`: check if `cfg.TTSVoice` is in `cfg.BlockedVoiceIDs`; if so, write `voice_blocked` audit entry and return the compliance error
  - [ ] 3.3 After each successful `invokeTTSProvider` call, write `tts_synthesis` audit entry (non-fatal; log error, do not abort)
  - [ ] 3.4 Update `NewTTSTrack` validation: `AuditLogger` nil is allowed (write a no-op guard to `Log`)
  - [ ] 3.5 Add/update tests in `internal/pipeline/tts_track_test.go`

- [ ] Task 4: Audit in image track (AC: 1)
  - [ ] 4.1 Add `AuditLogger domain.AuditLogger` to `ImageTrackConfig`
  - [ ] 4.2 After each successful image `Generate` / `Edit` call, write `image_generation` audit entry (non-fatal)
  - [ ] 4.3 Add/update tests in `internal/pipeline/image_track_test.go`

- [ ] Task 5: Audit in Phase A text agents (AC: 1)
  - [ ] 5.1 Add `AuditLogger domain.AuditLogger` to `TextAgentConfig` (in `internal/pipeline/agents/writer.go`)
  - [ ] 5.2 In `NewWriter`, `NewPostWriterCritic`, `NewReviewer`, `NewCritic` (and other agents that call `gen.Generate`): after successful generate call, write `text_generation` entry; `stage` field = agent stage name (e.g., `"writer"`, `"critic"`)
  - [ ] 5.3 `AuditLogger` nil is allowed — skip logging when nil (avoids breaking all existing tests that don't set AuditLogger)
  - [ ] 5.4 Update `PhaseARunner` to thread `AuditLogger` into each `TextAgentConfig` at construction time

- [ ] Task 6: Wire up at construction sites (AC: 1, 2)
  - [ ] 6.1 In `cmd/` or wherever `TTSTrackConfig`, `ImageTrackConfig`, and `PhaseARunner` are constructed, wire in a `FileAuditLogger` instance and `cfg.BlockedVoiceIDs`
  - [ ] 6.2 Confirm `CleanStageArtifacts` does NOT touch `audit.log` (already true — no change needed, just verify)

- [ ] Task 7: Integration tests (AC: 1, 2)
  - [ ] 7.1 Integration test: full Phase B mock run → verify `audit.log` in run dir contains expected entries
  - [ ] 7.2 Integration test: blocked voice-ID → verify Synthesize not called, correct error text, audit entry written
  - [ ] 7.3 Integration test: resume after voice-ID fix → old audit entries preserved, new ones appended

---

## Dev Notes

### New files to create

| File | Purpose |
|------|---------|
| `internal/domain/audit.go` | `AuditEventType`, `AuditEntry`, `AuditLogger` interface |
| `internal/pipeline/audit.go` | `FileAuditLogger` implementation |
| `internal/pipeline/audit_test.go` | Unit tests for FileAuditLogger |

### Files to modify

| File | Change |
|------|--------|
| `internal/domain/config.go` | Add `BlockedVoiceIDs []string` to `PipelineConfig`; add to `DefaultConfig()` as empty slice |
| `internal/pipeline/tts_track.go` | Add `BlockedVoiceIDs`, `AuditLogger` to `TTSTrackConfig`; pre-call blocklist check; post-call audit write |
| `internal/pipeline/image_track.go` | Add `AuditLogger` to `ImageTrackConfig`; post-call audit write |
| `internal/pipeline/agents/writer.go` | Add `AuditLogger` to `TextAgentConfig`; post-generate audit write |
| `internal/pipeline/agents/critic.go` | Call `cfg.AuditLogger.Log(...)` after successful `gen.Generate` calls |
| `internal/pipeline/agents/reviewer.go` | Same pattern |
| `internal/pipeline/phase_a.go` | Thread AuditLogger through into TextAgentConfig at construction |

### Domain layer placement (critical — prevents circular imports)

`AuditLogger` interface and `AuditEntry` struct MUST live in `internal/domain/audit.go`, NOT `internal/pipeline/`. This is because:
- `internal/pipeline/agents/` imports `internal/domain` ✓
- `internal/pipeline/agents/` cannot import `internal/pipeline` (circular) ✗

Follow the existing pattern: `domain.TTSSynthesizer`, `domain.TextGenerator` are all in domain; their implementations are in `internal/llmclient/`.

### AuditEntry struct

```go
// internal/domain/audit.go

type AuditEventType string

const (
    AuditEventTextGeneration  AuditEventType = "text_generation"
    AuditEventImageGeneration AuditEventType = "image_generation"
    AuditEventTTSSynthesis    AuditEventType = "tts_synthesis"
    AuditEventVoiceBlocked    AuditEventType = "voice_blocked"
)

type AuditEntry struct {
    Timestamp time.Time      `json:"timestamp"`
    EventType AuditEventType `json:"event_type"`
    RunID     string         `json:"run_id"`
    Stage     string         `json:"stage"`
    Provider  string         `json:"provider"`
    Model     string         `json:"model"`
    Prompt    string         `json:"prompt"`    // truncated to 2048 chars
    CostUSD   float64        `json:"cost_usd"`
    BlockedID string         `json:"blocked_id,omitempty"`
}

type AuditLogger interface {
    Log(ctx context.Context, entry AuditEntry) error
}
```

### FileAuditLogger implementation notes

```go
// internal/pipeline/audit.go

type FileAuditLogger struct {
    outputDir string
    mu        sync.Mutex
}

func NewFileAuditLogger(outputDir string) *FileAuditLogger { ... }

func (l *FileAuditLogger) Log(ctx context.Context, entry domain.AuditEntry) error {
    // 1. Truncate entry.Prompt to 2048 chars
    // 2. l.mu.Lock(); defer l.mu.Unlock()
    // 3. Open with os.OpenFile(path, O_APPEND|O_CREATE|O_WRONLY, 0o644)
    // 4. json.NewEncoder(f).Encode(entry)  — Encode appends '\n' automatically
    // 5. f.Close()
}
```

**Why mutex?** Phase B runs image + TTS tracks concurrently via `errgroup` inside `PhaseBRunner.Run`. Both tracks share the same `FileAuditLogger` instance and write to the same `audit.log` file. Without a mutex, concurrent writes can produce interleaved partial JSON lines.

**Why open+close per write?** Avoids holding a file handle across the entire pipeline run. On stage resume, the file is opened fresh for append. `O_APPEND` guarantees atomic seek-to-end on Linux for small writes.

### BlockedVoiceIDs in config.go

```go
// Add to PipelineConfig struct:
BlockedVoiceIDs []string `yaml:"blocked_voice_ids" mapstructure:"blocked_voice_ids"`

// DefaultConfig(): BlockedVoiceIDs is nil or empty slice — no voices blocked by default
```

Operator adds entries to `~/.youtube-pipeline/config.yaml`:
```yaml
blocked_voice_ids:
  - some-voice-profile-id
  - another-blocked-id
```

### Blocklist check in tts_track.go

Add this block to `runTTSTrack`, just before the scene-iteration loop (or just before calling `invokeTTSProvider` for the first scene):

```go
// FR47: check voice blocklist before any API call
for _, blockedID := range cfg.BlockedVoiceIDs {
    if cfg.TTSVoice == blockedID {
        if cfg.AuditLogger != nil {
            _ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
                Timestamp: clk.Now(),
                EventType: domain.AuditEventVoiceBlocked,
                RunID:     req.RunID,
                Stage:     string(domain.StageTTS),
                Provider:  "dashscope",
                Model:     cfg.TTSModel,
                BlockedID: blockedID,
            })
        }
        return TTSTrackResult{}, fmt.Errorf(
            "Voice profile '%s' is blocked by compliance policy: %w",
            blockedID, domain.ErrValidation,
        )
    }
}
```

**Exact error message contract** (pinned by AC-2 and tests):
`"Voice profile '<id>' is blocked by compliance policy"`

### Audit write after successful TTS call (non-fatal pattern)

```go
// After UpsertTTSArtifact, inside the scene loop:
if cfg.AuditLogger != nil {
    if logErr := cfg.AuditLogger.Log(ctx, domain.AuditEntry{
        Timestamp: clk.Now(),
        EventType: domain.AuditEventTTSSynthesis,
        RunID:     req.RunID,
        Stage:     string(domain.StageTTS),
        Provider:  resp.Provider,
        Model:     resp.Model,
        Prompt:    truncatePrompt(transliterated, 2048),
        CostUSD:   resp.CostUSD,
    }); logErr != nil {
        logger.Warn("audit log write failed", "run_id", req.RunID, "error", logErr)
        // Non-fatal: audit failure must not abort synthesis
    }
}
```

The `truncatePrompt` helper is a 3-line helper in `audit.go` (or `tts_track.go`) that rune-aware truncates to N chars.

### Phase A agent pattern

`TextAgentConfig.AuditLogger` is nil-safe — all existing tests continue to pass without modification because they construct `TextAgentConfig{Model: "...", Provider: "..."}` without setting AuditLogger.

In each agent after a successful `gen.Generate` call:
```go
if cfg.AuditLogger != nil {
    _ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
        Timestamp: time.Now(),  // or use a clock if available
        EventType: domain.AuditEventTextGeneration,
        RunID:     state.RunID,
        Stage:     "writer", // agent-specific constant
        Provider:  resp.Provider,
        Model:     resp.Model,
        Prompt:    truncatePrompt(prompt, 2048),
        CostUSD:   resp.CostUSD,
    })
}
```

Note: `time.Now()` is acceptable in agents since they don't have clock injection. Do not add clock injection to agents to satisfy this story.

### audit.log is NOT cleaned on stage resume

`CleanStageArtifacts` in `internal/pipeline/artifact.go` only removes `images/`, `tts/`, `clips/`, `output.mp4`, `metadata.json`, `manifest.json` — it does not touch `audit.log`. This is intentional and already correct; **do not add audit.log cleanup to that function**.

This means: if a TTS stage fails (e.g., blocked voice), the `voice_blocked` entry persists in `audit.log` across the resume. After the operator fixes `config.yaml` and resumes, new `tts_synthesis` entries are appended. The audit trail is complete.

### Run output directory structure (update `docs/` if present)

The per-run output directory tree gains a new file:
```
~/.youtube-pipeline/output/<run-id>/
├── scenario.json
├── images/
├── tts/
├── clips/
├── rejected/
├── output.mp4
├── metadata.json
├── manifest.json
└── audit.log              ← NEW (NDJSON, append-only, never cleaned)
```

### NFR compliance

- **NFR-M1**: `BlockedVoiceIDs` is config-driven — no IDs hardcoded in source
- **NFR-M2**: `blocked_voice_ids` list lives in `config.yaml` (version-controlled by operator)
- **NFR-L3**: `provider` field is non-null for every audit entry (enforce at `Log` call sites)
- **NFR-M4**: Layer import linter — `AuditLogger` interface in `domain`, implementation in `pipeline` (same pattern as TTSSynthesizer/TTSTrack)

### Project Structure Notes

- All new Go files go in `internal/domain/` or `internal/pipeline/` per the existing structure
- No new packages needed — `audit.go` lives in existing packages
- `internal/pipeline/agents/` imports `internal/domain` (already does) — no new import path needed
- The `cmd/` wiring (wherever `TTSTrackConfig` and `ImageTrackConfig` are assembled) needs `FileAuditLogger` injection — find those construction sites and add the field

### References

- FR45 / FR47 requirements: [epics.md#FR45-FR47](../_bmad-output/planning-artifacts/epics.md)
- NFR-M1, NFR-M2, NFR-L3: [epics.md#NFR section](../_bmad-output/planning-artifacts/epics.md)
- `TTSTrackConfig` and `invokeTTSProvider`: [internal/pipeline/tts_track.go](../../internal/pipeline/tts_track.go)
- `ImageTrackConfig`: [internal/pipeline/image_track.go](../../internal/pipeline/image_track.go)
- `TextAgentConfig`: [internal/pipeline/agents/writer.go](../../internal/pipeline/agents/writer.go)
- `PipelineConfig` + `DefaultConfig`: [internal/domain/config.go](../../internal/domain/config.go)
- `CleanStageArtifacts` (no change needed): [internal/pipeline/artifact.go](../../internal/pipeline/artifact.go)
- `PhaseBRunner.Run` (errgroup concurrency — why mutex): [internal/pipeline/phase_b.go](../../internal/pipeline/phase_b.go)
- `ValidateDistinctProviders` (FR46 guard pattern to mirror): [internal/pipeline/provider_guard.go](../../internal/pipeline/provider_guard.go)
- `domain.ErrValidation` wrapping pattern: [internal/pipeline/tts_track.go:57-67](../../internal/pipeline/tts_track.go)
- Architecture output dir tree: [architecture.md](../_bmad-output/planning-artifacts/architecture.md)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### Review Findings

- [x] [Review][Patch] Production wiring still leaves `phaseBRunner` unused in `serve`, so AC-1 remains unmet for runtime `text_generation` and `image_generation` logging [cmd/pipeline/serve.go:145]
- [x] [Review][Patch] Blocked-voice error text includes the wrapped `validation error` suffix instead of the exact operator-facing message required by AC-2 [internal/pipeline/tts_track.go:139]
- [x] [Review][Patch] `truncatePrompt` returns 2047 runes for over-limit prompts, so logged prompts are shorter than the 2048-char contract in AC-1 [internal/pipeline/audit.go:60]

### File List
