# Story 9.2: Metadata & Attribution Bundle

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want to generate a metadata package with full attributions,
so that the output complies with the SCP Wiki CC BY-SA 3.0 license and YouTube AI-content disclosure requirements.

## Prerequisites

**Hard dependencies:**
- Story 9.1 (FFmpeg Two-Stage Assembly Engine) must complete before this story executes at runtime — `metadata_ack` follows `assemble` in the state machine (`engine.go:77–80`). However, Story 9.2 can be developed and unit-tested independently of Story 9.1 since the metadata builder reads `scenario.json` and config, not FFmpeg outputs.
- Phase A and Phase B must have completed for a run so that `scenario.json` and all provider metadata exist.

**State machine context:** `assemble` → (EventComplete) → `metadata_ack` (HITL wait point). The metadata builder fires as the *entry action* for `metadata_ack` — it generates `metadata.json` and `manifest.json` before the pipeline pauses for operator acknowledgment. See `internal/pipeline/engine.go:77–85` and `IsHITLStage` at line 93.

**Artifact cleanup already wired:** `internal/pipeline/artifact.go:33–38` already handles `StageMetadataAck` cleanup (removes `metadata.json` and `manifest.json` on resume). Do NOT change this.

**No new DB tables.** The metadata builder is a pure read-then-write function — reads `scenario.json` + config + corpus, writes two JSON files. No DB migration needed.

## Acceptance Criteria

### AC-1: `SCPMeta` extended with attribution fields

**Given** the corpus `meta.json` for any SCP entry
**When** the corpus reader parses `meta.json`
**Then** `SCPMeta` (in `internal/pipeline/agents/corpus.go`) has two new fields:
  - `AuthorName string` (`json:"author_name"`) — the primary author/creator of the SCP article
  - `SourceURL string` (`json:"source_url"`) — canonical article URL (e.g., `https://scp-wiki.wikidot.com/scp-049`)

**And** both fields are required for metadata generation (non-empty validation at build time, not at parse time — missing author/URL in corpus is a data gap, not a parse error)

**Rules:**
- Do NOT add `AuthorName`/`SourceURL` to `SCPFacts` — attribution metadata belongs in `SCPMeta`, not the content-summary facts.
- Update the test fixture at `testdata/fixtures/corpus/SCP-TEST/meta.json` to include `"author_name": "Test Author"` and `"source_url": "https://scp-wiki.wikidot.com/scp-test"`.
- The corpus reader is in `internal/pipeline/agents/corpus.go:50–54`. Add the two fields to the `SCPMeta` struct. No changes to the reader function itself — Go's `encoding/json` will unmarshal the new fields automatically.

**Tests:** Unit — `corpus_test.go`: verify `SCPMeta` with `author_name` and `source_url` parses correctly; verify omitted fields yield empty strings (zero value, not error).

---

### AC-2: `domain.MetadataBundle` and `domain.SourceManifest` types defined

**Given** the project's domain type layer (`internal/domain/`)
**When** Story 9.2 is implemented
**Then** a new file `internal/domain/compliance.go` defines:

```go
// MetadataBundle is the YT-ready AI-content disclosure written to metadata.json.
type MetadataBundle struct {
    Version      int                      `json:"version"`
    GeneratedAt  string                   `json:"generated_at"` // RFC3339
    RunID        string                   `json:"run_id"`
    SCPID        string                   `json:"scp_id"`
    Title        string                   `json:"title"`
    AIGenerated  AIGeneratedFlags         `json:"ai_generated"`
    ModelsUsed   map[string]ModelRecord   `json:"models_used"`
}

// AIGeneratedFlags declares which content components were AI-generated.
type AIGeneratedFlags struct {
    Narration bool `json:"narration"`
    Imagery   bool `json:"imagery"`
    TTS       bool `json:"tts"`
}

// ModelRecord records a single provider+model pair used during generation.
type ModelRecord struct {
    Provider string `json:"provider"`
    Model    string `json:"model"`
    Voice    string `json:"voice,omitempty"` // TTS only
}

// SourceManifest is the license-audit record written to manifest.json.
type SourceManifest struct {
    Version      int            `json:"version"`
    GeneratedAt  string         `json:"generated_at"` // RFC3339
    RunID        string         `json:"run_id"`
    SCPID        string         `json:"scp_id"`
    SourceURL    string         `json:"source_url"`
    AuthorName   string         `json:"author_name"`
    License      string         `json:"license"`      // "CC BY-SA 3.0"
    LicenseURL   string         `json:"license_url"`  // canonical CC URL
    LicenseChain []LicenseEntry `json:"license_chain"`
}

// LicenseEntry is one node in the license attribution chain.
type LicenseEntry struct {
    Component  string `json:"component"`  // e.g. "SCP article text"
    SourceURL  string `json:"source_url"`
    AuthorName string `json:"author_name"`
    License    string `json:"license"`
}
```

