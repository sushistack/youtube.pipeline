# Story 1.3: Domain Types, Error System & Architecture Interfaces

Status: done

## Story

As a developer,
I want core domain types, error classifications, and provider interfaces defined,
so that all subsequent domain implementation has a stable foundation.

## Acceptance Criteria

1. **AC-STAGE:** `internal/domain/types.go` defines a `Stage` string type with exactly 15 constants: `pending`, `research`, `structure`, `write`, `visual_break`, `review`, `critic`, `scenario_review`, `character_pick`, `image`, `tts`, `batch_review`, `assemble`, `metadata_ack`, `complete`.

2. **AC-STRUCTS:** `internal/domain/types.go` defines `Run`, `Episode`, and `NormalizedResponse` structs with `snake_case` JSON tags.

3. **AC-ERRORS:** `internal/domain/errors.go` defines 7 sentinel errors: `ErrRateLimited`, `ErrUpstreamTimeout`, `ErrStageFailed`, `ErrValidation`, `ErrConflict`, `ErrCostCapExceeded`, `ErrNotFound`. Each carries HTTP status code and retryable flag attributes.

4. **AC-IFACE:** `internal/domain/llm.go` defines `TextGenerator`, `ImageGenerator` (with both `Generate` and `Edit` methods), and `TTSSynthesizer` interfaces. All implementations accept `*http.Client` via constructor — `http.DefaultClient` usage is forbidden.

5. **AC-CLOCK:** `internal/clock/clock.go` defines a `Clock` interface with `RealClock` and `FakeClock` implementations. `FakeClock` supports deterministic time advancement for testing.

6. **AC-IMPORT:** `internal/domain/` imports nothing from `internal/`. `internal/clock/` imports nothing from `internal/`.

7. **AC-TEST:** Compile-time interface satisfaction checks exist for all interfaces. FakeClock advancement test passes. Error classification tests verify all 7 errors have correct HTTP status and retryable attributes.

## Tasks / Subtasks

