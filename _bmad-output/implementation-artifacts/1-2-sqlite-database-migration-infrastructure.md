# Story 1.2: SQLite Database & Migration Infrastructure

Status: done

## Story

As an operator,
I want the database schema initialized automatically on first run,
so that pipeline state tracking works from the start.

## Acceptance Criteria

1. **AC-OPEN:** `internal/db/sqlite.go` opens a new SQLite connection with WAL mode enforced (`PRAGMA journal_mode=wal`), `busy_timeout=5000`, and the DB file created with `0600` permissions on POSIX systems.

2. **AC-MIGRATE:** `internal/db/migrate.go` (~50 LoC) implements a migration runner that reads `.sql` files from `migrations.FS` (`embed.FS`), applies them sequentially, and tracks applied version via `PRAGMA user_version`. No external migration tools (goose, golang-migrate).

3. **AC-SCHEMA:** `migrations/001_init.sql` creates 3 tables (`runs`, `decisions`, `segments`) with column types and defaults exactly matching the Architecture DDL.

4. **AC-UNIQUE:** `segments` table has `UNIQUE(run_id, scene_index)` constraint.

5. **AC-IDEMPOTENT:** If migration 001 has already been applied (`PRAGMA user_version` >= 1), re-running the migration runner causes no error and `user_version` remains unchanged.

6. **AC-VERSION:** After migration 001 completes, `PRAGMA user_version` equals 1.

## Tasks / Subtasks

