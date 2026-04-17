# Story 1.4: Test Infrastructure & External API Isolation

Status: done

## Story

As a developer,
I want test utilities and API isolation in place,
so that every subsequent story can be safely tested without hitting real APIs.

## Acceptance Criteria

1. **AC-ASSERT:** `internal/testutil/assert.go` exports `AssertEqual[T comparable]` generic helper. On mismatch: `t.Helper()` + diff output showing got vs want with file/line.

2. **AC-FIXTURE:** `internal/testutil/fixture.go` exports `LoadFixture(t, "contracts/pipeline_state.json")` returning `[]byte` from `testdata/` at project root.

3. **AC-NOHTTP:** `internal/testutil/nohttp.go` exports `BlockExternalHTTP(t)`. Any HTTP call to a non-localhost URL fails immediately with: `"external HTTP call blocked in test: {url}"`.

4. **AC-FETCH-BLOCK:** `web/src/test/setup.ts` overrides `globalThis.fetch` to reject non-localhost URLs with a clear error in all Vitest tests.

5. **AC-CI-LOCKOUT:** A Go test init function panics when `CI=true` and any of `DASHSCOPE_API_KEY`, `DEEPSEEK_API_KEY`, `GEMINI_API_KEY` env vars are set. Message: `"API keys must not be set in CI environment"`.

6. **AC-CONTRACT-FIXTURE:** `testdata/contracts/pipeline_state.json` contains a valid pipeline state contract fixture. `testdata/golden/` directory exists (empty, ready for Epic 4). `testdata/contracts/` files are NEVER auto-updated.

7. **AC-CONTRACT-TEST:** `go test ./internal/testutil/ -run Contract` loads `pipeline_state.json`, unmarshals into `domain.Run`, and validates without error.

8. **AC-SEED-FIXTURE:** `internal/testutil/fixture.go` exports `LoadRunStateFixture(t, "paused_at_batch_review")` which creates a temporary SQLite DB pre-seeded with a run at a specific stage, with segments and decisions. Fixtures are SQL seed files in `testdata/fixtures/`.

9. **AC-TEST:** Unit tests for assert pass/fail; fixture load; seed-able fixture load + query. Integration tests for nohttp blocking (attempt real URL, assert failure); CI lockout (subprocess panic verification).

## Tasks / Subtasks

