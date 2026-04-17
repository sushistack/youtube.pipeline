# Story 1.7: CI Pipeline & E2E Smoke Test

Status: done

## Story

As a developer,
I want CI running green from Day 1 with contract tests, layer-import linting, and an E2E smoke test,
so that every commit is validated against the full quality gate suite.

## Acceptance Criteria

1. **AC-CI-WORKFLOW:** `.github/workflows/ci.yml` defines 4 jobs: `test-go` and `test-web` run in parallel (no `needs` dependency), then `test-e2e` and `build` run sequentially after both pass (`needs: [test-go, test-web]`). Trigger: push to any branch.

2. **AC-CACHE:** Go module cache via `actions/setup-go` (`cache: true`), npm cache via `actions/setup-node` (`cache: 'npm'`, `cache-dependency-path: 'web/package-lock.json'`), and Playwright Chromium binary cached via `actions/cache` keyed on `e2e/package-lock.json`.

3. **AC-NO-KEYS:** No API keys are injected into CI environment. No secrets are referenced in `ci.yml`. `testutil.CILockout` (Story 1.4) panics if API keys are detected in CI. This is Layer 3 of the 3-layer API isolation defense.

4. **AC-CONTRACT:** Contract tests execute as part of `test-go` job. `go test ./internal/testutil/ -run Contract` loads JSON Schema fixtures from `testdata/contracts/`, unmarshals into corresponding Go structs, and validates without error.

5. **AC-LAYER-LINT:** A Go-based layer-import linter script (`scripts/check-layer-imports.go`) enforces import direction: `cmd/` may import anything; `internal/api/` may import `internal/service/`, `internal/domain/`, `internal/db/`, `internal/pipeline/`, `internal/clock/`, `internal/web/`; `internal/service/` may import `internal/domain/`, `internal/db/`, `internal/pipeline/`, `internal/clock/`; `internal/pipeline/` may import `internal/domain/`, `internal/db/`, `internal/llmclient/`, `internal/clock/`; `internal/db/` may import `internal/domain/`; `internal/domain/` imports nothing from `internal/`; `internal/clock/` imports nothing from `internal/`; `internal/llmclient/` may import `internal/domain/`, `internal/clock/`. Violations cause CI build failure (NFR-M4).

6. **AC-FR-COVERAGE:** `testdata/fr-coverage.json` maps FR IDs to test function names. `scripts/check-fr-coverage.go` validates: (a) every mapped `test_ids[]` entry resolves to an existing `Test*` or `test*` function, (b) annotated FRs do not exceed 15% of total. Mode is `grace: true` — unmapped FRs produce warnings, not failures. A `// TODO: Switch to strict mode after Epic 6 routes exist` comment is present in the validator script.

7. **AC-E2E-GO:** `internal/pipeline/e2e_test.go` contains `TestE2E_FullPipeline` exercising Phase A -> B -> C with mocked providers (all `domain.TextGenerator`, `domain.ImageGenerator`, `domain.TTSSynthesizer` injected as test doubles). Uses `testutil.BlockExternalHTTP(t)`. Verifies output artifacts exist: `scenario.json`, `images/` directory, `tts/` directory, `output.mp4`, `metadata.json`, `manifest.json`.

8. **AC-E2E-RUN:** The `test-go` job runs `go test -run E2E ./internal/...` in a separate step (after regular unit/integration tests) to clearly identify E2E test results. Alternatively, E2E tests run as part of the standard `go test ./internal/...` command — either approach is acceptable as long as E2E tests execute in CI.

9. **AC-WALL-CLOCK:** Total CI wall-clock time is <= 10 minutes (NFR-T6). `test-go` and `test-web` run in parallel to stay within budget.

10. **AC-RACE-FLAG:** The `-race` deferred issue from Story 1.1 is resolved. Since `CGO_ENABLED=0` is required (ncruces/go-sqlite3 is pure Go), the `-race` flag is NOT used in CI. A comment in `ci.yml` documents this decision: `# -race requires CGO; ncruces/go-sqlite3 is pure Go, no CGO needed`.

11. **AC-TEST:** CI validation: all 4 jobs green on current codebase; contract test passes; layer-import lint passes; fr-coverage validator runs in grace mode; E2E smoke test passes with mocked APIs.

## Tasks / Subtasks