**Rules:**
- `ModelsUsed` keys MUST be one of: `"writer"`, `"critic"`, `"image"`, `"tts"`, `"visual_breakdown"` — all five must be present with non-empty `Provider` and `Model` fields (FR45 non-null guarantee).
- `Version: 1` for V1.
- `License` constant for SCP Wiki: `"CC BY-SA 3.0"`. `LicenseURL`: `"https://creativecommons.org/licenses/by-sa/3.0/"`.
- Do NOT use `interface{}` or `map[string]any` — all fields must be strongly typed.
- No `omitempty` on required fields (version, run_id, scp_id, title, source_url, author_name, license).

**Tests:** Unit — `compliance_test.go` in `internal/domain/`: JSON round-trip for both types; verify `ModelsUsed` serializes all 5 keys; verify `LicenseChain` slice serializes correctly.

---

### AC-3: `MetadataBuilder` builds and validates both bundles

**Given** a completed run with `scenario.json` on disk and a `SCPMeta` with `AuthorName`/`SourceURL`
**When** `MetadataBuilder.Build(ctx, runID)` is called
**Then** it returns `(MetadataBundle, SourceManifest, error)` where both are fully populated

**Input contract:**
- Reads `{outputDir}/{runID}/scenario.json` → unmarshal `agents.PipelineState`
- Reads `SCPMeta` via `CorpusReader.Read(ctx, scpID)` → `CorpusDocument.Meta`
- Reads model/provider config from `MetadataBuilderConfig` (injected at construction)

**Output contract for `MetadataBundle`:**
```json
{
  "version": 1,
  "generated_at": "<RFC3339>",
  "run_id": "scp-049-run-1",
  "scp_id": "049",
  "title": "SCP-049 - The Plague Doctor",
  "ai_generated": {"narration": true, "imagery": true, "tts": true},
  "models_used": {
    "writer":            {"provider": "deepseek", "model": "deepseek-v4-flash"},
    "critic":            {"provider": "gemini",   "model": "gemini-3.1-flash-lite-preview"},
    "image":             {"provider": "dashscope","model": "qwen-max-vl"},
    "tts":               {"provider": "dashscope","model": "qwen3-tts-flash-2025-09-18", "voice": "longhua"},
    "visual_breakdown":  {"provider": "gemini",   "model": "gemini-3.1-flash-lite-preview"}
  }
}
```

**Output contract for `SourceManifest`:**
```json
{
  "version": 1,
  "generated_at": "<RFC3339>",
  "run_id": "scp-049-run-1",
  "scp_id": "049",
  "source_url": "https://scp-wiki.wikidot.com/scp-049",
  "author_name": "Djoric",
  "license": "CC BY-SA 3.0",
  "license_url": "https://creativecommons.org/licenses/by-sa/3.0/",
  "license_chain": [
    {
      "component": "SCP article text",
      "source_url": "https://scp-wiki.wikidot.com/scp-049",
      "author_name": "Djoric",
      "license": "CC BY-SA 3.0"
    }
  ]
}
```

**Validation rules (return `domain.ErrValidation` for):**
- `WriterProvider` empty in `scenario.json` narration metadata
- `VisualBreakdownProvider` empty in `scenario.json` visual breakdown metadata
- `AuthorName` or `SourceURL` empty in corpus meta
- `scenario.json` does not exist or fails to parse