- [x] **T1: `migrations/001_init.sql` — Full DDL** (AC: #3, #4)
  - [x] Replace placeholder content with exact Architecture DDL (3 tables: `runs`, `decisions`, `segments`)
  - [x] Verify every column name, type, default, FK, and the `UNIQUE(run_id, scene_index)` constraint match architecture.md verbatim

- [x] **T2: `internal/db/sqlite.go` — OpenDB function** (AC: #1)
  - [x] Create `OpenDB(path string) (*sql.DB, error)` function
  - [x] Use `sql.Open("sqlite3", path)` with ncruces driver (already imported via blank import in `db.go`)
  - [x] Execute `PRAGMA journal_mode=wal` and verify the response is `"wal"` (not silently ignored)
  - [x] Execute `PRAGMA busy_timeout=5000`
  - [x] Set DB file permissions to `0600` using `os.Chmod` after open (POSIX only)
  - [x] Call `Migrate(db)` before returning

- [x] **T3: `internal/db/migrate.go` — Migration Runner** (AC: #2, #5, #6)
  - [x] Create `Migrate(db *sql.DB) error` function (~50 LoC)
  - [x] Read current version: `PRAGMA user_version`
  - [x] List `.sql` files from `migrations.FS` sorted by filename (numeric prefix order)
  - [x] Parse version number from filename (`001` → version 1)
  - [x] Skip files where parsed version <= current `user_version` (idempotency)
  - [x] For each unapplied migration: read SQL, `db.Exec(sql)`, then set `PRAGMA user_version = N`
  - [x] Wrap each migration in a transaction for atomicity

- [x] **T4: Update `internal/db/db.go`** (AC: #1, #2)
  - [x] Remove the stub `Migrations` variable (no longer needed — `migrate.go` imports `migrations.FS` directly)
  - [x] Keep `package db` and the ncruces driver blank import

- [x] **T5: `internal/db/sqlite_test.go` — Tests** (AC: #1–#6)
  - [x] WAL mode assertion: open DB → query `PRAGMA journal_mode` → assert `"wal"`
  - [x] busy_timeout assertion: open DB → query `PRAGMA busy_timeout` → assert `5000`
  - [x] File permission check: open DB → `os.Stat` → assert mode `0600` (skip on Windows)
  - [x] Migration idempotency: run `Migrate` twice → no error, `PRAGMA user_version` still `1`
  - [x] Schema verification: open DB → assert tables `runs`, `decisions`, `segments` exist
  - [x] Column verification: query `PRAGMA table_info(runs)` etc. → assert column names/types match DDL
  - [x] UNIQUE constraint verification: insert two rows with same `(run_id, scene_index)` → assert error

### Review Findings

- [x] [Review][Decision] Crash recovery gap: COMMIT→PRAGMA user_version window — 수용 결정. 극히 좁은 crash window, localhost 단일 사용자 도구, 수동 복구 가능
- [x] [Review][Patch] `PRAGMA foreign_keys=ON` 추가 완료 [internal/db/sqlite.go]
- [x] [Review][Patch] `db.SetMaxOpenConns(1)` 추가 완료 [internal/db/sqlite.go]
- [x] [Review][Defer] WAL sidecar 파일(-wal, -shm) 0600 미적용 [internal/db/sqlite.go:32] — deferred, 스펙은 "DB file" 단수, sidecar는 SQLite 엔진이 생성하며 umask 설정 필요
- [x] [Review][Defer] `updated_at` 자동 갱신 트리거 없음 [migrations/001_init.sql:17] — deferred, UPDATE 로직 구현 시(Epic 2) 트리거 마이그레이션 추가

## Dev Notes

### Architecture DDL — Copy Verbatim

The exact DDL from [architecture.md](../_bmad-output/planning-artifacts/architecture.md) section "Schema (V1, 3 tables)":

```sql
-- migrations/001_init.sql

CREATE TABLE runs (
    id           TEXT PRIMARY KEY,
    scp_id       TEXT NOT NULL,
    stage        TEXT NOT NULL DEFAULT 'pending',
    status       TEXT NOT NULL DEFAULT 'pending',
    retry_count  INTEGER NOT NULL DEFAULT 0,
    retry_reason TEXT,
    critic_score REAL,
    cost_usd     REAL NOT NULL DEFAULT 0.0,
    token_in     INTEGER NOT NULL DEFAULT 0,
    token_out    INTEGER NOT NULL DEFAULT 0,
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    human_override INTEGER NOT NULL DEFAULT 0,
    scenario_path TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE decisions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id          TEXT NOT NULL REFERENCES runs(id),
    scene_id        TEXT,
    decision_type   TEXT NOT NULL,
    context_snapshot TEXT,
    outcome_link    TEXT,
    tags            TEXT,
    feedback_source TEXT,
    external_ref    TEXT,
    feedback_at     TEXT,
    superseded_by   INTEGER REFERENCES decisions(id),
    note            TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE segments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id          TEXT NOT NULL REFERENCES runs(id),
    scene_index     INTEGER NOT NULL,
    narration       TEXT,
    shot_count      INTEGER NOT NULL DEFAULT 1,
    shots           TEXT,
    tts_path        TEXT,
    tts_duration_ms INTEGER,
    clip_path       TEXT,
    critic_score    REAL,
    critic_sub      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(run_id, scene_index)
);
```

Do NOT add, remove, or rename any column. Do NOT add comments inside the SQL file beyond the migration header comment.

### ncruces/go-sqlite3 Driver Details

- Driver name for `sql.Open` is `"sqlite3"` (registered by the blank import `_ "github.com/ncruces/go-sqlite3/driver"` in `db.go`)
- Pure Go, no CGO — `CGO_ENABLED=0` safe
- WAL mode supported; set via `PRAGMA` after open (not DSN parameter)
- Already in `go.mod` as `v0.33.3`

### OpenDB Function Pattern

```go
// internal/db/sqlite.go
package db

import (
    "database/sql"
    "fmt"
    "os"

    _ "github.com/ncruces/go-sqlite3/driver"
)

func OpenDB(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite3", path)
    if err != nil {
        return nil, fmt.Errorf("open db: %w", err)
    }

    // WAL mode — must verify response
    var mode string
    if err := db.QueryRow("PRAGMA journal_mode=wal").Scan(&mode); err != nil {
        db.Close()
        return nil, fmt.Errorf("set WAL mode: %w", err)
    }
    if mode != "wal" {
        db.Close()
        return nil, fmt.Errorf("WAL mode not enabled, got: %s", mode)
    }

    // busy_timeout
    if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
        db.Close()
        return nil, fmt.Errorf("set busy_timeout: %w", err)
    }

    // File permissions (POSIX)
    if err := os.Chmod(path, 0600); err != nil {
        db.Close()
        return nil, fmt.Errorf("set file permissions: %w", err)
    }

    // Run migrations
    if err := Migrate(db); err != nil {
        db.Close()
        return nil, fmt.Errorf("migrate: %w", err)
    }

    return db, nil
}
```

### Migrate Function Pattern

```go
// internal/db/migrate.go
package db

import (
    "database/sql"
    "fmt"
    "io/fs"
    "sort"
    "strconv"
    "strings"

    "github.com/sushistack/youtube.pipeline/migrations"
)

func Migrate(db *sql.DB) error {
    var current int
    if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
        return fmt.Errorf("read user_version: %w", err)
    }

    entries, err := fs.ReadDir(migrations.FS, ".")
    if err != nil {
        return fmt.Errorf("read migrations: %w", err)
    }

    // Sort by filename (lexicographic = numeric order for NNN_ prefix)
    sort.Slice(entries, func(i, j int) bool {
        return entries[i].Name() < entries[j].Name()
    })

    for _, e := range entries {
        if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
            continue
        }
        ver, err := parseMigrationVersion(e.Name())
        if err != nil {
            return fmt.Errorf("parse migration %s: %w", e.Name(), err)
        }
        if ver <= current {
            continue // already applied
        }

        data, err := fs.ReadFile(migrations.FS, e.Name())
        if err != nil {
            return fmt.Errorf("read %s: %w", e.Name(), err)
        }

        tx, err := db.Begin()
        if err != nil {
            return fmt.Errorf("begin tx for %s: %w", e.Name(), err)
        }
        if _, err := tx.Exec(string(data)); err != nil {
            tx.Rollback()
            return fmt.Errorf("exec %s: %w", e.Name(), err)
        }
        // PRAGMA user_version cannot run inside a transaction in SQLite
        if err := tx.Commit(); err != nil {
            return fmt.Errorf("commit %s: %w", e.Name(), err)
        }
        if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", ver)); err != nil {
            return fmt.Errorf("set user_version to %d: %w", ver, err)
        }
    }
    return nil
}

func parseMigrationVersion(filename string) (int, error) {
    // Extract leading digits: "001_init.sql" → "001" → 1
    parts := strings.SplitN(filename, "_", 2)
    if len(parts) == 0 {
        return 0, fmt.Errorf("invalid migration filename: %s", filename)
    }
    return strconv.Atoi(parts[0])
}
```

**PRAGMA user_version and transactions:** `PRAGMA user_version = N` cannot be executed inside a transaction in SQLite — it is silently ignored. Always execute it after `tx.Commit()`.

### db.go Cleanup

Story 1.1 created `internal/db/db.go` with a stub `Migrations` variable. This story replaces it with `sqlite.go` and `migrate.go`. Update `db.go` to keep only the package declaration and driver import:

```go
// Package db provides SQLite database connection and migration utilities.
package db

import _ "github.com/ncruces/go-sqlite3/driver"
```

The `migrations` package import and `Migrations` variable are no longer needed — `migrate.go` imports `migrations.FS` directly.

### Test Pattern

Use `t.TempDir()` for test DB paths — automatically cleaned up. Test with real SQLite files, not `:memory:`, to verify WAL behavior and file permissions.

```go
func TestOpenDB_WALMode(t *testing.T) {
    path := filepath.Join(t.TempDir(), "test.db")
    db, err := OpenDB(path)
    if err != nil {
        t.Fatalf("OpenDB: %v", err)
    }
    defer db.Close()

    var mode string
    db.QueryRow("PRAGMA journal_mode").Scan(&mode)
    if mode != "wal" {
        t.Errorf("journal_mode = %q, want wal", mode)
    }
}
```

### File Layout After This Story

```
internal/db/
  db.go           # package declaration + driver import only
  sqlite.go       # OpenDB function
  migrate.go      # Migrate function + parseMigrationVersion
  sqlite_test.go  # all tests for this story
migrations/
  embed.go        # unchanged from Story 1.1
  001_init.sql    # full DDL (replaces placeholder)
```

### Critical Constraints

- **No external migration tools:** No goose, golang-migrate, etc. Pure Go, ~50 LoC.
- **No `:memory:` in tests:** Use real file-based SQLite for WAL and permission testing.
- **PRAGMA user_version outside transactions:** SQLite silently ignores `PRAGMA user_version = N` inside a transaction. Always set it after commit.
- **CGO_ENABLED=0:** All Go builds and tests must work with `CGO_ENABLED=0`. ncruces/go-sqlite3 is pure Go.
- **migrations/ directory is at project root:** Do not move SQL files into `internal/db/`. Go `embed.FS` cannot reference `../` paths. `migrations/embed.go` already handles this.
- **No stdlib testify:** Use Go built-in `testing` package only. `t.Fatalf`, `t.Errorf`, direct comparisons.

### Project Structure Notes

- `internal/db/db.go` exists from Story 1.1 — modify, do not recreate
- `migrations/embed.go` exists from Story 1.1 — do not modify
- `migrations/001_init.sql` exists from Story 1.1 as placeholder — replace content entirely
- New files: `internal/db/sqlite.go`, `internal/db/migrate.go`, `internal/db/sqlite_test.go`

### References

- Architecture DDL: [architecture.md — Schema (V1, 3 tables)](../_bmad-output/planning-artifacts/architecture.md) lines 472–539
- Migration strategy: [architecture.md — Migration Strategy: Manual embedded SQL + PRAGMA user_version](../_bmad-output/planning-artifacts/architecture.md) lines 463–470
- ncruces rationale: [architecture.md — Technology Stack](../_bmad-output/planning-artifacts/architecture.md) line 257
- Test DB helper pattern: [architecture.md — testutil.NewTestDB](../_bmad-output/planning-artifacts/architecture.md) lines 1391–1397
- Implementation sequence: [architecture.md — Decision Impact Analysis](../_bmad-output/planning-artifacts/architecture.md) line 934 (step 1: "SQLite schema + migration runner + DB open")
- Database naming conventions: [architecture.md — Database Naming](../_bmad-output/planning-artifacts/architecture.md) lines 967–977
- Story 1.1 embed constraint: [1-1 story — migrations/ embed constraint](1-1-go-react-spa-project-scaffolding-build-chain.md) lines 111–112
- Epic 1 Story 1.2 AC: [epics.md — Story 1.2](../_bmad-output/planning-artifacts/epics.md) lines 684–709

## Dev Agent Record

### Agent Model Used

claude-opus-4-6

### Debug Log References

None

### Completion Notes List

- `migrations/001_init.sql`: Architecture DDL verbatim — 3 tables (runs 15 cols, decisions 13 cols, segments 13 cols + UNIQUE constraint)
- `internal/db/sqlite.go`: `OpenDB(path)` — WAL mode with response verification, busy_timeout=5000, os.Chmod 0600, auto-migrate on open
- `internal/db/migrate.go`: ~70 LoC migration runner — embed.FS read, PRAGMA user_version tracking, per-migration transactions, PRAGMA user_version set outside tx
- `internal/db/db.go`: cleaned stub — package declaration + ncruces driver blank import only
- 10 tests all passing: WAL mode, busy_timeout, file permissions, idempotency, user_version, 3 table existence, 3 column verifications (runs/decisions/segments), UNIQUE constraint violation
- No external dependencies added; ncruces/go-sqlite3 already in go.mod from Story 1.1
- CGO_ENABLED=0 build verified

### Change Log

- 2026-04-16: Story 1.2 implemented — SQLite DB open, migration runner, full schema DDL, 10 tests passing

### File List

- migrations/001_init.sql (modified — placeholder replaced with full DDL)
- internal/db/db.go (modified — removed stub Migrations variable)
- internal/db/sqlite.go (new — OpenDB function)
- internal/db/migrate.go (new — Migrate function + parseMigrationVersion)
- internal/db/sqlite_test.go (new — 10 tests)
