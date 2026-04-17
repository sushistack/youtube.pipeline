# Story 1.6: CLI Renderer & Output Formatting

Status: done

## Story

As an operator,
I want both human-readable and JSON output for CLI commands,
so that I can use the tool interactively and script against it programmatically.

## Acceptance Criteria

1. **AC-INTERFACE:** `cmd/pipeline/render.go` defines a `Renderer` interface with `RenderSuccess(data any)` and `RenderError(err error)` methods.

2. **AC-HUMAN:** `HumanRenderer` writes color-coded hierarchical output to a configured `io.Writer`:
   - Green (`\033[32m`) for success/passing items
   - Yellow/amber (`\033[33m`) for warnings
   - Red (`\033[31m`) for errors/failures
   - Reset (`\033[0m`) after every color sequence

3. **AC-JSON:** `JSONRenderer` writes versioned envelope JSON to a configured `io.Writer`:
   - Success: `{"version": 1, "data": {...}}`
   - Error: `{"version": 1, "error": {"code": "...", "message": "...", "recoverable": true/false}}`
   - Error codes map to the 7-category classification from `domain.Classify(err)` (Story 1.3)
   - Unclassified errors map to `INTERNAL_ERROR` with `recoverable: false`

4. **AC-FLAG:** `--json` persistent flag on the root command. When set, all commands use `JSONRenderer`; otherwise `HumanRenderer` is default.

5. **AC-DOCTOR-HUMAN:** `pipeline doctor` (without `--json`) prints each check with `✓` (green) or `✗` (red) + remediation hints. Summary line shows pass count.

6. **AC-DOCTOR-JSON:** `pipeline doctor --json` outputs valid JSON matching the versioned envelope: `{"version": 1, "data": {"checks": [...], "passed": true/false}}`.

7. **AC-INIT-HUMAN:** `pipeline init` (without `--json`) prints colored success message with created paths.

8. **AC-INIT-JSON:** `pipeline init --json` outputs valid JSON envelope with created paths.

9. **AC-ERROR-JSON:** When any command fails with `--json`, output is `{"version": 1, "error": {"code": "...", "message": "...", "recoverable": ...}}` — Cobra's own error printing is suppressed.

10. **AC-TEST:** Unit tests: Renderer interface compliance for both impls; JSON envelope structure validation; human output contains ANSI color codes; round-trip JSON parsing; doctor output in both modes; init output in both modes; all 7 domain error codes + INTERNAL_ERROR fallback.

## Tasks / Subtasks