**Rules:**
- Extract `WriterModel` and `WriterProvider` from `state.Narration.Metadata.WriterModel` and `state.Narration.Metadata.WriterProvider`.
- Extract `VisualBreakdownModel` and `VisualBreakdownProvider` from `state.VisualBreakdown.Metadata.VisualBreakdownModel` and `state.VisualBreakdown.Metadata.VisualBreakdownProvider`.
- Critic model/provider come from `MetadataBuilderConfig` (config-driven) — NOT from scenario.json (the Critic agent does NOT currently write its model info to `PipelineState`; avoid inventing fields on `PipelineState` or `CriticOutput` in this story).
- Image and TTS model/provider/voice come from `MetadataBuilderConfig`.
- `Title` comes from `state.Research.Title` (or `state.Narration.SCPID` as fallback if Research nil).
- `generated_at` uses `clock.Clock.Now()` injected into `MetadataBuilderConfig`.

**Tests:** Unit — mock `CorpusReader` + mock `clock.Clock` + scenario.json fixture. Verify complete `ModelsUsed` map, license chain content, and validation errors for missing fields.

---

### AC-4: `MetadataBuilderConfig` struct defined and wired into Phase C runner

**Given** the pipeline config (`domain.PipelineConfig`)
**When** the Phase C runner is constructed
**Then** a `MetadataBuilderConfig` struct captures all provider/model/voice values needed for bundle generation:

```go
// internal/pipeline/phase_c_metadata.go
type MetadataBuilderConfig struct {
    OutputDir          string
    WriterModel        string // from PipelineConfig
    WriterProvider     string // from PipelineConfig
    CriticModel        string // from PipelineConfig
    CriticProvider     string // from PipelineConfig
    ImageModel         string // from PipelineConfig
    ImageProvider      string // "dashscope" default — add ImageProvider field to PipelineConfig
    TTSModel           string // from PipelineConfig
    TTSProvider        string // "dashscope" default — add TTSProvider field to PipelineConfig
    TTSVoice           string // from PipelineConfig
    Corpus             agents.CorpusReader
    Clock              clock.Clock
    Logger             *slog.Logger
}
```

**Two new config fields added to `domain.PipelineConfig`:**
- `ImageProvider string` `yaml:"image_provider"` — default `"dashscope"` in `DefaultConfig()`
- `TTSProvider string` `yaml:"tts_provider"` — default `"dashscope"` in `DefaultConfig()`

**Rules:**
- Do NOT hardcode `"dashscope"` inside `phase_c_metadata.go` — read from config. The config default handles V1.
- The `MetadataBuilderConfig` is NOT a domain type — keep it in `internal/pipeline/` alongside Phase B configs (`image_track.go`, `tts_track.go`).
- Follow the same constructor pattern as `NewTTSTrack` and `NewImageTrack`: return `(MetadataBuilder, error)` with validation of required fields.

**Tests:** Unit — construction fails with `domain.ErrValidation` for empty `OutputDir`, `CriticModel`, `CriticProvider`.

---

### AC-5: `metadata.json` and `manifest.json` written to run output directory

**Given** `MetadataBuilder.Build(ctx, runID)` returns successfully
**When** `MetadataBuilder.Write(ctx, runID, bundle, manifest)` is called
**Then** `{outputDir}/{runID}/metadata.json` and `{outputDir}/{runID}/manifest.json` are written with `encoding/json` (indented, `json.MarshalIndent` with 2-space indent)

**Rules:**
- Write is atomic: write to `<path>.tmp` then `os.Rename` to final path. Prevents half-written files on crash.
- File mode `0644`.
- If the file already exists (resume scenario), overwrite it — idempotent.
- Do NOT create a subdirectory; both files go directly in `{outputDir}/{runID}/`.
- Follow `artifact.go:removeFile` pattern for cleanup of `metadata.json` / `manifest.json` on resume (already wired in `StageMetadataAck` cleanup — do not change `artifact.go`).