- [x] **T1: `internal/testutil/assert.go` — Generic assertion helpers** (AC: #1)
  - [x] `AssertEqual[T comparable](t testing.TB, got, want T)` with `t.Helper()`, formatted diff
  - [x] `AssertJSONEq(t testing.TB, got, want string)` for JSON comparison (unmarshal + compare)
  - [x] Tests: pass case, fail case (verify t.Helper output)

- [x] **T2: `internal/testutil/fixture.go` — Fixture loading + seed-able DB** (AC: #2, #8)
  - [x] `LoadFixture(t testing.TB, path string) []byte` — reads from `testdata/{path}` at project root
  - [x] `LoadRunStateFixture(t testing.TB, name string) *sql.DB` — opens temp SQLite, runs migrations, executes SQL seed from `testdata/fixtures/{name}.sql`, returns DB with `t.Cleanup`
  - [x] Tests: fixture load round-trip; seed fixture load + query validation

- [x] **T3: `internal/testutil/nohttp.go` — External HTTP blocking** (AC: #3)
  - [x] `BlockExternalHTTP(t testing.TB)` — sets `http.DefaultTransport` to blocking transport, restores on `t.Cleanup`
  - [x] Blocking transport: allows `localhost`/`127.0.0.1`/`::1`, rejects everything else with clear error
  - [x] Tests: localhost allowed, external blocked with correct error message

- [x] **T4: `internal/testutil/ci_lockout.go` — CI environment lockout** (AC: #5)
  - [x] `init()` panics when `CI=true` + any API key env var present
  - [x] Test: subprocess verification (`go test -run TestCILockout` with env vars set, assert exit code + stderr)

- [x] **T5: `web/src/test/setup.ts` — Vitest fetch blocking** (AC: #4)
  - [x] Override `globalThis.fetch` to reject non-localhost URLs
  - [x] Allow `localhost`, `127.0.0.1`, `::1`
  - [x] Test: fetch('https://example.com') rejects; fetch('http://localhost:3000/api') resolves

- [x] **T6: `testdata/` — Contract fixtures + golden directory** (AC: #6)
  - [x] `testdata/contracts/pipeline_state.json` — valid Run object matching `domain.Run` struct
  - [x] `testdata/fixtures/paused_at_batch_review.sql` — SQL INSERT seed for run + segments + decisions at batch_review stage
  - [x] `testdata/golden/` directory exists (keep `.gitkeep`)

- [x] **T7: Contract test** (AC: #7)
  - [x] `internal/testutil/contract_test.go` — `TestContract_PipelineState` loads fixture, unmarshals into `domain.Run`, validates fields

### Review Findings

- [x] [Review][Patch] PRAGMA 반환값 미검증 — production은 WAL 모드를 QueryRow+Scan으로 확인하지만 test fixture는 Exec 결과 무시 [internal/testutil/fixture.go:36-37]
- [x] [Review][Patch] busy_timeout PRAGMA 누락 — production은 `PRAGMA busy_timeout=5000` 설정하지만 test fixture에 빠짐 [internal/testutil/fixture.go:35-37]
- [x] [Review][Patch] SQL seed fixture 타임스탬프에 Z 접미사 누락 — RFC3339 비호환 (`'2026-01-01T00:00:00'` → `'2026-01-01T00:00:00Z'`) [testdata/fixtures/paused_at_batch_review.sql]
- [x] [Review][Patch] Contract test 필드 검증 불완전 — `HumanOverride`, `RetryCount`, `CostUSD`, `TokenIn`, `TokenOut`, `DurationMs` 미검증 [internal/testutil/contract_test.go]
- [x] [Review][Defer] `BlockExternalHTTP` goroutine 비안전 — deferred, Layer 1(생성자 주입)이 병렬 테스트 커버, 현재 프로젝트에서 t.Parallel()+nohttp 미사용
- [x] [Review][Defer] `Migrate` user_version PRAGMA가 트랜잭션 외부 — deferred, Story 1.2 기존 이슈, 단일 사용자 도구에서 크래시 윈도우 허용 가능

## Dev Notes

### Architecture: Three-Layer API Isolation

The architecture defines a three-layer defense ensuring no real API calls in CI:

1. **Layer 1 — HTTP client injection:** All external API clients accept `*http.Client` via constructor (established in Story 1.3 interfaces). No `http.DefaultClient` usage.
2. **Layer 2 — Runtime blocking:** `testutil/nohttp.go` replaces `http.DefaultTransport` with a blocking transport. `web/src/test/setup.ts` blocks `globalThis.fetch`.
3. **Layer 3 — Environment lockout:** CI workflow never injects API keys. Go test `init()` panics if `CI=true` + API key present.

### `AssertEqual[T]` Implementation

Architecture specifies: "Custom `assertEqual[T]` generic helper (5 lines) replaces assert.Equal" (architecture.md line 266). Export as `AssertEqual` for cross-package use.

```go
func AssertEqual[T comparable](t testing.TB, got, want T) {
    t.Helper()
    if got != want {
        t.Errorf("got %v, want %v", got, want)
    }
}
```

Also provide `AssertJSONEq` for JSON comparison — unmarshal both strings into `any`, use `reflect.DeepEqual`. Architecture references this at line 1380.

### `LoadFixture` Implementation

Resolves path relative to project root's `testdata/` directory. Use `os.Getwd()` walking upward to find `go.mod` as project root anchor (same pattern as `migrations` embed — reliable in all `go test` invocations).

```go
func LoadFixture(t testing.TB, path string) []byte {
    t.Helper()
    root := findProjectRoot(t)
    data, err := os.ReadFile(filepath.Join(root, "testdata", path))
    if err != nil {
        t.Fatalf("load fixture %s: %v", path, err)
    }
    return data
}
```

Alternative (simpler): use `runtime.Caller` to get source file path and navigate from there. Pick whichever is more robust across `go test ./...` invocations.

### `LoadRunStateFixture` Implementation

Creates a temporary SQLite DB, runs schema migrations via `db.Migrate()`, then executes a SQL seed file.

```go
func LoadRunStateFixture(t testing.TB, name string) *sql.DB {
    t.Helper()
    tmp := filepath.Join(t.TempDir(), "test.db")
    testDB, err := sql.Open("sqlite3", tmp)
    if err != nil {
        t.Fatalf("open test db: %v", err)
    }
    // WAL mode + foreign keys (match production OpenDB)
    testDB.Exec("PRAGMA journal_mode=wal")
    testDB.Exec("PRAGMA foreign_keys=ON")
    testDB.SetMaxOpenConns(1)

    if err := db.Migrate(testDB); err != nil {
        t.Fatalf("migrate test db: %v", err)
    }
    seed := LoadFixture(t, filepath.Join("fixtures", name+".sql"))
    if _, err := testDB.Exec(string(seed)); err != nil {
        t.Fatalf("seed fixture %s: %v", name, err)
    }
    t.Cleanup(func() { testDB.Close() })
    return testDB
}
```

**Import note:** `testutil` will import `internal/db` for `db.Migrate()`. This is acceptable — `testutil` is a test-only package, not a domain package. The import restriction is on `domain/` and `clock/`, not on `testutil/`.

### `BlockExternalHTTP` Implementation

```go
func BlockExternalHTTP(t testing.TB) {
    t.Helper()
    original := http.DefaultTransport
    http.DefaultTransport = &blockingTransport{}
    t.Cleanup(func() { http.DefaultTransport = original })
}

type blockingTransport struct{}

func (b *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    host := req.URL.Hostname()
    if host == "localhost" || host == "127.0.0.1" || host == "::1" {
        return http.DefaultTransport.RoundTrip(req) // BUG: recursion!
    }
    return nil, fmt.Errorf("external HTTP call blocked in test: %s", req.URL.String())
}
```

**Critical:** Store the original transport before replacement. The `RoundTrip` for localhost must delegate to the *original* transport (stored in a field), not `http.DefaultTransport` (which would recurse). Fix:

```go
type blockingTransport struct {
    original http.RoundTripper
}

func (b *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    host := req.URL.Hostname()
    if host == "localhost" || host == "127.0.0.1" || host == "::1" {
        return b.original.RoundTrip(req)
    }
    return nil, fmt.Errorf("external HTTP call blocked in test: %s", req.URL.String())
}
```

### CI Lockout Implementation

Place in `internal/testutil/ci_lockout.go`. The `init()` function runs when any test imports `testutil`.

```go
func init() {
    if os.Getenv("CI") != "true" {
        return
    }
    keys := []string{"DASHSCOPE_API_KEY", "DEEPSEEK_API_KEY", "GEMINI_API_KEY"}
    for _, k := range keys {
        if os.Getenv(k) != "" {
            panic("API keys must not be set in CI environment")
        }
    }
}
```

**Testing the init panic:** Use `exec.Command` to run a subprocess with the env vars set:

```go
func TestCILockout(t *testing.T) {
    if os.Getenv("CI_LOCKOUT_SUBPROCESS") == "1" {
        // This will trigger the init() panic — but init already ran.
        // So test the lockout function directly instead.
        return
    }
    cmd := exec.Command(os.Args[0], "-test.run=TestCILockout")
    cmd.Env = append(os.Environ(), "CI=true", "DASHSCOPE_API_KEY=fake", "CI_LOCKOUT_SUBPROCESS=1")
    output, err := cmd.CombinedOutput()
    if err == nil {
        t.Fatal("expected subprocess to fail")
    }
    if !strings.Contains(string(output), "API keys must not be set in CI environment") {
        t.Errorf("expected panic message, got: %s", output)
    }
}
```

**Subtlety:** `init()` runs once per process at import time. The subprocess approach is the correct way to test init panics — you cannot re-trigger init in the same process.

### Vitest Fetch Blocking (`web/src/test/setup.ts`)

Replace the current stub content with:

```typescript
const originalFetch = globalThis.fetch;

globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
  const parsed = new URL(url, 'http://localhost');
  const host = parsed.hostname;

  if (host === 'localhost' || host === '127.0.0.1' || host === '::1') {
    return originalFetch(input, init);
  }

  throw new Error(`external fetch blocked in test: ${url}`);
};
```

### Contract Fixture: `testdata/contracts/pipeline_state.json`

Must match `domain.Run` struct exactly (snake_case keys, correct types). This is the SSoT — both Go and future Zod schemas validate against it.

```json
{
  "id": "scp-049-run-1",
  "scp_id": "049",
  "stage": "pending",
  "status": "pending",
  "retry_count": 0,
  "cost_usd": 0.0,
  "token_in": 0,
  "token_out": 0,
  "duration_ms": 0,
  "human_override": false,
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

Nullable fields (`retry_reason`, `critic_score`, `scenario_path`) omitted — Go `omitempty` tags handle this correctly for JSON unmarshal (nil pointer).

### Seed Fixture: `testdata/fixtures/paused_at_batch_review.sql`

Pre-seeds a run at `batch_review` stage with segments and decisions:

```sql
INSERT INTO runs (id, scp_id, stage, status, retry_count, cost_usd, token_in, token_out, duration_ms, human_override, created_at, updated_at)
VALUES ('scp-049-run-1', '049', 'batch_review', 'waiting', 0, 1.25, 15000, 3000, 45000, 0, '2026-01-01T00:00:00', '2026-01-01T00:30:00');

INSERT INTO segments (run_id, scene_index, narration, shot_count, status)
VALUES ('scp-049-run-1', 0, 'SCP-049 접근 장면', 2, 'completed'),
       ('scp-049-run-1', 1, 'SCP-049 실험 기록', 1, 'completed'),
       ('scp-049-run-1', 2, 'SCP-049 격리 절차', 1, 'pending');

INSERT INTO decisions (run_id, scene_id, decision_type, created_at)
VALUES ('scp-049-run-1', '0', 'approve', '2026-01-01T00:20:00'),
       ('scp-049-run-1', '1', 'approve', '2026-01-01T00:25:00');
```

### File Organization After This Story

```
internal/testutil/
  doc.go               # UNCHANGED (from Story 1.1)
  assert.go            # NEW — AssertEqual[T], AssertJSONEq
  fixture.go           # NEW — LoadFixture, LoadRunStateFixture
  nohttp.go            # NEW — BlockExternalHTTP
  ci_lockout.go        # NEW — init() CI environment lockout
  assert_test.go       # NEW
  fixture_test.go      # NEW
  nohttp_test.go       # NEW
  ci_lockout_test.go   # NEW
  contract_test.go     # NEW — pipeline_state.json → domain.Run

web/src/test/
  setup.ts             # MODIFIED — fetch blocking implementation

testdata/
  contracts/
    pipeline_state.json  # NEW — Run contract fixture (SSoT)
    .gitkeep             # KEEP
  fixtures/
    paused_at_batch_review.sql  # NEW — seed-able DB fixture
  golden/
    .gitkeep             # KEEP (empty, ready for Epic 4)
```

### Critical Constraints

- **No testify, no gomock.** Go stdlib `testing` only (consistent with Stories 1.1–1.3).
- **CGO_ENABLED=0.** All Go builds and tests work without CGO.
- **`testutil/` can import `internal/db`** (for Migrate) and `internal/domain` (for contract test types). `testutil/` is test infrastructure, not domain code.
- **`testdata/contracts/` never auto-updated.** Manual review only (architecture structural rule #4).
- **`testdata/golden/` may be auto-updated** via `-update` test flag (future Epic 4).
- **Error message exact match:** `"external HTTP call blocked in test: {url}"` — this is the canonical format.
- **Panic message exact match:** `"API keys must not be set in CI environment"`.
- **`doc.go` exists** in `internal/testutil/` from Story 1.1. Do NOT delete or overwrite.
- **Naming convention:** Exported functions use PascalCase (`AssertEqual`, `LoadFixture`, `BlockExternalHTTP`). Architecture line 1379 shows lowercase `assertEqual` — but that's the intra-package form. Since `testutil` is consumed by other packages, export with PascalCase.
- **300-line file cap applies to `domain/`**, not `testutil/`. No hard cap on testutil files, but keep each file focused on a single concern.

### Previous Story Intelligence

**Story 1.3 (done):** Implemented domain types, error system, provider interfaces, clock abstraction.
- `domain.Run` struct: 15 fields with snake_case JSON tags, pointer types for nullable. Contract fixture must match this exactly.
- `domain.Stage` constants: 15 values. Seed fixture uses `batch_review` (valid stage).
- Error wrapping pattern: `fmt.Errorf("context: %w", err)` — follow this in testutil.
- Go stdlib testing only — no testify. Pattern: `t.Run` subtests, `t.Helper()`, `t.Cleanup()`.
- `FakeClock` uses `t.Cleanup` — follow same cleanup pattern in testutil.

**Story 1.2 (done):** SQLite DB with `db.Migrate()`, `db.OpenDB()`.
- `db.Migrate(db)` applies all pending migrations — use in `LoadRunStateFixture`.
- Test DB pattern: `t.TempDir()` for temp DB, `t.Cleanup(func() { db.Close() })`.
- `PRAGMA foreign_keys=ON` + `db.SetMaxOpenConns(1)` — replicate in test DB setup.
- `PRAGMA journal_mode=wal` — replicate for consistency.
- Driver import: `_ "github.com/ncruces/go-sqlite3/driver"` — testutil needs this import since it opens DBs.

### Git Intelligence

Go module: `github.com/sushistack/youtube.pipeline`, Go 1.25.7. Dependencies: Cobra v1.10.2, ncruces/go-sqlite3 v0.33.3. Commit style: imperative mood, brief subject line.

### Project Structure Notes

- `internal/testutil/doc.go` already exists as package stub from Story 1.1 — new files go alongside
- `testdata/contracts/` and `testdata/golden/` directories exist with `.gitkeep` — add files alongside
- `testdata/fixtures/` directory does NOT exist yet — must be created
- `web/src/test/setup.ts` exists with stub comment — will be replaced with fetch blocking implementation
- Vitest config (`web/vitest.config.ts`) already references `./src/test/setup.ts` as setupFile

### References

- Epic 1 Story 1.4 AC: [epics.md:745-792](../_bmad-output/planning-artifacts/epics.md)
- Three-layer API isolation: [architecture.md:401-421](../_bmad-output/planning-artifacts/architecture.md)
- testutil package spec (6 helpers): [architecture.md:1373-1404](../_bmad-output/planning-artifacts/architecture.md)
- Project structure (testutil + testdata): [architecture.md:345-354](../_bmad-output/planning-artifacts/architecture.md)
- Full project tree: [architecture.md:1617-1707](../_bmad-output/planning-artifacts/architecture.md)
- Go testing stdlib decision: [architecture.md:266](../_bmad-output/planning-artifacts/architecture.md)
- Contract test SSoT pattern: [architecture.md:1453-1465](../_bmad-output/planning-artifacts/architecture.md)
- Structural rule #4 (contracts never auto-updated): [architecture.md:1791-1793](../_bmad-output/planning-artifacts/architecture.md)
- FR51 test infrastructure: [epics.md:75](../_bmad-output/planning-artifacts/epics.md)
- Story 1.3 domain types: [1-3-domain-types-error-system-architecture-interfaces.md](1-3-domain-types-error-system-architecture-interfaces.md)
- SQLite DDL: [migrations/001_init.sql](../../migrations/001_init.sql)
- DB migration runner: [internal/db/migrate.go](../../internal/db/migrate.go)
- Existing testutil doc.go: [internal/testutil/doc.go](../../internal/testutil/doc.go)
- Existing setup.ts: [web/src/test/setup.ts](../../web/src/test/setup.ts)

## Dev Agent Record

### Agent Model Used

claude-opus-4-6

### Debug Log References

None

### Completion Notes List

- `assert.go`: `AssertEqual[T comparable]` generic helper with `t.Helper()` + diff output; `AssertJSONEq` for JSON semantic comparison via `reflect.DeepEqual`
- `fixture.go`: `LoadFixture` resolves `testdata/` via `go.mod` root-walking; `LoadRunStateFixture` creates temp SQLite DB with WAL + foreign keys + migrations + SQL seed, `t.Cleanup` for DB close
- `nohttp.go`: `BlockExternalHTTP` replaces `http.DefaultTransport` with blocking transport (stores original to avoid recursion), allows localhost/127.0.0.1/::1, restores via `t.Cleanup`
- `ci_lockout.go`: `init()` panics when `CI=true` and any of 3 API key env vars set; tested via subprocess re-invocation
- `setup.ts`: Overrides `globalThis.fetch` to reject non-localhost URLs with descriptive error
- `pipeline_state.json`: Contract fixture matching `domain.Run` struct (snake_case, omitempty nullable fields)
- `paused_at_batch_review.sql`: Seed fixture with run at batch_review stage, 3 segments, 2 decisions
- `contract_test.go`: Loads fixture, unmarshals into `domain.Run`, validates all fields + nullable nil + round-trip
- 20 Go tests (6 assert + 5 fixture + 3 nohttp + 2 ci_lockout + 1 contract + 3 load/query), 4 Vitest tests — all passing, zero regressions

### Change Log

- 2026-04-17: Story 1.4 implemented — test utilities (assert, fixture, nohttp, CI lockout), Vitest fetch blocking, contract fixtures, seed-able SQLite fixtures, 24 total tests passing

### File List

- internal/testutil/assert.go (new)
- internal/testutil/assert_test.go (new)
- internal/testutil/fixture.go (new)
- internal/testutil/fixture_test.go (new)
- internal/testutil/nohttp.go (new)
- internal/testutil/nohttp_test.go (new)
- internal/testutil/ci_lockout.go (new)
- internal/testutil/ci_lockout_test.go (new)
- internal/testutil/contract_test.go (new)
- web/src/test/setup.ts (modified)
- web/src/test/setup.test.ts (new)
- testdata/contracts/pipeline_state.json (new)
- testdata/fixtures/paused_at_batch_review.sql (new)