- [x] **T1: `internal/domain/types.go` — Stage type + core structs** (AC: #1, #2)
  - [x] Define `Stage` string type with 15 exported constants (`StagePending` through `StageComplete`)
  - [x] Define `Run` struct mapping to `runs` DB table (15 fields, snake_case JSON tags, pointer types for nullable fields)
  - [x] Define `Episode` struct representing a scene/segment (maps to `segments` table; contains `Shots []Shot` as JSON-serialized field)
  - [x] Define `Shot` struct (image path, duration, transition type, visual descriptor)
  - [x] Define `NormalizedResponse` struct (common LLM response envelope: content, model, provider, token counts, cost, duration)
  - [x] Define `Decision` struct mapping to `decisions` DB table (13 fields)

- [x] **T2: `internal/domain/errors.go` — Error classification system** (AC: #3)
  - [x] Define `DomainError` struct with `Code`, `Message`, `HTTPStatus`, `Retryable` fields
  - [x] Implement `Error() string` method on `*DomainError`
  - [x] Define 7 sentinel errors as `*DomainError` variables with correct classification per architecture table
  - [x] Add `Classify(err error) (httpStatus int, code string, retryable bool)` helper using `errors.As`

- [x] **T3: `internal/domain/llm.go` — Provider interfaces + request/response types** (AC: #4)
  - [x] Define `TextRequest` / `TextResponse` structs
  - [x] Define `ImageRequest` / `ImageEditRequest` / `ImageResponse` structs
  - [x] Define `TTSRequest` / `TTSResponse` structs
  - [x] Define `TextGenerator` interface with `Generate(ctx, TextRequest) (TextResponse, error)`
  - [x] Define `ImageGenerator` interface with `Generate(ctx, ImageRequest) (ImageResponse, error)` and `Edit(ctx, ImageEditRequest) (ImageResponse, error)`
  - [x] Define `TTSSynthesizer` interface with `Synthesize(ctx, TTSRequest) (TTSResponse, error)`

- [x] **T4: `internal/clock/clock.go` — Clock abstraction** (AC: #5)
  - [x] Define `Clock` interface with `Now() time.Time` and `Sleep(ctx context.Context, d time.Duration) error`
  - [x] Implement `RealClock` using real `time` package
  - [x] Implement `FakeClock` with `Advance(d time.Duration)` for deterministic time control
  - [x] `FakeClock.Sleep` must respect both `Advance` and context cancellation

- [x] **T5: Tests** (AC: #6, #7)
  - [x] `internal/domain/types_test.go`: Stage constant count assertion (exactly 15), JSON tag snake_case verification via reflection
  - [x] `internal/domain/errors_test.go`: All 7 errors have correct HTTP status + retryable; `errors.Is` works through `fmt.Errorf` wrapping; `Classify` extracts correct values
  - [x] `internal/domain/llm_test.go`: Compile-time interface satisfaction checks (`var _ TextGenerator = (*mockTextGen)(nil)` etc.)
  - [x] `internal/clock/clock_test.go`: FakeClock `Now()` returns initial time; `Advance` moves time forward; `Sleep` resolves after `Advance`; context cancellation aborts `Sleep`

### Review Findings

- [x] [Review][Patch] `RealClock.Sleep` time.After 타이머 누수 — ctx 취소 시 timer.Stop() 필요 [internal/clock/clock.go:21-27]
- [x] [Review][Patch] `FakeClock.Sleep` waiter 누수 — ctx 취소 시 waiters 슬라이스에서 미제거 [internal/clock/clock.go:70-87]
- [x] [Review][Patch] `AllStages` 가변 slice — 외부 패키지에서 변조 가능, 함수로 전환 [internal/domain/types.go:25]
- [x] [Review][Defer] DomainError sentinel 포인터 가변 — deferred, internal 패키지이며 단일 사용자 도구
- [x] [Review][Defer] Stage("") 유효값 — deferred, 검증은 Epic 2 상태머신 범위
- [x] [Review][Defer] TextRequest 제로값 검증 없음 — deferred, provider 구현 시 검증 추가 (Epic 5)

## Dev Notes

### Architecture File Split

The architecture document specifies this file organization for `domain/`:

```
internal/domain/
  types.go    — Run, Segment, Decision structs
  stages.go   — Stage/Status/Event enums, NextStage()
  llm.go      — TextGenerator, ImageGenerator, TTSSynthesizer
  errors.go   — Sentinel errors
  config.go   — PipelineConfig struct
```

**For Story 1.3, deviate from architecture file split as follows:** Place Stage type + constants in `types.go` (not `stages.go`) because the AC explicitly specifies this, and `NextStage()` / `Status` / `Event` enums are Epic 2 scope. When Epic 2 creates `stages.go` for the state machine, the Stage type stays in `types.go` — it is a domain type, not a state machine function.

Do NOT create `stages.go` or `config.go` in this story.

### Stage Constants (Exactly 15)

```go
type Stage string

const (
    StagePending        Stage = "pending"
    StageResearch       Stage = "research"
    StageStructure      Stage = "structure"
    StageWrite          Stage = "write"
    StageVisualBreak    Stage = "visual_break"
    StageReview         Stage = "review"
    StageCritic         Stage = "critic"
    StageScenarioReview Stage = "scenario_review"  // HITL wait point
    StageCharacterPick  Stage = "character_pick"    // HITL wait point
    StageImage          Stage = "image"
    StageTTS            Stage = "tts"
    StageBatchReview    Stage = "batch_review"      // HITL wait point
    StageAssemble       Stage = "assemble"
    StageMetadataAck    Stage = "metadata_ack"      // HITL wait point
    StageComplete       Stage = "complete"
)
```

Values are lowercase snake_case strings matching the `runs.stage` DB column values exactly. These must match the `001_init.sql` schema's `DEFAULT 'pending'` for the `stage` column.

### Run Struct — Maps to `runs` Table

The `runs` table DDL (from Story 1.2's `001_init.sql`) has these columns:

| Column | Go type | JSON tag | Notes |
|---|---|---|---|
| id | `string` | `id` | PK, format: `scp-{scp_id}-run-{n}` |
| scp_id | `string` | `scp_id` | |
| stage | `Stage` | `stage` | Uses Stage type |
| status | `string` | `status` | pending/running/waiting/completed/failed |
| retry_count | `int` | `retry_count` | |
| retry_reason | `*string` | `retry_reason,omitempty` | Nullable |
| critic_score | `*float64` | `critic_score,omitempty` | Nullable |
| cost_usd | `float64` | `cost_usd` | |
| token_in | `int` | `token_in` | |
| token_out | `int` | `token_out` | |
| duration_ms | `int64` | `duration_ms` | |
| human_override | `bool` | `human_override` | DB stores as INTEGER (0/1) |
| scenario_path | `*string` | `scenario_path,omitempty` | Nullable |
| created_at | `string` | `created_at` | ISO 8601 / RFC 3339 |
| updated_at | `string` | `updated_at` | ISO 8601 / RFC 3339 |

Use `*string` and `*float64` for nullable DB columns. `human_override` is `bool` in Go (maps to SQLite INTEGER via 0/1).

### Episode Struct — Maps to `segments` Table

"Episode" represents a scene/segment. Maps to the `segments` table. The `shots` column stores JSON.

| Column | Go type | JSON tag | Notes |
|---|---|---|---|
| id | `int64` | `id` | AUTOINCREMENT PK |
| run_id | `string` | `run_id` | FK to runs.id |
| scene_index | `int` | `scene_index` | |
| narration | `*string` | `narration,omitempty` | Nullable |
| shot_count | `int` | `shot_count` | Default 1 |
| shots | `[]Shot` | `shots` | JSON-serialized in DB |
| tts_path | `*string` | `tts_path,omitempty` | Nullable |
| tts_duration_ms | `*int` | `tts_duration_ms,omitempty` | Nullable |
| clip_path | `*string` | `clip_path,omitempty` | Nullable |
| critic_score | `*float64` | `critic_score,omitempty` | Nullable |
| critic_sub | `*string` | `critic_sub,omitempty` | JSON rubric sub-scores |
| status | `string` | `status` | |
| created_at | `string` | `created_at` | |

### Shot Struct

Nested within Episode's `Shots` field. Stored as JSON array in the `segments.shots` column.

```go
type Shot struct {
    ImagePath        string  `json:"image_path"`
    DurationSeconds  float64 `json:"duration_s"`
    Transition       string  `json:"transition"`        // "ken_burns", "cross_dissolve", "hard_cut"
    VisualDescriptor string  `json:"visual_descriptor"`
}
```

This matches the architecture's artifact convention: each shot has an image, duration, transition type, and visual descriptor.

### Decision Struct — Maps to `decisions` Table

Include this for completeness (13 fields matching the DDL). Used by Story 1.6 (Renderer) and Epic 2.

### NormalizedResponse Struct

Common response envelope for all LLM provider adapters. Eliminates provider-specific type leakage into the service layer.

```go
type NormalizedResponse struct {
    Content      string  `json:"content"`
    Model        string  `json:"model"`
    Provider     string  `json:"provider"`
    TokensIn     int     `json:"tokens_in"`
    TokensOut    int     `json:"tokens_out"`
    CostUSD      float64 `json:"cost_usd"`
    DurationMs   int64   `json:"duration_ms"`
    FinishReason string  `json:"finish_reason,omitempty"`
}
```

### Error Classification System (7 Categories)

The architecture defines 7 error categories. **The architecture code snippet at line 1322 shows only 6 sentinel errors — it omits `ErrUpstreamTimeout`. The AC and the architecture classification table (line 617) both specify 7. Implement all 7.**

```go
type DomainError struct {
    Code       string
    Message    string
    HTTPStatus int
    Retryable  bool
}

func (e *DomainError) Error() string { return e.Message }
```

| Sentinel | Code | HTTP | Retryable |
|---|---|---|---|
| `ErrRateLimited` | `RATE_LIMITED` | 429 | true |
| `ErrUpstreamTimeout` | `UPSTREAM_TIMEOUT` | 504 | true |
| `ErrStageFailed` | `STAGE_FAILED` | 500 | true |
| `ErrValidation` | `VALIDATION_ERROR` | 400 | false |
| `ErrConflict` | `CONFLICT` | 409 | false |
| `ErrCostCapExceeded` | `COST_CAP_EXCEEDED` | 402 | false |
| `ErrNotFound` | `NOT_FOUND` | 404 | false |

**`errors.Is` compatibility:** `*DomainError` sentinel pointers work with `fmt.Errorf("context: %w", domain.ErrStageFailed)` because `errors.Is` unwraps and compares the pointer.

**`errors.As` support:** Add a `Classify` helper that extracts `DomainError` from any wrapped error chain:

```go
func Classify(err error) (httpStatus int, code string, retryable bool) {
    var de *DomainError
    if errors.As(err, &de) {
        return de.HTTPStatus, de.Code, de.Retryable
    }
    return 500, "INTERNAL_ERROR", false
}
```

This replaces the architecture's `mapDomainError()` switch statement with a type-driven approach. The API layer (`internal/api/response.go` in Epic 2) will call `domain.Classify(err)` instead of a manual switch.

### Provider Interface Signatures

From architecture lines 828–841. Location: `internal/domain/llm.go`.

```go
type TextGenerator interface {
    Generate(ctx context.Context, req TextRequest) (TextResponse, error)
}

type ImageGenerator interface {
    Generate(ctx context.Context, req ImageRequest) (ImageResponse, error)
    Edit(ctx context.Context, req ImageEditRequest) (ImageResponse, error)
}

type TTSSynthesizer interface {
    Synthesize(ctx context.Context, req TTSRequest) (TTSResponse, error)
}
```

**Request/Response types — keep minimal for now.** Concrete fields will be refined when vendor implementations are built in Epic 5. Define the minimum contract:

- `TextRequest`: `Prompt string`, `Model string`, `MaxTokens int`, `Temperature float64`
- `TextResponse`: embeds `NormalizedResponse`
- `ImageRequest`: `Prompt string`, `Model string`, `Width int`, `Height int`
- `ImageEditRequest`: `Prompt string`, `Model string`, `ReferenceImagePath string`, `Width int`, `Height int`
- `ImageResponse`: `ImagePath string`, `Model string`, `Provider string`, `CostUSD float64`, `DurationMs int64`
- `TTSRequest`: `Text string`, `Model string`, `Voice string`
- `TTSResponse`: `AudioPath string`, `DurationMs int64`, `Model string`, `Provider string`, `CostUSD float64`

**Critical constraint:** All implementing constructors must accept `*http.Client` as a parameter. Document this in the interface doc comment. Example future constructor signature:

```go
// In internal/llmclient/deepseek/client.go (Epic 5):
func NewClient(httpClient *http.Client, apiKey string) *Client
```

Using `http.DefaultClient` anywhere is forbidden — this enables API isolation (nohttp transport in tests, rate-limiting transport in production).

### Clock Interface

Location: `internal/clock/clock.go`. Purpose: testable time operations for retry backoff, anti-progress detection, and observability timing.

```go
type Clock interface {
    Now() time.Time
    Sleep(ctx context.Context, d time.Duration) error
}
```

**RealClock:** delegates to `time.Now()` and `time.Sleep` (with context cancellation via `select`).

**FakeClock:** stores a mutable current time. Key design:
- `NewFakeClock(t time.Time) *FakeClock` — starts at a fixed time
- `Now()` returns the stored time
- `Advance(d time.Duration)` moves the stored time forward and unblocks any pending `Sleep` calls whose deadline has been reached
- `Sleep(ctx, d)` blocks until either: (a) `Advance` has moved time past the deadline, or (b) `ctx` is cancelled
- Thread-safe via `sync.Mutex` + channel signaling

**FakeClock implementation pattern:**

```go
type FakeClock struct {
    mu      sync.Mutex
    now     time.Time
    waiters []waiter
}

type waiter struct {
    deadline time.Time
    ch       chan struct{}
}

func (c *FakeClock) Advance(d time.Duration) {
    c.mu.Lock()
    c.now = c.now.Add(d)
    // Wake up waiters whose deadline has been reached
    // ...
    c.mu.Unlock()
}
```

### Import Direction Rule (CRITICAL)

`domain/` and `clock/` must import **NOTHING** from `internal/`. This is enforced in CI (Story 1.7):

```bash
if grep -r '"github.com/sushistack/youtube.pipeline/internal/' internal/domain/; then
  echo "FAIL: domain/ must not import other internal packages"; exit 1
fi
```

Allowed imports for `domain/`: only Go stdlib (`context`, `errors`, `fmt`, `encoding/json`, `time`).
Allowed imports for `clock/`: only Go stdlib (`context`, `sync`, `time`).

### Interface Definition Rule

Interfaces are defined in the **consuming** package, not the implementing package. **Exception:** `domain/` defines capability interfaces (`TextGenerator`, `ImageGenerator`, `TTSSynthesizer`) because they represent domain concepts shared across multiple consumers.

For example, `service/` will later define `RunStore` interface that `db/` implements. But the LLM provider interfaces live in `domain/` because `service/`, `pipeline/`, and `llmclient/` all reference them.

### Naming Conventions

| Element | Convention | Example |
|---|---|---|
| Exported types | PascalCase | `PipelineState`, `TextGenerator` |
| Interfaces | -er suffix or capability | `TextGenerator`, `TTSSynthesizer` |
| Errors | Err prefix | `ErrStageFailed`, `ErrCostCapExceeded` |
| Constants | PascalCase (exported) | `StageResearch`, `MaxRetries` |
| Files | snake_case.go | types.go, errors.go, llm.go, clock.go |
| JSON tags | snake_case | `json:"scp_id"`, `json:"cost_usd"` |

### Testing Patterns

Use Go stdlib `testing` only — no testify, no gomock. Consistent with Stories 1.1 and 1.2.

**Compile-time interface satisfaction:**

```go
// internal/domain/llm_test.go
var _ TextGenerator = (*mockTextGen)(nil)
var _ ImageGenerator = (*mockImageGen)(nil)
var _ TTSSynthesizer = (*mockTTSSynth)(nil)

// Define minimal mock structs that satisfy the interfaces
type mockTextGen struct{}
func (m *mockTextGen) Generate(ctx context.Context, req TextRequest) (TextResponse, error) {
    return TextResponse{}, nil
}
// ... etc.
```

**FakeClock test pattern:**

```go
func TestFakeClock_Advance(t *testing.T) {
    start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
    fc := clock.NewFakeClock(start)
    if !fc.Now().Equal(start) {
        t.Errorf("Now() = %v, want %v", fc.Now(), start)
    }
    fc.Advance(5 * time.Second)
    if !fc.Now().Equal(start.Add(5 * time.Second)) {
        t.Errorf("after Advance(5s): Now() = %v, want %v", fc.Now(), start.Add(5*time.Second))
    }
}
```

**Error classification test:**

```go
func TestDomainErrors_Classification(t *testing.T) {
    tests := []struct {
        err       *DomainError
        wantHTTP  int
        wantRetry bool
    }{
        {ErrRateLimited, 429, true},
        {ErrUpstreamTimeout, 504, true},
        {ErrStageFailed, 500, true},
        {ErrValidation, 400, false},
        {ErrConflict, 409, false},
        {ErrCostCapExceeded, 402, false},
        {ErrNotFound, 404, false},
    }
    for _, tt := range tests {
        t.Run(tt.err.Code, func(t *testing.T) {
            if tt.err.HTTPStatus != tt.wantHTTP {
                t.Errorf("HTTPStatus = %d, want %d", tt.err.HTTPStatus, tt.wantHTTP)
            }
            if tt.err.Retryable != tt.wantRetry {
                t.Errorf("Retryable = %v, want %v", tt.err.Retryable, tt.wantRetry)
            }
        })
    }
}
```

### Critical Constraints

- **domain/ 300-line cap per file.** Split by concept if approaching limit.
- **No `http.DefaultClient`** anywhere in the codebase. All HTTP clients are constructor-injected.
- **No external test frameworks.** Use Go stdlib `testing` only.
- **CGO_ENABLED=0.** All Go builds and tests must work without CGO.
- **snake_case JSON tags only.** No camelCase, no omission of tags on exported struct fields.
- **Pointer types for nullable DB fields.** `*string`, `*float64`, `*int` — not zero values.
- **`doc.go` files exist** in both `internal/domain/` and `internal/clock/` from Story 1.1. Do NOT delete or overwrite them — add new files alongside.
- **No `time.Now()` or `time.Sleep()` in non-clock packages.** All time operations go through the Clock interface. This enables deterministic testing of backoff, timing, and anti-progress detection.

### Previous Story Intelligence

**Story 1.1 (done):** Created the full directory structure. `internal/domain/doc.go` and `internal/clock/doc.go` already exist as package stubs. `internal/llmclient/{dashscope,deepseek,gemini}/doc.go` stubs exist.

**Story 1.2 (done):** Implemented SQLite DB with full DDL. Key learnings:
- Used `fmt.Errorf("context: %w", err)` error wrapping pattern consistently
- `PRAGMA user_version` cannot run inside a transaction (silently ignored)
- Review added `PRAGMA foreign_keys=ON` and `db.SetMaxOpenConns(1)` — good defensive patterns
- Deferred `updated_at` trigger to Epic 2
- Tests use `t.TempDir()` for test DB paths

**Story 1.2 Review Findings (apply to this story):**
- `PRAGMA foreign_keys=ON` was added in review — be thorough on DB interaction implications
- Crash recovery gaps in narrow windows are acceptable for single-user localhost tool

### Git Intelligence

Recent commit style: imperative mood, brief subject line, detailed body.
```
Add LLM provider package stubs (DashScope, DeepSeek, Gemini)
```

Go module: `github.com/sushistack/youtube.pipeline`, Go 1.25.7. Dependencies: Cobra v1.10.2, ncruces/go-sqlite3 v0.33.3.

### File Layout After This Story

```
internal/domain/
  doc.go       # unchanged (from Story 1.1)
  types.go     # NEW — Stage, Run, Episode, Shot, Decision, NormalizedResponse
  errors.go    # NEW — DomainError, 7 sentinels, Classify()
  llm.go       # NEW — TextGenerator, ImageGenerator, TTSSynthesizer + request/response types
  types_test.go   # NEW — Stage count, JSON tag, struct field tests
  errors_test.go  # NEW — Classification, errors.Is/As, Classify() tests
  llm_test.go     # NEW — Interface satisfaction checks

internal/clock/
  doc.go       # unchanged (from Story 1.1)
  clock.go     # NEW — Clock interface, RealClock, FakeClock
  clock_test.go   # NEW — FakeClock Advance, Sleep, context cancellation
```

No files outside `internal/domain/` and `internal/clock/` are created or modified.

### Project Structure Notes

- Alignment with unified project structure: all paths confirmed to exist from Story 1.1 scaffolding
- `internal/domain/` and `internal/clock/` currently contain only `doc.go` stubs — ready for implementation
- No conflicts with existing files

### References

- Stage constants: [architecture.md lines 742–759](../_bmad-output/planning-artifacts/architecture.md)
- Error classification table: [architecture.md lines 612–622](../_bmad-output/planning-artifacts/architecture.md)
- Provider interfaces: [architecture.md lines 826–841](../_bmad-output/planning-artifacts/architecture.md)
- Domain file organization: [architecture.md lines 1515–1521](../_bmad-output/planning-artifacts/architecture.md)
- Import direction rule: [architecture.md lines 1059–1076](../_bmad-output/planning-artifacts/architecture.md)
- Interface definition rule: [architecture.md lines 1079–1084](../_bmad-output/planning-artifacts/architecture.md)
- Naming conventions: [architecture.md lines 1013–1024](../_bmad-output/planning-artifacts/architecture.md)
- Clock usage in retry: [architecture.md lines 870–885](../_bmad-output/planning-artifacts/architecture.md)
- mapDomainError pattern: [architecture.md lines 1194–1203](../_bmad-output/planning-artifacts/architecture.md)
- `runs` table DDL: [migrations/001_init.sql](../../migrations/001_init.sql) (Story 1.2)
- `segments` table DDL: [migrations/001_init.sql](../../migrations/001_init.sql) (Story 1.2)
- `decisions` table DDL: [migrations/001_init.sql](../../migrations/001_init.sql) (Story 1.2)
- Epic 1 Story 1.3 AC: [epics.md lines 713–741](../_bmad-output/planning-artifacts/epics.md)
- Sprint prompt context: [sprint-prompts.md lines 64–71](../_bmad-output/planning-artifacts/sprint-prompts.md)
- Story 1.2 implementation: [1-2-sqlite-database-migration-infrastructure.md](1-2-sqlite-database-migration-infrastructure.md)

## Dev Agent Record

### Agent Model Used

claude-opus-4-6

### Debug Log References

None

### Completion Notes List

- `types.go`: Stage (15 constants + AllStages slice), Run (15 fields), Episode (13 fields), Shot (4 fields), Decision (13 fields), NormalizedResponse (8 fields) — all with snake_case JSON tags, pointer types for nullable DB columns
- `errors.go`: DomainError struct, 7 sentinel errors with correct HTTP status/retryable classification, Classify() helper using errors.As
- `llm.go`: TextGenerator, ImageGenerator (Generate+Edit), TTSSynthesizer interfaces with request/response types. Doc comments specify *http.Client constructor requirement
- `clock.go`: Clock interface, RealClock (real time + context-aware Sleep), FakeClock (Advance wakes pending Sleep calls, mutex-protected, channel-signaled)
- Import direction verified: domain/ and clock/ import only Go stdlib
- 22 tests total: 15 domain (stage count, JSON tags, error classification, errors.Is/As, Classify, JSON roundtrip) + 3 compile-time interface checks + 7 clock (Now, Advance, Sleep resolve, context cancel, zero duration)
- No `Is(target error) bool` method needed on DomainError — sentinel pointers work naturally with errors.Is via pointer comparison through wrapping

### Change Log

- 2026-04-17: Story 1.3 implemented — domain types, error system, provider interfaces, clock abstraction, 22 tests passing

### File List

- internal/domain/types.go (new)
- internal/domain/errors.go (new)
- internal/domain/llm.go (new)
- internal/domain/types_test.go (new)
- internal/domain/errors_test.go (new)
- internal/domain/llm_test.go (new)
- internal/clock/clock.go (new)
- internal/clock/clock_test.go (new)