**Tests:** Integration — write to `t.TempDir()`, verify both files exist, unmarshal and compare fields; verify idempotent overwrite (call Write twice, second call succeeds).

---

### AC-6: Metadata builder invoked as Phase C entry action in the pipeline engine

**Given** the assembly stage (`StageAssemble`) completes successfully (EventComplete)
**When** the engine transitions to `StageMetadataAck`
**Then** the metadata builder runs before the engine pauses for HITL

**Implementation location:** Create `internal/pipeline/phase_c.go` with a `PhaseCMetadataEntry` function (mirrors the pattern of `finalize_phase_a.go`) that:
1. Calls `MetadataBuilder.Build(ctx, runID)` → gets bundle + manifest
2. Calls `MetadataBuilder.Write(ctx, runID, bundle, manifest)` → writes files
3. Returns error if either step fails (engine will mark stage failed, operator can resume)

**How the runner wires this:** In the pipeline runner (wherever `StageAssemble` → `StageMetadataAck` advance is handled), call `PhaseCMetadataEntry` after the DB stage transition write but before returning control to the HITL wait. Follow the pattern in the existing Phase A finalization: `finalize_phase_a.go` + `finalize_phase_a_test.go`.

**Rules:**
- Do NOT call the metadata builder inside `artifact.go` or `engine.go` — keep the pure state-machine functions pure.
- Do NOT create a new `phase_c_runner.go` that reimplements the Phase B runner pattern — Phase C assembly (Story 9.1) and metadata bundle are two separate entry functions, not a parallel track runner.
- The metadata builder must be injectable (via interface) so the runner can be unit tested without real filesystem.
- If `PhaseCMetadataEntry` fails, the stage remains at `assemble` status=failed (standard error path); the operator can resume.

**Tests:** Integration — inject a mock `MetadataBuilderFunc` into the runner, verify it is called exactly once when `StageAssemble` completes; verify `StageMetadataAck` is only entered after metadata write succeeds.

---

### AC-7: NFR-L2 compliance — files are co-located with video outputs

**Given** the per-run output directory `~/.youtube-pipeline/output/{run-id}/`
**When** Phase C metadata entry completes
**Then** both `metadata.json` and `manifest.json` are in the same directory as `output.mp4`
**And** the directory structure matches the canonical layout:
```
{run-id}/
├── output.mp4
├── metadata.json   ← this story
└── manifest.json   ← this story
```

**Rules:** This is a placement rule only — do not add a subdirectory. The E2E test (`e2e_test.go`) already asserts `metadata.json` and `manifest.json` exist in the run output directory (Story 1.7, FR52-go). Ensure the paths match.

**Tests:** Verify in the E2E test (`e2e_test.go`) that `metadata.json` and `manifest.json` exist after a full pipeline run. (The E2E test already checks for these files per Story 1.7 — confirm the implementation satisfies the existing assertion rather than adding a duplicate check.)

---

## Tasks / Subtasks

- [ ] Task 1: Extend `SCPMeta` with attribution fields (AC-1)
  - [ ] Add `AuthorName string` and `SourceURL string` to `SCPMeta` in `internal/pipeline/agents/corpus.go`
  - [ ] Update `testdata/fixtures/corpus/SCP-TEST/meta.json` with test values
  - [ ] Add `corpus_test.go` cases for new fields
- [ ] Task 2: Define domain compliance types (AC-2)
  - [ ] Create `internal/domain/compliance.go` with `MetadataBundle`, `SourceManifest`, et al.
  - [ ] Add `compliance_test.go` JSON round-trip tests
- [ ] Task 3: Add `ImageProvider` and `TTSProvider` to `PipelineConfig` (AC-4 prerequisite)
  - [ ] Add fields to `internal/domain/config.go`
  - [ ] Set defaults `"dashscope"` in `DefaultConfig()`
  - [ ] Update `config_test.go` if it asserts on the default struct
