# Deferred Work

## Deferred from: code review of 1-1-go-react-spa-project-scaffolding-build-chain (2026-04-16)

- `-race` flag requires CGO but Makefile uses `CGO_ENABLED=0` — architecture doc internally inconsistent. Resolve when adding CI pipeline (Story 1.7): either split `test-race` target or enable CGO for test target only.
- AC-GITIGNORE spec text says `web/dist/` but actual build output goes to `internal/web/dist/` — update story AC text to match chosen Vite outDir architecture.

## Deferred from: code review of 1-2-sqlite-database-migration-infrastructure (2026-04-16)

- WAL sidecar files (-wal, -shm) inherit default umask permissions instead of 0600. Spec says "DB file" (singular); fixing requires process-level umask or post-hoc chmod of lazily-created files. Localhost-only tool mitigates risk.
- `runs.updated_at` column has DEFAULT but no AFTER UPDATE trigger. Column will be stale after any UPDATE. Add trigger in a future migration when UPDATE operations are implemented (Epic 2).