- [ ] **T1: `cmd/pipeline/render.go` — Renderer interface + both implementations** (AC: #1, #2, #3)
  - [ ] Define `Renderer` interface: `RenderSuccess(data any)`, `RenderError(err error)`
  - [ ] Define ANSI color constants: `colorReset`, `colorRed`, `colorGreen`, `colorYellow`
  - [ ] Define JSON envelope types: `Envelope` (version, data, error), `ErrorInfo` (code, message, recoverable)
  - [ ] Define output data types: `DoctorOutput` (checks, passed), `CheckResult` (name, passed, message), `InitOutput` (config, env, database, output)
  - [ ] Implement `HumanRenderer` — `io.Writer` field, type-switch in RenderSuccess for known data types, red error output for RenderError
  - [ ] Implement `JSONRenderer` — `io.Writer` field, `json.NewEncoder` for both methods, `domain.Classify(err)` in RenderError
  - [ ] Constructor functions: `NewHumanRenderer(w io.Writer)`, `NewJSONRenderer(w io.Writer)`

- [ ] **T2: `cmd/pipeline/main.go` — Add `--json` flag + renderer wiring** (AC: #4, #9)
  - [ ] Add `var jsonOutput bool` package-level variable
  - [ ] Add `rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")`
  - [ ] Add `newRenderer(w io.Writer) Renderer` helper function
  - [ ] Add `PersistentPreRunE` to suppress Cobra error output when `--json` is active

- [ ] **T3: `cmd/pipeline/doctor.go` — Refactor to use Renderer** (AC: #5, #6)
  - [ ] Convert `config.Result` slice to `[]CheckResult` slice
  - [ ] Build `DoctorOutput` with checks and `config.AllPassed(results)`
  - [ ] Call `renderer.RenderSuccess(doctorOutput)`
  - [ ] On config load error, call `renderer.RenderError(err)` when `--json`
  - [ ] Keep `errDoctorFailed` return for exit code 1

- [ ] **T4: `cmd/pipeline/init.go` — Refactor to use Renderer** (AC: #7, #8)
  - [ ] Build `InitOutput` from created paths
  - [ ] Call `renderer.RenderSuccess(initOutput)` on success
  - [ ] On error with `--json`, call `renderer.RenderError(err)`

- [ ] **T5: `cmd/pipeline/render_test.go` — Comprehensive tests** (AC: #10)
  - [ ] Test `HumanRenderer.RenderSuccess` with `DoctorOutput`: contains ANSI green for passing, red for failing
  - [ ] Test `HumanRenderer.RenderSuccess` with `InitOutput`: contains path strings with color
  - [ ] Test `HumanRenderer.RenderError`: output contains red ANSI code
  - [ ] Test `JSONRenderer.RenderSuccess` with `DoctorOutput`: valid JSON, `version == 1`, `data.checks` present
  - [ ] Test `JSONRenderer.RenderSuccess` with `InitOutput`: valid JSON, `version == 1`, `data.config` present
  - [ ] Test `JSONRenderer.RenderError` with each of 7 domain errors: correct code field
  - [ ] Test `JSONRenderer.RenderError` with plain `errors.New()`: falls back to `INTERNAL_ERROR`
  - [ ] Test round-trip: marshal via JSONRenderer → unmarshal → verify all fields
  - [ ] Test doctor full flow: both human and JSON modes produce expected output shapes

## Dev Notes

### Renderer Interface Design

The `Renderer` interface lives in `cmd/pipeline/render.go`. Per the architecture's Interface Definition Rule (interfaces in consuming package), the CLI package consumes the Renderer.

```go
type Renderer interface {
    RenderSuccess(data any)
    RenderError(err error)
}
```

Both implementations take `io.Writer` at construction. Tests use `bytes.Buffer` to capture output.

### ANSI Color Constants

Raw ANSI escape codes. No external color library dependency.

```go
const (
    colorReset  = "\033[0m"
    colorRed    = "\033[31m"
    colorGreen  = "\033[32m"
    colorYellow = "\033[33m"
)
```

### HumanRenderer Type Switching

`RenderSuccess` type-switches on known output data types. Each new command adds one case. Architecture calls this a "small interface, implement early, extend cheaply."

```go
func (r *HumanRenderer) RenderSuccess(data any) {
    switch v := data.(type) {
    case *DoctorOutput:
        r.renderDoctor(v)
    case *InitOutput:
        r.renderInit(v)
    default:
        fmt.Fprintf(r.w, "%v\n", data)
    }
}
```

For `DoctorOutput`: print each check with `✓` (green) or `✗` (red), message on failure, summary line at end.

For `InitOutput`: print "Initialized youtube.pipeline:" header in green, each path indented.

`RenderError`: print error message in red.

### JSON Envelope Types

```go
type Envelope struct {
    Version int        `json:"version"`
    Data    any        `json:"data,omitempty"`
    Error   *ErrorInfo `json:"error,omitempty"`
}

type ErrorInfo struct {
    Code        string `json:"code"`
    Message     string `json:"message"`
    Recoverable bool   `json:"recoverable"`
}
```

All JSON fields are `snake_case` per architecture mandate.

### JSONRenderer Error Mapping

`RenderError` reuses `domain.Classify(err)` — DO NOT reimplement the error classification. Story 1.3's `Classify()` already handles `errors.As` unwrapping and returns `(httpStatus, code, retryable)`. The renderer only needs `code` and `retryable`.

```go
func (r *JSONRenderer) RenderError(err error) {
    _, code, recoverable := domain.Classify(err)
    env := Envelope{
        Version: 1,
        Error: &ErrorInfo{
            Code:        code,
            Message:     err.Error(),
            Recoverable: recoverable,
        },
    }
    json.NewEncoder(r.w).Encode(env)
}
```

### Output Data Types

```go
type DoctorOutput struct {
    Checks []CheckResult `json:"checks"`
    Passed bool          `json:"passed"`
}

type CheckResult struct {
    Name    string `json:"name"`
    Passed  bool   `json:"passed"`
    Message string `json:"message,omitempty"`
}

type InitOutput struct {
    Config   string `json:"config"`
    Env      string `json:"env"`
    Database string `json:"database"`
    Output   string `json:"output"`
}
```

These types live in `render.go` — they are output-layer concerns, not business logic.

### Command Wiring Pattern

In `main.go`:

```go
var jsonOutput bool

func newRenderer(w io.Writer) Renderer {
    if jsonOutput {
        return NewJSONRenderer(w)
    }
    return NewHumanRenderer(w)
}
```

Each command calls `newRenderer(cmd.OutOrStdout())` inside its `RunE`. This is the simplest approach — no Cobra middleware complexity.

**Cobra error suppression for `--json`:** Set `PersistentPreRunE` on the root command to suppress Cobra's native error/usage printing when `--json` is active. This prevents Cobra from mixing plain-text errors into JSON output.

```go
rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
    if jsonOutput {
        cmd.Root().SilenceErrors = true
        cmd.Root().SilenceUsage = true
    }
    return nil
}
```

### Doctor Refactoring

Current `runDoctor` calls `config.FormatResults(results)` which returns a string. After refactoring:

1. Run checks → `config.Result` slice
2. Convert to `[]CheckResult`
3. Build `DoctorOutput{Checks: checks, Passed: config.AllPassed(results)}`
4. Call `renderer.RenderSuccess(&doctorOutput)`
5. If `!doctorOutput.Passed`, return `errDoctorFailed` for exit code 1

`config.FormatResults()` and `config.AllPassed()` stay in `config/doctor.go` — no modifications to that package. `AllPassed()` is still used for the pass/fail exit code decision.

### Init Refactoring

Current `runInit` uses `fmt.Fprintf` for output. After refactoring:

1. Run all init steps (unchanged)
2. Build `InitOutput{Config: cfgPath, Env: envPath, Database: cfg.DBPath, Output: cfg.OutputDir}`
3. Call `renderer.RenderSuccess(&initOutput)`

### File Changes

```
cmd/pipeline/
  render.go              # NEW — Renderer interface, HumanRenderer, JSONRenderer, output types
  render_test.go         # NEW — comprehensive tests
  main.go                # MODIFIED — add --json flag, newRenderer(), PersistentPreRunE
  doctor.go              # MODIFIED — use Renderer instead of fmt.Fprintf
  init.go                # MODIFIED — use Renderer instead of fmt.Fprintf
```

### Critical Constraints

- **No external color libraries.** Raw ANSI escape codes only.
- **No testify, no gomock.** Go stdlib `testing` package only. Use `testutil.AssertEqual[T]` and `testutil.AssertJSONEq`.
- **`domain.Classify(err)` reuse.** Do NOT reimplement error classification. Import and call `domain.Classify()` directly.
- **snake_case for ALL JSON fields.** `version`, `data`, `error`, `code`, `message`, `recoverable`, `checks`, `passed`, `name`, `config`, `env`, `database`, `output`.
- **`cmd/pipeline/` package only.** No new `internal/` packages for this story.
- **Exit codes preserved.** `pipeline doctor` returns exit 1 if any check fails, regardless of `--json`.
- **`io.Writer` injection.** Renderers take `io.Writer` at construction. Commands pass `cmd.OutOrStdout()`. Tests pass `bytes.Buffer`.
- **Existing `config.FormatResults` NOT deleted.** It remains in `config/doctor.go`. The `HumanRenderer` handles formatting now, but FormatResults stays to avoid breaking config package tests.
- **Cobra `SilenceErrors` / `SilenceUsage`** must be set only when `--json` is active. Default (non-JSON) behavior keeps Cobra's error printing.
- **Import direction:** `cmd/pipeline/` imports `internal/domain/` and `internal/config/`. This is correct per architecture rules.
- **300-line domain/ cap** doesn't apply to `cmd/` but keep `render.go` focused — split if it approaches 250 lines.

### Previous Story Intelligence

**Story 1.5 (review):** CLI init + doctor commands.
- `doctor.go` outputs via `fmt.Fprintln(cmd.OutOrStdout(), ...)` — refactor target
- `init.go` outputs via `fmt.Fprintln(cmd.OutOrStdout(), ...)` — refactor target
- `config.FormatResults()` uses `✓`/`✗` Unicode without color — HumanRenderer adds color
- `config.AllPassed()` used for exit code — keep this
- `errDoctorFailed` sentinel for exit code 1 — keep this pattern
- `cfgPath` is package-level var bound to `--config` persistent flag — `jsonOutput` follows same pattern
- `SilenceUsage: true` already set on doctorCmd — we move this logic to root when `--json`
- Existing tests: `cmd/pipeline/doctor_test.go`, `cmd/pipeline/init_test.go` — check these still pass after refactoring

**Story 1.3 (done):** Domain types and error system.
- `domain.DomainError` struct: `Code string`, `Message string`, `HTTPStatus int`, `Retryable bool`
- `domain.Classify(err)` returns `(httpStatus int, code string, retryable bool)` via `errors.As`
- 7 sentinel errors with exact codes: `RATE_LIMITED`, `UPSTREAM_TIMEOUT`, `STAGE_FAILED`, `VALIDATION_ERROR`, `CONFLICT`, `COST_CAP_EXCEEDED`, `NOT_FOUND`
- Fallback: `500, "INTERNAL_ERROR", false` for unclassified errors
- Error wrapping pattern: `fmt.Errorf("context: %w", err)`

**Story 1.4 (review):** Test infrastructure.
- `testutil.AssertEqual[T]` — use for all test value assertions
- `testutil.AssertJSONEq` — use for JSON output validation (semantic equality)
- Test patterns: `t.Run` subtests, `t.Helper()`, `t.TempDir()`, `bytes.Buffer` for output capture

### Git Intelligence

Recent commits: `"Add LLM provider package stubs"`, `"Add CLI init and doctor commands with Viper config loading"`.

Commit style for this story: `Add CLI renderer with human-readable and JSON output formatting`.

### Project Structure Notes

- `cmd/pipeline/` currently has: `main.go`, `init.go`, `init_test.go`, `doctor.go`, `doctor_test.go`
- `render.go` and `render_test.go` are NEW files
- No new `internal/` packages needed
- `internal/domain/errors.go` has `Classify()` — imported, not modified
- `internal/config/doctor.go` has `FormatResults()`, `AllPassed()`, `Result` — not modified, but doctor.go stops calling `FormatResults`

### References

- Epic 1 Story 1.6 AC: [epics.md:835-863](_bmad-output/planning-artifacts/epics.md)
- FR42 (JSON CLI output): [prd.md — FR42](_bmad-output/planning-artifacts/prd.md)
- FR43 (human-readable CLI output): [prd.md — FR43](_bmad-output/planning-artifacts/prd.md)
- Rendering abstraction as Tier 3 concern: [architecture.md:213](_bmad-output/planning-artifacts/architecture.md)
- Error classification table: [architecture.md:612-622](_bmad-output/planning-artifacts/architecture.md)
- Response envelope spec: [architecture.md:605-610](_bmad-output/planning-artifacts/architecture.md)
- `domain.Classify` function: [internal/domain/errors.go:28-34](internal/domain/errors.go)
- `mapDomainError` (API-side equivalent): [architecture.md:1194-1203](_bmad-output/planning-artifacts/architecture.md)
- Existing doctor command: [cmd/pipeline/doctor.go](cmd/pipeline/doctor.go)
- Existing init command: [cmd/pipeline/init.go](cmd/pipeline/init.go)
- Operator Tooling domain: [architecture.md:1756](_bmad-output/planning-artifacts/architecture.md)
- Project structure: [architecture.md:1497-1616](_bmad-output/planning-artifacts/architecture.md)
- `testutil.AssertEqual`: [internal/testutil/assert.go](internal/testutil/assert.go)
- `testutil.AssertJSONEq`: [internal/testutil/assert.go:18-30](internal/testutil/assert.go)
- `config.Result` type: [internal/config/doctor.go:18-22](internal/config/doctor.go)
- `config.AllPassed` helper: [internal/config/doctor.go:151-158](internal/config/doctor.go)

### Review Findings

- [x] [Review][Patch] F1: nil error → RenderError panic — added nil guard to both HumanRenderer.RenderError and JSONRenderer.RenderError [render.go]
- [x] [Review][Patch] F2: `--json` error double-output — introduced silentErr wrapper; doctor wraps errDoctorFailed in silentErr; main skips re-rendering for silentErr; removed in-command renderer.RenderError in doctor config path [render.go, doctor.go, main.go]
- [x] [Review][Patch] F3: Missing `--json` error-path tests — added 5 tests: nil error safety (×2), silentErr behavior, config error JSON output, doctor failure silentErr wrapping [render_test.go]
- [x] [Review][Defer] F4: ANSI color codes emitted without TTY/NO_COLOR detection [render.go:11-17] — deferred, out of Story 1.6 scope
- [x] [Review][Defer] F5: TOCTOU race in writeConfigIfNotExists [init.go:76-79] — deferred, pre-existing Story 1.5
- [x] [Review][Defer] F6: TOCTOU race in writeEnvIfNotExists [init.go:90-93] — deferred, pre-existing Story 1.5
- [x] [Review][Defer] F7: database.Close() error silently discarded [init.go:63] — deferred, pre-existing Story 1.5

## Dev Agent Record

### Agent Model Used

claude-opus-4-6

### Debug Log References

None

### Completion Notes List

- `render.go`: `Renderer` interface with `RenderSuccess(data any)` and `RenderError(err error)`. `HumanRenderer` (ANSI color-coded, type-switching on `*DoctorOutput`/`*InitOutput`) and `JSONRenderer` (versioned envelope `{"version": 1, ...}` using `domain.Classify` for error mapping). Envelope, ErrorInfo, DoctorOutput, CheckResult, InitOutput types with snake_case JSON tags.
- `main.go`: `--json` persistent flag, `newRenderer(w)` helper, `PersistentPreRunE` suppresses Cobra error/usage output when `--json` active, main error handler renders JSON to stderr.
- `doctor.go`: Refactored to convert `config.Result` → `CheckResult`, build `DoctorOutput`, call `renderer.RenderSuccess()`. Kept `errDoctorFailed` for exit code 1.
- `init.go`: Refactored to build `InitOutput` from created paths, call `renderer.RenderSuccess()`.
- `render_test.go`: 19 tests — HumanRenderer (doctor all-pass, with-failure, init, unknown type, error), JSONRenderer (doctor, init, snake_case fields, all 7 domain errors, wrapped error, INTERNAL_ERROR fallback, snake_case error fields), round-trip (doctor, init, error), semantic JSON equality, interface compliance, newRenderer helper.
- All 25 cmd/pipeline tests pass. Zero regressions across all packages.
- No external color libraries — raw ANSI codes only.
- `config.FormatResults()` preserved in config package, just no longer called from doctor.go.

### Change Log

- 2026-04-17: Story 1.6 implemented — CLI renderer with human-readable and JSON output formatting, 19 new tests, all passing

### File List

- cmd/pipeline/render.go (new)
- cmd/pipeline/render_test.go (new)
- cmd/pipeline/main.go (modified)
- cmd/pipeline/doctor.go (modified)
- cmd/pipeline/init.go (modified)