- [ ] Task 4: Implement `MetadataBuilder` (AC-3, AC-4, AC-5)
  - [ ] Create `internal/pipeline/phase_c_metadata.go`
  - [ ] Define `MetadataBuilderConfig` struct and `NewMetadataBuilder` constructor
  - [ ] Implement `Build()` — reads scenario.json + corpus, assembles bundles
  - [ ] Implement `Write()` — atomic write of both JSON files
  - [ ] Add `phase_c_metadata_test.go` with unit + integration tests
- [ ] Task 5: Wire metadata builder into Phase C runner (AC-6)
  - [ ] Create `internal/pipeline/phase_c.go` with `PhaseCMetadataEntry`
  - [ ] Inject into the pipeline runner at the `StageAssemble → StageMetadataAck` transition
  - [ ] Add `phase_c_test.go` integration tests
- [ ] Task 6: Verify E2E test assertions pass (AC-7)
  - [ ] Run `go test -run E2E ./internal/pipeline/...` and confirm `metadata.json` / `manifest.json` assertions pass

### Review Findings

**Decision-Needed (resolve before patching):**

- [x] [Review][Decision] D1: MetadataBuilderFunc.Write → no-op return nil (test adapter 패턴) [internal/pipeline/phase_c_metadata.go:32]
- [x] [Review][Decision] D2: Title priority → Research.Title 우선, SCPID fallback (spec 준수, Narration.Title 중간 fallback 제거) [internal/pipeline/phase_c_metadata.go:136]
- [x] [Review][Decision] D3: WriterProvider → scenario.json-only (config fallback 제거, spec LLM source map 준수) [internal/pipeline/phase_c_metadata.go:141]
- [x] [Review][Decision] D4: phaseCMetadata nil → slog.Warn 추가 (테스트 유연성 유지 + 컴플라이언스 가시성) [internal/pipeline/resume.go]
- [x] [Review][Decision] D5: TTSVoice → TTSProvider+TTSModel 모두 설정 시 공백이면 ErrValidation [internal/pipeline/phase_c_metadata.go:205]

**Patch (완료):**

- [x] [Review][Patch] P1: Dead if-block 제거 [internal/pipeline/phase_c_metadata.go:141]
- [x] [Review][Patch] P2: TestResume_MetadataAck_MetadataBuilderWired 구현 (StageMetadataAck 경로, ffmpeg 불필요) [internal/pipeline/resume_test.go]
- [x] [Review][Patch] P3: PhaseCMetadataEntry 순서 수정 — NextStage DB advance 후 실행 [internal/pipeline/resume.go]
- [x] [Review][Patch] P4: state.SCPID 빈 문자열 검증 추가 [internal/pipeline/phase_c_metadata.go:127]
- [x] [Review][Patch] P5: scenario.json 디코드 에러 %v → %w [internal/pipeline/phase_c_metadata.go:123]
- [x] [Review][Patch] P6: StageMetadataAck resume 경로 metadata 재생성 추가 [internal/pipeline/resume.go]

**Defer (사전 이슈 또는 추후 개선):**

- [x] [Review][Defer] W1: metadata.json + manifest.json 단위 원자성 미보장 — 첫 파일 성공 후 두 번째 실패 시 타임스탬프 불일치 재시도 발생. 스테이징 디렉토리 또는 트랜잭션 설계 필요 [internal/pipeline/phase_c_metadata.go:262] — deferred, 설계 작업 필요
- [x] [Review][Defer] W2: NewMetadataBuilder에서 WriterModel/WriterProvider 미검증 — Build()에서 검증되므로 기능 문제는 없으나 CriticModel 등 다른 필드와 불일치 [internal/pipeline/phase_c_metadata.go:68] — deferred, pre-existing style inconsistency
- [x] [Review][Defer] W3: xfade offset 음수 (shot < 0.5s) — phase_c.go 기존 이슈, 이번 스토리 미도입 [internal/pipeline/phase_c.go:392] — deferred, pre-existing
- [x] [Review][Defer] W4: concatClips duration probe 0값 허용 — phase_c.go 기존 이슈 [internal/pipeline/phase_c.go:505] — deferred, pre-existing
- [x] [Review][Defer] W5: ResumeWithOptions segments 스냅샷 stale — ClearClipPathsByRunID 후 in-memory slice에 stale ClipPath 잔존. 기존 이슈 [internal/pipeline/resume.go:209] — deferred, pre-existing