- [x] **T1: `.github/workflows/ci.yml` — CI pipeline configuration** (AC: #1, #2, #3, #9, #10)
  - [x] Create `.github/workflows/` directory
  - [x] Define workflow with `on: push` trigger
  - [x] `test-go` job: `runs-on: ubuntu-latest`, `actions/checkout@v4`, `actions/setup-go@v5` (Go 1.25.7, `cache: true`), `CGO_ENABLED=0 go test ./cmd/... ./internal/... -count=1 -timeout=120s`
  - [x] `test-go` job: layer-import lint step (`go run ./scripts/lintlayers/`)
  - [x] `test-go` job: fr-coverage validation step (`go run ./scripts/frcoverage/`)
  - [x] `test-web` job: `runs-on: ubuntu-latest`, `actions/checkout@v4`, `actions/setup-node@v4` (Node LTS, `cache: 'npm'`, `cache-dependency-path: 'web/package-lock.json'`), `cd web && npm ci && npx vitest run`
  - [x] `test-e2e` job: `needs: [test-go, test-web]`, Playwright Chromium cache, `cd e2e && npx playwright install --with-deps chromium && npx playwright test`
  - [x] `build` job: `needs: [test-go, test-web]`, full `make build` (web-build + go-build)
  - [x] No `secrets:` references anywhere in the file
  - [x] Comment explaining `-race` exclusion

- [x] **T2: `scripts/lintlayers/main.go` — Layer-import linter** (AC: #5)
  - [x] Standalone Go script (uses `go/parser`, `go/token`, `go/ast` from stdlib)
  - [x] Parse all `.go` files under `internal/`
  - [x] Define allowed import rules per package layer
  - [x] Report violations with file path + line number + violating import
  - [x] Exit code 1 on any violation
  - [x] Unit test: `scripts/lintlayers/main_test.go`

- [x] **T3: `testdata/fr-coverage.json` + `scripts/frcoverage/main.go` — FR coverage validator** (AC: #6)
  - [x] Create `testdata/fr-coverage.json` with initial mappings for implemented FRs (FR38, FR39, FR42, FR43, FR46, FR51, FR52)
  - [x] Script loads JSON, validates test_ids resolve to `Test*` functions via `go test -list`
  - [x] Grace mode: warn on unmapped FRs, do not fail
  - [x] Annotated FR count <= 15% check
  - [x] `// TODO: Switch to strict mode after Epic 6 routes exist` comment in script

- [x] **T4: `internal/pipeline/e2e_test.go` — FR52-go E2E smoke test** (AC: #7, #8)
  - [x] Create `TestE2E_FullPipeline` test function
  - [x] Call `testutil.BlockExternalHTTP(t)` at test start
  - [x] Create mock implementations of `domain.TextGenerator`, `domain.ImageGenerator`, `domain.TTSSynthesizer`
  - [x] Set up temporary output directory via `t.TempDir()`
  - [x] Execute pipeline Phase A -> B -> C with mocked providers (t.Skip until implemented)
  - [x] Assert output artifacts exist: `scenario.json`, `images/`, `tts/`, `output.mp4`, `metadata.json`, `manifest.json`

- [x] **T5: Makefile updates** (AC: #1)
  - [x] Add `lint-layers` target: `go run ./scripts/lintlayers/`
  - [x] Add `check-fr-coverage` target: `go run ./scripts/frcoverage/`
  - [x] Add `ci` target that runs all CI-equivalent checks locally

- [x] **T6: Verify green CI locally** (AC: #11)
  - [x] Run `make test` — all existing tests pass (25 cmd + 7 clock + 19 config + 12 db + 17 domain + 1 pipeline + 17 testutil + 2 web = 100 tests)
  - [x] Run `make lint-layers` — no violations
  - [x] Run `make check-fr-coverage` — grace mode warnings only (7 mapped, 41 unmapped)
  - [x] Run `go test -run E2E ./internal/...` — E2E passes (skipped, by design)
  - [x] Run `make build` — binary builds successfully

### Review Findings

- [x] [Review][Patch] frcoverage: lowercase "test" prefix captures non-test output lines [scripts/frcoverage/main.go:139] — fixed: removed `"test"` prefix, now only matches `"Test"` (capital T)
- [x] [Review][Patch] Lintlayers: sub-package self-import incorrectly flagged [scripts/lintlayers/main.go:93] — fixed: added self-import skip before allowed-list check
- [x] [Review][Patch] Lintlayers: unknown packages silently skipped [scripts/lintlayers/main.go:79] — fixed: added stderr warning for unregistered packages
- [x] [Review][Defer] BlockExternalHTTP not concurrency-safe — mutates global http.DefaultTransport [internal/testutil/nohttp.go:15] — deferred, pre-existing (Story 1.4 scope), relevant when parallel tests introduced
- [x] [Review][Defer] BlockExternalHTTP doesn't block custom-transport HTTP clients [internal/testutil/nohttp.go] — deferred, architecture limitation of Layer 2 defense
- [x] [Review][Defer] test-e2e continue-on-error masks real failures post-Epic 6 [.github/workflows/ci.yml:46] — deferred, by design until pipeline serve exists

## Dev Notes

### CI Workflow YAML Structure

The complete `.github/workflows/ci.yml` structure. Note: `test-go` and `test-web` have NO `needs:` field (parallel). `test-e2e` and `build` both have `needs: [test-go, test-web]`.

```yaml
name: CI

on:
  push:
    branches: ['**']
  pull_request:
    branches: [main]

jobs:
  test-go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.7'
          cache: true
      # -race requires CGO; ncruces/go-sqlite3 is pure Go, no CGO needed.
      # See deferred-work.md (Story 1.1): -race excluded from CI.
      - name: Run Go tests
        run: CGO_ENABLED=0 go test ./cmd/... ./internal/... -count=1 -timeout=120s
      - name: Layer-import lint (NFR-M4)
        run: CGO_ENABLED=0 go run scripts/check-layer-imports.go
      - name: FR coverage check (grace mode)
        run: CGO_ENABLED=0 go run scripts/check-fr-coverage.go

  test-web:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 'lts/*'
          cache: 'npm'
          cache-dependency-path: 'web/package-lock.json'
      - name: Install dependencies
        run: cd web && npm ci
      - name: Run Vitest
        run: cd web && npx vitest run

  test-e2e:
    needs: [test-go, test-web]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 'lts/*'
          cache: 'npm'
          cache-dependency-path: 'e2e/package-lock.json'
      - name: Install E2E dependencies
        run: cd e2e && npm ci
      - name: Cache Playwright browsers
        uses: actions/cache@v4
        with:
          path: ~/.cache/ms-playwright
          key: playwright-${{ hashFiles('e2e/package-lock.json') }}
      - name: Install Playwright Chromium
        run: cd e2e && npx playwright install --with-deps chromium
      - name: Build application
        uses: actions/setup-go@v5
        with:
          go-version: '1.25.7'
          cache: true
      - name: Build web
        uses: actions/setup-node@v4
        with:
          node-version: 'lts/*'
          cache: 'npm'
          cache-dependency-path: 'web/package-lock.json'
      - run: make build
      - name: Start server and run E2E
        run: |
          ./bin/pipeline serve &
          sleep 3
          cd e2e && npx playwright test
        env:
          # No API keys — Layer 3 defense
          CI: true

  build:
    needs: [test-go, test-web]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.7'
          cache: true
      - uses: actions/setup-node@v4
        with:
          node-version: 'lts/*'
          cache: 'npm'
          cache-dependency-path: 'web/package-lock.json'
      - name: Install web dependencies
        run: cd web && npm ci
      - name: Build
        run: CGO_ENABLED=0 make build
```

**Critical:** The `test-e2e` job requires the application binary to be built and running. The `build` job verifies the full build chain independently. These are separate concerns.

**Note on test-e2e:** The `test-e2e` job runs Playwright against the running application. Since `pipeline serve` may not be implemented yet (Epic 6), the `test-e2e` job should be configured to gracefully handle this. The existing `e2e/smoke.spec.ts` uses `test.todo()` which auto-skips. When the serve command exists, this will become a real test.

**Practical approach:** Until `pipeline serve` is implemented, the `test-e2e` job will either pass (because `test.todo` counts as skipped) or can be configured with `continue-on-error: true` temporarily. The important thing is the job structure exists for when routes are available (Epic 6).

### Layer-Import Linter Design

Use Go's `go/parser` standard library to parse AST of all `.go` files under `internal/`. Extract import paths and validate against the allowed-imports map.

**File:** `scripts/check-layer-imports.go`

```go
package main

// Allowed import direction rules:
// domain/    → (nothing from internal/)
// clock/     → (nothing from internal/)
// db/        → domain/
// llmclient/ → domain/, clock/
// pipeline/  → domain/, db/, llmclient/, clock/
// service/   → domain/, db/, pipeline/, clock/
// api/       → domain/, db/, service/, pipeline/, clock/, web/
// config/    → domain/
// hitl/      → domain/
// testutil/  → domain/, db/ (test infrastructure, broader access allowed)
// web/       → (nothing from internal/ — only embed.go)

var allowedImports = map[string][]string{
    "internal/domain":    {},
    "internal/clock":     {},
    "internal/db":        {"internal/domain"},
    "internal/llmclient": {"internal/domain", "internal/clock"},
    "internal/pipeline":  {"internal/domain", "internal/db", "internal/llmclient", "internal/clock"},
    "internal/service":   {"internal/domain", "internal/db", "internal/pipeline", "internal/clock"},
    "internal/api":       {"internal/domain", "internal/db", "internal/service", "internal/pipeline", "internal/clock", "internal/web"},
    "internal/config":    {"internal/domain"},
    "internal/hitl":      {"internal/domain"},
    "internal/testutil":  {"internal/domain", "internal/db"},
    "internal/web":       {},
}
```

**Logic:**
1. Walk `internal/` directory
2. Parse each `.go` file's imports
3. For each import starting with `github.com/sushistack/youtube.pipeline/internal/`, extract the internal path
4. Look up the file's package in `allowedImports`
5. Check if the imported internal path is in the allowed list (or is a sub-package of an allowed path)
6. Report violations: `VIOLATION: internal/service/run_service.go:5 imports internal/api (not allowed for internal/service)`
7. Exit 1 if any violations found

**Sub-package handling:** `internal/llmclient/dashscope` is a sub-package of `internal/llmclient`. The linter must match the top-level package prefix. A file in `internal/pipeline/` importing `internal/llmclient/dashscope/...` should be checked against `internal/llmclient`.

**Test files:** `_test.go` files should be linted too. Test files in `internal/pipeline/` should follow the same import rules as production files.

### FR Coverage JSON Schema

**File:** `testdata/fr-coverage.json`

```json
{
  "meta": {
    "grace": true,
    "total_frs": 48,
    "last_updated": "2026-04-17"
  },
  "coverage": [
    {
      "fr_id": "FR38",
      "test_ids": ["TestInitCmd_CreatesConfigDir", "TestInitCmd_Idempotent"],
      "annotation": null
    },
    {
      "fr_id": "FR39",
      "test_ids": ["TestDoctorCmd_AllPass", "TestDoctorCmd_MissingKey"],
      "annotation": null
    },
    {
      "fr_id": "FR42",
      "test_ids": [],
      "annotation": "not-directly-testable, covered by AC-JSON in Story 1.6"
    },
    {
      "fr_id": "FR43",
      "test_ids": [],
      "annotation": "not-directly-testable, covered by AC-HUMAN in Story 1.6"
    },
    {
      "fr_id": "FR46",
      "test_ids": ["TestWriterCriticCheck_SameProvider"],
      "annotation": null
    },
    {
      "fr_id": "FR51",
      "test_ids": ["TestContract_PipelineState", "TestLoadFixture", "TestLoadRunStateFixture", "TestBlockExternalHTTP_External", "TestCILockout"],
      "annotation": null
    },
    {
      "fr_id": "FR52",
      "test_ids": ["TestE2E_FullPipeline"],
      "annotation": null
    }
  ]
}
```

**Schema per entry:**
- `fr_id`: string, e.g. `"FR38"`
- `test_ids`: string array of Go `Test*` function names or web test names
- `annotation`: nullable string; if non-null, must be `"not-directly-testable, covered by X"` format

**Validator script** (`scripts/check-fr-coverage.go`):
1. Load `testdata/fr-coverage.json`
2. If `meta.grace == true`: warn (do not fail) for FRs not in coverage list
3. For each entry: verify `test_ids` resolve to real test functions (via `go test -list '.*' ./...` output)
4. Count annotated FRs; fail if > 15% of `meta.total_frs`
5. Exit 0 in grace mode (warnings only); exit 1 only for hard failures (malformed JSON, annotation count exceeded)

**Performance note:** Running `go test -list` to resolve test function names adds ~5-10s to CI. This is acceptable within the 10-minute budget.

### FR52-go E2E Test Design

**File:** `internal/pipeline/e2e_test.go`

This is a **placeholder E2E test** that validates the test structure and mocking approach. The full Phase A -> B -> C pipeline execution cannot be tested until those phases are implemented (Epic 2-3 for Phase A, Epic 5 for Phase B, Epic 9 for Phase C). The test is structured so it can be incrementally filled in as phases are built.

**Current scope (Story 1.7):**
- Test function exists with correct naming convention (`TestE2E_FullPipeline`)
- `testutil.BlockExternalHTTP(t)` is called
- Mock providers are constructed (compile-time verification of interface satisfaction)
- Temporary output directory is created
- Placeholder assertions for future artifact checks
- `t.Skip("Phase A/B/C not yet implemented — see Epic 2/5/9")` to make CI green while signaling intent

**Why t.Skip and not a stub pass:** `t.Skip` clearly communicates that this is intentionally deferred work, not a false-green test. CI reports skipped tests separately from passed tests, providing visibility.

```go
package pipeline_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/sushistack/youtube.pipeline/internal/domain"
    "github.com/sushistack/youtube.pipeline/internal/testutil"
)

// mockTextGenerator implements domain.TextGenerator for E2E testing.
type mockTextGenerator struct{}

func (m *mockTextGenerator) Generate(ctx context.Context, req domain.TextRequest) (domain.TextResponse, error) {
    return domain.TextResponse{
        Content:  `{"scenes": [{"narration": "test narration", "shots": [{"visual_descriptor": "test shot"}]}]}`,
        Model:    "mock-model",
        Provider: "mock",
    }, nil
}

// mockImageGenerator implements domain.ImageGenerator for E2E testing.
type mockImageGenerator struct{}

func (m *mockImageGenerator) Generate(ctx context.Context, req domain.ImageRequest) (domain.ImageResponse, error) {
    return domain.ImageResponse{ImagePath: "/tmp/mock.png"}, nil
}

func (m *mockImageGenerator) Edit(ctx context.Context, req domain.ImageEditRequest) (domain.ImageResponse, error) {
    return domain.ImageResponse{ImagePath: "/tmp/mock-edit.png"}, nil
}

// mockTTSSynthesizer implements domain.TTSSynthesizer for E2E testing.
type mockTTSSynthesizer struct{}

func (m *mockTTSSynthesizer) Synthesize(ctx context.Context, req domain.TTSRequest) (domain.TTSResponse, error) {
    return domain.TTSResponse{AudioPath: "/tmp/mock.wav"}, nil
}

// Compile-time interface satisfaction checks
var (
    _ domain.TextGenerator  = (*mockTextGenerator)(nil)
    _ domain.ImageGenerator = (*mockImageGenerator)(nil)
    _ domain.TTSSynthesizer = (*mockTTSSynthesizer)(nil)
)

func TestE2E_FullPipeline(t *testing.T) {
    testutil.BlockExternalHTTP(t)

    // Expected output artifacts after full pipeline execution
    outputDir := t.TempDir()
    expectedArtifacts := []string{
        "scenario.json",
        "images",
        "tts",
        "output.mp4",
        "metadata.json",
        "manifest.json",
    }

    // TODO: Wire up pipeline runner when Phase A/B/C are implemented (Epic 2, 5, 9)
    // The pipeline runner will accept injected mock providers:
    //   textGen := &mockTextGenerator{}
    //   imageGen := &mockImageGenerator{}
    //   ttsSynth := &mockTTSSynthesizer{}
    //   runner := pipeline.NewRunner(textGen, imageGen, ttsSynth, ...)
    //   err := runner.Execute(ctx, "scp-049", outputDir)

    t.Skip("Phase A/B/C pipeline runner not yet implemented — see Epic 2 (state machine), Epic 3 (Phase A agents), Epic 5 (Phase B media), Epic 9 (Phase C assembly)")

    // These assertions will be enabled when the pipeline runner exists:
    for _, artifact := range expectedArtifacts {
        path := filepath.Join(outputDir, artifact)
        if _, err := os.Stat(path); os.IsNotExist(err) {
            t.Errorf("expected artifact missing: %s", artifact)
        }
    }
}
```

**When to unskip:** When `pipeline.Runner.Execute()` exists and can process a canonical seed input end-to-end. This is expected after Epic 9 (Phase C assembly). Each epic should incrementally reduce the skip scope.

### Makefile Additions

```makefile
lint-layers:
	CGO_ENABLED=0 go run scripts/check-layer-imports.go

check-fr-coverage:
	CGO_ENABLED=0 go run scripts/check-fr-coverage.go

ci: test lint-layers check-fr-coverage build
```

The `ci` target allows developers to run the full CI-equivalent check locally before pushing.

### Deferred `-race` Flag Resolution

From `deferred-work.md` (Story 1.1): `-race` flag requires CGO but Makefile uses `CGO_ENABLED=0`. The architecture doc mentions `-race` in the `test-go` job description but this is internally inconsistent.

**Resolution in this story:** Do NOT add `-race` to CI. Document the decision in `ci.yml` with a comment. The reason: `ncruces/go-sqlite3` is a pure Go SQLite driver chosen specifically to avoid CGO. Enabling CGO solely for `-race` would negate this benefit and require `gcc` in CI. Data race detection is valuable but not critical for a single-user localhost tool. If race detection becomes important, it can be run as a separate CI job that excludes SQLite-dependent packages.

### Playwright Chromium Cache Strategy

Playwright downloads Chromium to `~/.cache/ms-playwright/`. Caching this directory avoids re-downloading ~130MB on every CI run.

```yaml
- uses: actions/cache@v4
  with:
    path: ~/.cache/ms-playwright
    key: playwright-${{ hashFiles('e2e/package-lock.json') }}
```

The cache key is based on `e2e/package-lock.json` because Playwright version changes when the lockfile changes, which requires a new browser download.

**Important:** `npx playwright install --with-deps chromium` must still run even with a cache hit, because `--with-deps` installs OS-level dependencies (libgbm, libdrm, etc.) that are not cached. Playwright CLI is smart enough to skip the browser download if the cached version matches.

### Estimated CI Timing Budget

| Job | Estimated Time | Notes |
|-----|---------------|-------|
| `test-go` | ~45-60s | Go module cache hit, ~30 test files, `-count=1` |
| `test-web` | ~20-30s | npm cache hit, Vitest ~5 tests, jsdom |
| `test-e2e` | ~60-90s | Playwright Chromium cache, 1 smoke test (currently `test.todo`) |
| `build` | ~40-60s | npm ci + vite build + go build |
| **Total (sequential)** | **~3-4 min** | test-go and test-web are parallel |

Well within the 10-minute NFR-T6 budget even at 3x estimated times.

### File Changes Summary

```
.github/workflows/
  ci.yml                          # NEW — CI pipeline (4 jobs)

scripts/
  check-layer-imports.go          # NEW — Layer-import linter (NFR-M4)
  check-layer-imports_test.go     # NEW — Linter unit tests
  check-fr-coverage.go            # NEW — FR coverage validator
  check-fr-coverage_test.go       # NEW — Validator unit tests

internal/pipeline/
  e2e_test.go                     # NEW — FR52-go E2E smoke test (t.Skip until Epic 9)

testdata/
  fr-coverage.json                # NEW — FR-to-test mapping

Makefile                          # MODIFIED — add lint-layers, check-fr-coverage, ci targets
```

### Critical Constraints

- **No testify, no gomock.** Go stdlib `testing` package only. Use `testutil.AssertEqual[T]` for assertions.
- **CGO_ENABLED=0.** All builds and tests. No `-race` flag.
- **No API keys in CI.** No `secrets:` references in `ci.yml`. `testutil.CILockout` enforces this.
- **`testutil.BlockExternalHTTP(t)`** must be called in E2E test. This is Layer 2 of the 3-layer defense.
- **`testdata/contracts/` NEVER auto-updated.** Manual review only (Structural Rule #4 from architecture).
- **`fr-coverage.json` grace mode.** Unmapped FRs warn, do not fail. Strict mode after Epic 6.
- **Module path:** `github.com/sushistack/youtube.pipeline` — use in all import validation.
- **snake_case for JSON fields** in `fr-coverage.json`.
- **Go 1.25.7** in CI — must match `go.mod`.
- **Playwright Chromium only** — no Firefox/WebKit in V1.
- **E2E test uses `t.Skip`** — the pipeline runner does not exist yet. The test must be structured to compile and provide compile-time interface checks even in skipped state.
- **`scripts/` directory** is new — must be created. Scripts are standalone `package main` Go programs (not test files).
- **Layer-import linter** must handle sub-packages. `internal/llmclient/dashscope` is under `internal/llmclient`.
- **The `test-e2e` job** depends on the application being buildable and serveable. Since `pipeline serve` is not yet implemented, configure the job so it does not hard-fail the pipeline. The existing `e2e/smoke.spec.ts` uses `test.todo()` which skips gracefully. Alternatively, mark `test-e2e` with `continue-on-error: true` until Epic 6 adds the serve command.
- **300-line domain/ cap** does not apply to `scripts/` or `internal/pipeline/e2e_test.go`.

### Previous Story Intelligence

**Story 1.4 (done):** Test infrastructure and API isolation.
- `testutil.AssertEqual[T]` — use for test assertions.
- `testutil.BlockExternalHTTP(t)` — Layer 2 defense, call in E2E test.
- `testutil.CILockout` — `init()` panics if `CI=true` + API keys present. No action needed in this story, just document that it exists.
- `testutil.LoadFixture(t, path)` — loads from `testdata/`. Already used in contract tests.
- `testutil.LoadRunStateFixture(t, name)` — creates pre-seeded SQLite DB.
- Contract test pattern: `internal/testutil/contract_test.go` already validates `testdata/contracts/pipeline_state.json`.
- `testdata/contracts/pipeline_state.json` — existing contract fixture.
- `testdata/fixtures/paused_at_batch_review.sql` — existing seed fixture.

**Story 1.3 (done):** Domain types, errors, provider interfaces.
- `domain.TextGenerator`, `domain.ImageGenerator`, `domain.TTSSynthesizer` interfaces — mock these in E2E test.
- `domain.TextRequest`, `domain.TextResponse`, `domain.ImageRequest`, `domain.ImageResponse`, `domain.TTSRequest`, `domain.TTSResponse` — used by mock implementations.
- `domain.ImageEditRequest` — `ImageGenerator` has both `Generate` and `Edit` methods.
- `domain.Run`, `domain.Stage` — established type patterns.
- Import rule: `domain/` imports nothing from `internal/`.

**Story 1.5 (done):** CLI init + doctor commands.
- `cmd/pipeline/init.go`, `cmd/pipeline/doctor.go` — existing commands to test in CI.
- `internal/config/loader.go`, `internal/config/doctor.go` — config loading and doctor checks.
- Dependencies: Viper v1.11.0, godotenv v1.5.1 — already in go.mod.

**Story 1.6 (ready-for-dev):** CLI renderer. May or may not be implemented when this story runs.
- If implemented: new files in `cmd/pipeline/render.go`, `cmd/pipeline/render_test.go`.
- Layer-import linter must handle `cmd/` importing from `internal/`.

**Story 1.1 (done):** Project scaffolding, build chain.
- Deferred: `-race` flag requires CGO. Resolved in this story by excluding `-race`.
- Makefile structure established. Add new targets following existing pattern.

**Story 1.2 (done):** SQLite database, migrations.
- `internal/db/sqlite.go`, `internal/db/migrate.go` — existing DB layer.
- `internal/db/db.go` — DB helper.
- ncruces/go-sqlite3 — pure Go, no CGO.

### Git Intelligence

Recent commits use imperative mood, brief subject lines:
- `"Add LLM provider package stubs (DashScope, DeepSeek, Gemini)"`
- `"Add BMad planning artifacts — architecture, PRD, epics, sprint plan"`
- `"Add E2E test scaffolding with Playwright"`

Commit style for this story: `Add CI pipeline with layer-import linting and E2E smoke test`.

### Project Structure After This Story

```
youtube.pipeline/
├── .github/
│   └── workflows/
│       └── ci.yml                    # NEW
├── scripts/
│   ├── check-layer-imports.go        # NEW
│   ├── check-layer-imports_test.go   # NEW
│   ├── check-fr-coverage.go          # NEW
│   └── check-fr-coverage_test.go     # NEW
├── internal/
│   └── pipeline/
│       └── e2e_test.go               # NEW
├── testdata/
│   ├── contracts/
│   │   └── pipeline_state.json       # EXISTING
│   ├── fixtures/
│   │   └── paused_at_batch_review.sql # EXISTING
│   ├── golden/
│   │   └── .gitkeep                  # EXISTING
│   └── fr-coverage.json              # NEW
├── Makefile                          # MODIFIED
└── ... (existing files unchanged)
```

### References

- Epic 1 Story 1.7 AC: [epics.md:867-899](_bmad-output/planning-artifacts/epics.md)
- CI pipeline architecture: [architecture.md:908-919](_bmad-output/planning-artifacts/architecture.md)
- NFR-T1 (CI pipeline scope): [epics.md:94](_bmad-output/planning-artifacts/epics.md)
- NFR-T3 (FR coverage): [epics.md:96](_bmad-output/planning-artifacts/epics.md)
- NFR-T6 (CI <= 10 min): [epics.md:99](_bmad-output/planning-artifacts/epics.md)
- NFR-M4 (layer-import linter): [epics.md:107](_bmad-output/planning-artifacts/epics.md)
- FR51 (test infrastructure): [epics.md:75](_bmad-output/planning-artifacts/epics.md)
- FR52 (E2E smoke tests): [epics.md:76](_bmad-output/planning-artifacts/epics.md)
- FR52-go/FR52-web split: [epics.md:633](_bmad-output/planning-artifacts/epics.md)
- Deferred `-race` issue: [deferred-work.md:5](_bmad-output/implementation-artifacts/deferred-work.md)
- Test pyramid: [architecture.md:236-239](_bmad-output/planning-artifacts/architecture.md)
- Project directory structure: [architecture.md:1501-1708](_bmad-output/planning-artifacts/architecture.md)
- Structural Rule #4 (contracts never auto-updated): [architecture.md:1791-1793](_bmad-output/planning-artifacts/architecture.md)
- Import direction rules: [architecture.md:1710-1727](_bmad-output/planning-artifacts/architecture.md)
- 3-layer API isolation: [Story 1.4 dev notes](1-4-test-infrastructure-external-api-isolation.md)
- Existing test utilities: [internal/testutil/assert.go](../../internal/testutil/assert.go), [internal/testutil/nohttp.go](../../internal/testutil/nohttp.go), [internal/testutil/ci_lockout.go](../../internal/testutil/ci_lockout.go)
- Existing contract test: [internal/testutil/contract_test.go](../../internal/testutil/contract_test.go)
- Existing Playwright config: [e2e/playwright.config.ts](../../e2e/playwright.config.ts)
- Existing smoke spec: [e2e/smoke.spec.ts](../../e2e/smoke.spec.ts)
- Sprint prompts for code review: [sprint-prompts.md:187-201](_bmad-output/planning-artifacts/sprint-prompts.md)
- Domain types: [internal/domain/types.go](../../internal/domain/types.go)
- Domain errors: [internal/domain/errors.go](../../internal/domain/errors.go)
- Domain LLM interfaces: [internal/domain/llm.go](../../internal/domain/llm.go)

## Dev Agent Record

### Agent Model Used

claude-opus-4-6

### Debug Log References

None

### Completion Notes List

- `.github/workflows/ci.yml`: 4 jobs — `test-go` and `test-web` parallel (no `needs`), then `test-e2e` and `build` sequential (`needs: [test-go, test-web]`). Go module cache via `setup-go cache: true`, npm cache via `setup-node`, Playwright Chromium cache via `actions/cache` keyed on `e2e/package-lock.json`. No `secrets:` references. `-race` exclusion documented in comment. `test-e2e` has `continue-on-error: true` until `pipeline serve` exists (Epic 6).
- Scripts restructured into subdirectories (`scripts/lintlayers/`, `scripts/frcoverage/`) because Go cannot have two `package main` files in the same directory. CI calls `go run ./scripts/lintlayers/` and `go run ./scripts/frcoverage/`.
- `scripts/lintlayers/main.go`: Layer-import linter using `go/parser` stdlib. Enforces full allowedImports matrix for all 11 internal packages. Sub-package handling (e.g. `internal/llmclient/dashscope` → `internal/llmclient`). Test files (`_test.go`) allowed to additionally import `internal/testutil` (test infrastructure). 7 unit tests.
- `scripts/frcoverage/main.go`: FR coverage validator. Loads `testdata/fr-coverage.json`, resolves test_ids via `go test -list`, checks annotated count <= 15%. Grace mode: unmapped FRs warn only. 5 unit tests.
- `testdata/fr-coverage.json`: 7 FRs mapped (FR38, FR39, FR42, FR43, FR46, FR51, FR52), 2 annotated, 41 unmapped (grace mode).
- `internal/pipeline/e2e_test.go`: `TestE2E_FullPipeline` with compile-time interface checks for all 3 mock providers. Uses `testutil.BlockExternalHTTP(t)` and `t.TempDir()`. `t.Skip` until Phase A/B/C runner exists (Epic 2/3/5/9).
- Makefile: `lint-layers`, `check-fr-coverage`, `ci` targets added.
- Fixed pre-existing bug: `cmd/pipeline/doctor.go` missing `"errors"` import (regression from Story 1.6 refactoring).
- All 100+ tests pass. Zero regressions. Layer-import lint clean. FR coverage grace mode OK.

### Change Log

- 2026-04-17: Story 1.7 implemented — CI pipeline, layer-import linter, FR coverage validator, E2E smoke test, Makefile targets

### File List

- .github/workflows/ci.yml (new)
- scripts/lintlayers/main.go (new)
- scripts/lintlayers/main_test.go (new)
- scripts/frcoverage/main.go (new)
- scripts/frcoverage/main_test.go (new)
- internal/pipeline/e2e_test.go (new)
- testdata/fr-coverage.json (new)
- Makefile (modified)
- cmd/pipeline/doctor.go (modified — fixed missing `errors` import)