## Dev Notes

### Critical gaps the dev MUST address

1. **`SCPMeta` lacks author/URL fields.** `internal/pipeline/agents/corpus.go:50–54` defines `SCPMeta` with only `SCPID`, `Tags`, `RelatedDocs`. The corpus fixture at `testdata/fixtures/corpus/SCP-TEST/meta.json` currently has no `author_name` or `source_url`. Both must be added before the manifest builder can produce a valid `SourceManifest`.

2. **`PipelineConfig` lacks `ImageProvider` and `TTSProvider`.** `internal/domain/config.go` has `WriterProvider` and `CriticProvider` but no equivalent for image/TTS. Both DashScope (V1 default). Add both fields with `"dashscope"` defaults.

3. **Critic model/provider NOT in scenario.json.** The Critic agent (`internal/pipeline/agents/`) does not write its model info into `PipelineState` or `CriticOutput`. Do NOT add fields to `PipelineState` in this story — read critic model/provider from `MetadataBuilderConfig` (which gets its values from `PipelineConfig`).

4. **FR45 non-null guarantee.** All 5 `ModelsUsed` entries must have non-empty `Provider` + `Model`. Validate in `Build()` and return `domain.ErrValidation` for any empty value. NFR-R1 explicitly states "non-null required metadata bundle" for stage-level resume correctness.

### Existing patterns to reuse

- **Config pattern:** Follow `ImageTrackConfig` in `image_track.go:43–55` — bundle all deps into a config struct, validate in constructor.
- **Atomic write pattern:** Use `os.Rename` from temp file (same pattern as `scenario.json` serialization in `finalize_phase_a.go`).
- **Clock injection:** Use `clock.Clock` from `internal/clock/` (already used in `tts_track.go`, `image_track.go`) for `generated_at` RFC3339 timestamp.
- **CorpusReader interface:** Already defined in `internal/pipeline/agents/corpus.go:15–17`. Pass it to `MetadataBuilderConfig` — do NOT re-read the corpus directly in `phase_c_metadata.go` via filesystem. The interface keeps the metadata builder testable.
- **ErrValidation / ErrStageFailed:** Use `domain.ErrValidation` for schema/data errors, `domain.ErrStageFailed` for IO errors. Mirror `corpus.go` error wrapping style.
- **JSON indented write:** The architecture does not mandate indentation for machine-read files, but `json.MarshalIndent` with 2 spaces makes the compliance files human-auditable (NFR-L2).

### File locations

| New file | Purpose |
|---|---|
| `internal/domain/compliance.go` | `MetadataBundle`, `SourceManifest`, `ModelRecord`, etc. |
| `internal/domain/compliance_test.go` | JSON round-trip tests |
| `internal/pipeline/phase_c_metadata.go` | `MetadataBuilderConfig`, `NewMetadataBuilder`, `Build`, `Write` |
| `internal/pipeline/phase_c_metadata_test.go` | Unit + integration tests |
| `internal/pipeline/phase_c.go` | `PhaseCMetadataEntry` function |
| `internal/pipeline/phase_c_test.go` | Integration tests for runner wiring |

| Modified file | Change |
|---|---|
| `internal/pipeline/agents/corpus.go` | Add `AuthorName`, `SourceURL` to `SCPMeta` struct |
| `internal/domain/config.go` | Add `ImageProvider`, `TTSProvider` fields + defaults |
| `testdata/fixtures/corpus/SCP-TEST/meta.json` | Add `author_name`, `source_url` test values |
| Pipeline runner (wherever assemble→metadata_ack transition fires) | Call `PhaseCMetadataEntry` |

### State machine and HITL flow

```
StageAssemble (EventComplete)
  → DB write: stage=metadata_ack, status=running
  → PhaseCMetadataEntry()  ← this story
      Build(ctx, runID) → MetadataBundle + SourceManifest
      Write(ctx, runID, ...) → metadata.json + manifest.json
  → DB write: status=waiting (HITL pause)
  → Operator ACKs via POST /api/runs/{id}/metadata/ack
  → StageMetadataAck (EventApprove) → StageComplete
```

### LLM provider source map for `ModelsUsed`

| Key | Source in code |
|---|---|
| `writer.provider` | `state.Narration.Metadata.WriterProvider` (scenario.json) |
| `writer.model` | `state.Narration.Metadata.WriterModel` (scenario.json) |
| `critic.provider` | `MetadataBuilderConfig.CriticProvider` (from PipelineConfig) |
| `critic.model` | `MetadataBuilderConfig.CriticModel` (from PipelineConfig) |
| `image.provider` | `MetadataBuilderConfig.ImageProvider` (new config field, default "dashscope") |
| `image.model` | `MetadataBuilderConfig.ImageModel` (from PipelineConfig.ImageModel) |
| `tts.provider` | `MetadataBuilderConfig.TTSProvider` (new config field, default "dashscope") |
| `tts.model` | `MetadataBuilderConfig.TTSModel` (from PipelineConfig.TTSModel) |
| `tts.voice` | `MetadataBuilderConfig.TTSVoice` (from PipelineConfig.TTSVoice) |
| `visual_breakdown.provider` | `state.VisualBreakdown.Metadata.VisualBreakdownProvider` (scenario.json) |
| `visual_breakdown.model` | `state.VisualBreakdown.Metadata.VisualBreakdownModel` (scenario.json) |

### Testing standards

- **Unit tests:** Mock `CorpusReader` + inject a `clock.Clock` stub returning a fixed time. Use `testdata/fixtures/` scenario.json fixture (or construct `agents.PipelineState` inline).
- **Integration tests:** Write to `t.TempDir()`, assert file content via `os.ReadFile` + `json.Unmarshal`.
- **No real DashScope/DeepSeek calls** — all provider info is config-driven strings, not live API calls.
- **Validate schema:** After `Build()`, assert all 5 `ModelsUsed` keys present with non-empty values.
- **E2E:** Existing `e2e_test.go` already asserts `metadata.json` and `manifest.json` exist — the implementation must satisfy this without modifying the E2E test assertions.

### Project Structure Notes

- New Go files in `internal/pipeline/` follow snake_case naming with `_test.go` suffix for tests.
- All new domain types go in `internal/domain/` — not in `internal/pipeline/`.
- `internal/pipeline/agents/corpus.go` changes are minimal (2 struct fields) — do not touch `CorpusReader` interface or `filesystemCorpus` reader logic.
- Layer import rules (NFR-M4): `pipeline` may import `domain` and `agents`, but `domain` must NOT import `pipeline` or `agents`. `compliance.go` in `domain` has zero external imports.

### References

- Architecture: output directory structure → `architecture.md` "Artifact File Structure Convention" (lines ~796–821)
- Architecture: DB schema → `architecture.md` "Schema (V1, 3 tables)" (lines ~474–539)
- Architecture: provider abstraction → `architecture.md` "LLM provider abstraction" (lines ~191–194)
- FR21, FR22: `epics.md` lines 43–44
- FR45: `epics.md` line 69
- NFR-L1–L4: `epics.md` lines 111–114
- NFR-R1: `epics.md` line 86
- `artifact.go:33–38` — StageMetadataAck cleanup (do NOT change)
- `engine.go:77–85` — state machine transitions for assemble→metadata_ack
- `internal/domain/config.go` — `PipelineConfig` struct (add ImageProvider, TTSProvider)
- `internal/domain/narration.go:34–42` — `NarrationMetadata` (WriterProvider, WriterModel)
- `internal/domain/visual_breakdown.go:43–48` — `VisualBreakdownMetadata` (VisualBreakdownProvider, VisualBreakdownModel)
- `internal/pipeline/agents/corpus.go:50–54` — `SCPMeta` struct (add AuthorName, SourceURL)
- `testdata/fixtures/corpus/SCP-TEST/meta.json` — fixture to update
- `internal/pipeline/finalize_phase_a.go` — atomic write + clock injection pattern to mirror

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List
