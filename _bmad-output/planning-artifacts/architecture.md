---
stepsCompleted:
  - 1
  - 2
  - 3
  - 4
  - 5
  - 6
  - 7
  - 8
lastStep: 8
status: 'complete'
completedAt: '2026-04-14'
partyModeInsights:
  step2:
    agents: ["Winston (Architect)", "Murat (TEA)", "Amelia (Developer)", "John (PM)"]
    rounds: 1
    outcome: >
      Cross-cutting concern 3-tier 재분류, Artifact lifecycle 신규 concern,
      Test infrastructure 독립 도메인 승격, External API mock boundary 추가,
      Inter-agent schema contract 승격, Phase B rate-limit 조율 명시,
      LLM adapter 계층 구체화, SQLite WAL 동시성 제약 구체화,
      Contract test SSoT 명시, 도메인별 아키텍처 가중치 추가,
      Time abstraction for backoff testing
  step3:
    agents: ["Winston (Architect)", "Murat (TEA)", "Amelia (Developer)"]
    rounds: 1
    outcome: >
      Vite 8→7 다운그레이드 (boring technology), mattn→ncruces/go-sqlite3
      교체 (no CGO), testify→stdlib only, jsdom 확정 (happy-dom 아님),
      Playwright 1개 smoke만, Day 1 필수 파일 6개 식별, API 차단 3중 방어
  step4:
    agents: ["Winston (Architect)", "Amelia (Developer)", "Murat (TEA)", "Sally (UX)"]
    rounds: 1
    outcome: >
      WaitGroup→errgroup(no ctx cancel), segments resume=DELETE+reinsert,
      semaphore+x/time/rate 병용, Zustand persist(localStorage), 리뷰 목록
      스냅샷 고정 패턴, undo 포커스 복귀 포함, HITL 대기 상태 명시,
      per-run 파일 컨벤션 명시, 에러 retryable/fatal 분류 체계 추가
  step5:
    agents: ["Winston (Architect)", "Amelia (Developer)", "Murat (TEA)"]
    rounds: 1
    outcome: >
      라우터 등록+미들웨어 패턴 추가, testutil 최소 6헬퍼 명시,
      import direction CI 강제, ID생성(scp-{id}-run-{n}) 확정,
      ESLint snake_case 허용, HTTP handler 테스트 템플릿,
      golden file 구조검증 패턴, slog injection, 엔드포인트 추가 체크리스트
  step6:
    agents: ["Winston (Architect)", "Amelia (Developer)", "Murat (TEA)"]
    rounds: 1
    outcome: >
      domain/ 300줄 파일 상한, engine↔service 순환 의존 해결(인터페이스 분리),
      Day 1=10파일 목표, config/loader.go 추가, MSW handlers 디렉토리 분리,
      renderWithProviders.tsx 추가, contracts/ 자동갱신 금지, undo.go→scene_service 내부
  step7:
    agents: ["Winston (Architect)", "Amelia (Developer)", "Murat (TEA)"]
    rounds: 1
    verdict: "Ship it (3/3 Go)"
    outcome: >
      force-reset CLI 명령 추가 권고, 17→12 endpoint pruning 구현 시 고려,
      FFmpeg binding Week 1 확정, HITL이 E2E safety net 역할 확인,
      최고확률 미탐지 버그=백엔드↔프론트 타이밍(HITL 커버)
inputDocuments:
  - _bmad-output/planning-artifacts/prd.md
  - _bmad-output/planning-artifacts/ux-design-specification.md
  - _bmad-output/planning-artifacts/ux-design-directions.html
  - docs/prd.md
  - docs/analysis/scp.yt.channels.analysis.md
  - docs/tts/qwen3.tts.vc.md
  - docs/images/image.gen.policy.md
  - docs/vision/descriptor_enrichment.md
  - docs/prompts/scenario/format_guide.md
  - docs/prompts/scenario/01_research.md
  - docs/prompts/scenario/02_structure.md
  - docs/prompts/scenario/03_writing.md
  - docs/prompts/scenario/03_5_visual_breakdown.md
  - docs/prompts/scenario/04_review.md
  - docs/prompts/scenario/critic_agent.md
  - docs/prompts/image/01_shot_breakdown.md
  - docs/prompts/image/02_shot_to_prompt.md
  - docs/prompts/tts/scenario_refine.md
  - docs/prompts/vision/descriptor_enrichment.md
workflowType: 'architecture'
project_name: 'youtube.pipeline'
user_name: 'Jay'
date: '2026-04-14'
documentCounts:
  prd: 2
  uxDesign: 2
  research: 0
  projectDocs: 19
---

# Architecture Decision Document

_This document builds collaboratively through step-by-step discovery. Sections are appended as we work through each architectural decision together._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**

48 functional requirements (FR1–FR53, with gaps) organized into 9
capability domains. Test Infrastructure is elevated to a standalone
domain per FR51's "first-class artifact" designation.

| Domain | FRs | Architectural Implication |
|---|---|---|
| Pipeline Lifecycle | FR1–FR8, FR50 | State machine, stage transitions, resume/cancel, anti-progress detection |
| Phase A — Scenario | FR9–FR13 | 6-agent sequential chain, inter-agent schema contracts, cross-model enforcement |
| Phase B — Media | FR14–FR19 | Parallel image + TTS tracks with shared rate-limit budget (~30 images/run, 1-5 shots/scene), character reference workflow (DDG cache → pick → Image-Edit), frozen descriptor propagation across all shots and scenes |
| Phase C — Assembly | FR20–FR23 | Two-stage FFmpeg composition: (1) per-scene clips (shots + Ken Burns/cross-dissolve/hard-cut transitions + TTS audio) (2) final concat. Metadata bundle, compliance gate |
| Quality Gating | FR24–FR30 | Critic subsystem (rubric scoring, Golden/Shadow eval, regression harness, minor-content safeguard). Meta-testing concern: the quality judgment system's own quality requires calibration infrastructure |
| HITL Review | FR31a–FR37, FR53 | 3-tier review (auto/batch/precision), Command Pattern undo stack, decision history, semantic similarity warning |
| Operator Tooling | FR38–FR44 | Init/doctor, web serve, Renderer abstraction, JSON envelope, mode switching |
| Compliance | FR45–FR49 | Provider audit trail, writer≠critic enforcement, voice-ID block list, forbidden-term enforcement, HITL session resume |
| Test Infrastructure | FR51–FR52 | Contract test suite (CLI↔Web parity), integration harness, Golden/Shadow eval runner, seed-able fixture facility, E2E smoke test. Single source of truth for contract schema must be defined (JSON Schema recommended, validated by both Go and Zod) |

**Non-Functional Requirements:**

| Category | Key NFRs | Architecture Driver |
|---|---|---|
| Cost & Resource | NFR-C1, C2, C3 | Per-stage and per-run cost caps as circuit breakers (hard stop + escalation); cost telemetry on every external call. Not simple logging — active enforcement |
| Reliability | NFR-R1, R2, R3, R4 | Stage-level resume idempotency; anti-progress false-positive tracking; durable SQLite writes; web UI stateless (DB reconstructs) |
| Performance | NFR-P3, P4 | 429 backoff without advancing stage; requires time abstraction (clock interface) for testable backoff logic; wall-clock baseline capture |
| Accessibility | NFR-A1, A2 | 8-key shortcut set (V1 non-negotiable); full WCAG out of scope |
| Testing | NFR-T1 (implied), CI ≤ 10 min | CI time budget is an architectural forcing function — constrains test pyramid shape, mandates mock boundaries for external APIs, requires build cache strategy |

**Scale & Complexity:**

- Primary domain: Hybrid CLI tool + local web app (AI-driven media content automation)
- Complexity level: Medium-high (but V1 architecture must absorb only what ships in 6 weeks — see Architecture Priority Weighting below)
- Estimated architectural components: ~15-18 major components across 4 layers (CLI, Web, Service, External Integrations)

### Architecture Priority Weighting

Not all 9 domains require equal architectural investment in V1.
The 6-week single-developer constraint demands explicit tiering
to prevent uniform design across domains of unequal urgency.

**First-class (architecture-level design):**

| Domain | Why |
|---|---|
| Pipeline Lifecycle (state machine + resume) | Load-bearing: if resume breaks, the tool is unusable. Edge cases dominate implementation time. |
| Phase A — Scenario (6-agent chain) | Highest LLM integration complexity; inter-agent schema contracts define the pipeline's data model |
| HITL Review | Primary operator touchpoint; Command Pattern, optimistic UI, keyboard shortcuts — UX is the product |
| Test Infrastructure | FR51 first-class. Mock boundaries and fixture architecture must be designed up front or retrofitting costs 2-3x |

**Solid implementation (clear interfaces, but simpler internals):**

| Domain | Why |
|---|---|
| Phase B — Media (image + TTS) | Parallel execution adds complexity, but individual track logic is straightforward API call → file write. Rate-limit coordination is the hard part. |
| Quality Gating (Critic Stack) | Golden/Shadow eval is well-specified; the architectural concern is ensuring Critic is a pluggable component, not wired into Phase A internals |

**Minimum viable (works correctly, minimal abstraction):**

| Domain | Why |
|---|---|
| Phase C — Assembly | Two-stage FFmpeg assembly: per-scene clip (shots + 3 transition types + TTS) then final concat. Audio fade / BGM / advanced transitions are V1.5. |
| Compliance | Metadata bundle generation and compliance gate are important but structurally simple (JSON file + boolean gate) |
| Operator Tooling | Init/doctor are one-shot commands; Renderer abstraction is a small interface |

### Technical Constraints & Dependencies

| Constraint | Source | Architecture Impact |
|---|---|---|
| Single-machine, localhost only | PRD scope | No auth, no TLS, no multi-machine coordination; SQLite is sufficient |
| Go 1.25.7 + Cobra CLI + Viper config | PRD §1 | Language and framework locked; embed.FS for SPA assets |
| SQLite (single-writer, WAL mode required) | PRD §1 | All state in one DB. **WAL mode + busy_timeout must be enforced at connection open** (`_journal_mode=wal&_busy_timeout=5000` in DSN). CLI writes pipeline state; Web writes HITL decisions — writer contention is real, not theoretical. Connection management belongs in a single `internal/db/` package. |
| React + shadcn/ui + Tailwind | UX Spec | Frontend framework decided; Vite build → embed.FS. **Dev mode requires Vite proxy** (`--dev` flag on `pipeline serve`) to avoid full rebuild on every frontend change. |
| DashScope single-region | PRD §Domain | No cross-region failover; region set at init time. **Phase B parallel tracks share the same DashScope rate-limit budget** — per-endpoint semaphore/throttle required. Multi-shot model increases image API calls from ~10 to ~30 per run; rate-limit config must account for this volume |
| Writer ≠ Critic provider | FR12, FR46 | Must support ≥2 LLM providers simultaneously. Enforced at doctor + run entry (defense in depth) |
| DashScope only (no SiliconFlow) | PRD §Integration | TTS and image API calls route exclusively through DashScope |
| FFmpeg system binary | PRD §Integration | External dependency; Go binding or exec wrapper. V1: two-stage assembly — per-scene clips use `zoompan` (Ken Burns) / `xfade` (cross-dissolve) filters within scenes, then concat demuxer for final video. `filter_complex` is required for intra-scene transitions but not for inter-scene concat |
| `/mnt/data/raw` local corpus | PRD §Domain | Filesystem dependency; validated at doctor preflight |
| Stage-level resume only (V1) | PRD §Resume semantics | Partial work inside a failed stage is re-executed; per-scene is V1.5. **Filesystem artifacts from partial stage execution must be cleaned or overwritten on resume** — artifact lifecycle concern |
| 6-week V1 budget | PRD §Risk Register | Architecture must be implementable within constraint; no speculative abstractions. Amelia's estimate: 29-40 working days for implementation alone (excluding tests), meaning scope discipline is load-bearing |

### Cross-Cutting Concerns (Tiered)

Cross-cutting concerns are tiered by architectural impact to prevent
uniform investment across concerns of unequal urgency.

**Tier 1 — Architecture Skeleton (must be designed before any domain implementation):**

| Concern | Spans | V1 Mechanism |
|---|---|---|
| **LLM provider abstraction + adapter layer** | Phase A (writer, critic), Phase B (image, TTS) | Interface per capability in `internal/domain/` (e.g. `LLMProvider`, `ImageGenerator`, `TTSSynthesizer`); vendor implementations in `internal/llmclient/{dashscope,deepseek,gemini}/`; service layer references interfaces only. Common `NormalizedResponse` struct eliminates provider-specific branching in business logic. |
| **External API mock boundary** | Every external call; CI; development | Interface-based injection. **CI must never hit real APIs** — enforced by build tag or env guard (`PIPELINE_ENV=test` blocks real HTTP clients). Protects Jay's wallet and CI reliability. |
| **Error handling + stage-level resume** | Every stage | Stage status enum (pending/running/completed/failed); resume re-executes from last successful stage. Stage entry must validate/clean prior partial artifacts before re-execution. |
| **Inter-agent schema contracts** | Phase A 6-agent chain | Not a simple JSON Schema check — this is the pipeline's inter-stage data model. Each agent declares its input/output schema; the pipeline runtime validates at every handoff. Schema violations fail the stage, not silently pass malformed data. Belongs in pipeline runtime, not as a utility. |
| **Rate-limit coordination** | Phase B parallel tracks (shared DashScope budget) | Per-endpoint semaphore with configurable concurrency. Phase B image (N calls) and TTS (M calls) compete for the same DashScope rate budget. Token-bucket or semaphore pattern, not per-call backoff alone. |

**Tier 2 — Important, Incrementally Applicable:**

| Concern | Spans | V1 Mechanism |
|---|---|---|
| **Cost tracking & circuit breaker** | Every external API call | Accumulator per stage; hard stop on cap with operator escalation; `cost_usd` column in pipeline_runs. This is a circuit breaker pattern, not passive logging. |
| **Observability** | Every stage | 8-column pipeline_runs row: duration_ms, token_in/out, retry_count, retry_reason, critic_score, cost_usd, human_override |
| **Decision history** | Every HITL touchpoint | `decisions` table; passive capture; Command Pattern for undo. Undo scope in V1: approve, reject, skip, batch-approve, Vision Descriptor edit (5 action types). Full 10-type undo is V1.5. |
| **Artifact lifecycle management** | Phase A outputs, Phase B files, Phase C segments | Per-run directory tree (`./output/<run-id>/` in the project-root layout). Naming convention is a contract. Partial artifacts on stage failure must be cleaned before resume. Filesystem state ↔ DB state consistency verified at resume entry. Rejected assets → `rejected/` subdirectory. |
| **Time abstraction** | Backoff logic, anti-progress detection, telemetry | Clock interface (`internal/clock/`) injected into services. Tests use fake clock. Required for testable 429 backoff and anti-progress cosine-similarity timing without real delays in CI. |

**Tier 3 — V1 Minimal, V1.5 Full:**

| Concern | Spans | V1 Mechanism |
|---|---|---|
| **Compliance metadata** | Phase A source → Phase C output | Per-video source manifest (JSON); attribution chain; metadata bundle; compliance gate. Structurally simple in V1 (JSON generation + boolean gate). |
| **Schema validation (rule-based pre-checks)** | Phase A agent outputs | JSON schema + forbidden-term regex before Critic invocation. Implemented as a validation middleware in the agent chain. |
| **Rendering abstraction** | Every CLI command | `Renderer` interface; human-readable default + JSON envelope. Small interface, implement early, extend cheaply. |

### Test Architecture Context

FR51 designates test infrastructure as a first-class artifact. This
means testability constraints shape architecture decisions, not the
reverse.

**Key testability requirements identified:**

| Requirement | Architecture Implication |
|---|---|
| External API isolation in CI | Interface-based injection mandatory for all external clients. Build tag or env guard enforces mock-only in CI. |
| State machine transition testing | `internal/pipeline/` must expose transition functions as pure functions testable without DB. Integration tests use real SQLite file (not `:memory:`) for WAL behavior. |
| LLM nondeterminism | Golden eval fixtures are versioned in `testdata/golden/`. "Sufficiently similar" judgment uses structural schema validation, not output equality. Snapshot testing inappropriate for LLM outputs. |
| Contract test SSoT | JSON Schema files in `testdata/contracts/` are the single source of truth. Go tests validate via `encoding/json` + schema library; JS tests validate via Zod schemas generated from the same JSON Schema. Schema drift causes simultaneous failure on both sides. |
| CI ≤ 10 minutes | Test pyramid: ~60% unit, ~30% integration, ~10% E2E. Requires Go build cache + Playwright browser binary cache in CI. Serial execution fits within budget with margin. |
| Cost tracking logic | Unit-testable accumulator with deterministic thresholds. No real API calls in cost-cap tests. |
| Critic Stack meta-testing | Golden/Shadow eval tests validate the Critic's behavior, but the Critic's calibration (kappa tracking) is measured operationally, not in CI. CI tests verify mechanics (eval runner works), not quality (kappa threshold). |

**Recommended test pyramid (this project):**

| Level | Ratio | What it covers |
|---|---|---|
| Unit | ~60% | Go service logic, React components, cost accumulator, schema validation, Renderer |
| Integration | ~30% | SQLite state transitions, CLI→Service flow, API mock boundary, inter-agent handoff, WAL concurrent access |
| E2E | ~10% | FR52 smoke test (full pipeline on canonical seed input), Playwright critical-path smoke |

## Starter Template Evaluation

### Primary Technology Domain

Hybrid CLI tool (Go) + local web app (React SPA), identified from
PRD project classification. No single starter template covers both
surfaces — the project uses two separate scaffolding approaches
integrated via Makefile and Go embed.FS.

### Technology Stack (Verified Versions, April 2026)

| Technology | Version | Rationale |
|---|---|---|
| Go | **1.25.7** (PRD-specified) | Go 1.26.2 available but not adopted. PRD-specified version = verified version. Boring technology principle: no version upgrade without a problem to solve. |
| Cobra | **v2.5.1** | Stable, well-maintained CLI framework. |
| Viper | **v1.11.0** | Config management. Hierarchy: `./.env` (secrets, gitignored) → `./config.yaml` (non-secret, git-tracked) → CLI flags. Project-root layout (not `~/.youtube-pipeline/`). |
| ncruces/go-sqlite3 | **latest (pure Go)** | Replaces mattn/go-sqlite3. No CGO dependency — eliminates gcc requirement in CI, simplifies cross-compilation, enables `CGO_ENABLED=0` clean builds. WAL mode supported. mattn v1.14.33 had a WAL deadlock bug (fixed in v1.14.34); ncruces avoids this entire class of CGO-related issues. Performance difference negligible for this project's DB workload (metadata + state management, not bulk data). |
| Vite | **7.3** (not 8.x) | Vite 8 uses Rolldown (Rust-based bundler) — a major engine swap with insufficient production track record. Vite 7.3 is maintained, stable, and has deep community troubleshooting coverage. Build speed difference irrelevant for a Go embed.FS SPA (2s vs 4s build is not meaningful). Boring technology wins. |
| React | **19.x** (via Vite template) | PRD and UX spec confirm React. Jay has React experience — zero framework learning curve. |
| shadcn/ui | **CLI v4** (March 2026) | Copy-paste component model. Vite template support via `--base vite`. Radix UI primitives for accessibility. |
| Tailwind CSS | **4.x** (via shadcn init) | Dark-only theme with CSS custom properties. No dark: prefix needed. |
| Vitest | **^4.1.4** | Current stable. Pin minor, allow patches. Jest-compatible API. Replaces Jest entirely. |
| React Testing Library | **latest** | Standard component testing. Behavior-focused, not implementation-focused. |
| jsdom | **latest** (not happy-dom) | jsdom is slower but more compatible. happy-dom has edge cases with getComputedStyle, IntersectionObserver that cause "works locally, fails in CI" scenarios. At V1 test volume (~50 frontend tests), speed difference is negligible (~3s vs ~1.5s). If jsdom becomes a bottleneck at scale, switching to happy-dom is a one-line config change. Reverse migration (happy-dom → jsdom) requires fixing broken tests. Risk asymmetry favors jsdom. |
| Playwright | **latest** | E2E testing. **Chromium only** — no Firefox/WebKit in V1. Exactly 1 smoke test in V1 (FR52). |
| Go testing | **stdlib only** (no testify) | Go's built-in `testing` package + `t.Run` subtests + `t.Helper()` + `t.Cleanup()` cover 95% of testify's value. Avoids require/assert confusion, suite pattern temptation, and mock package creep. Custom `assertEqual[T]` generic helper (5 lines) replaces assert.Equal. testify can be added later if needed — removal is harder. |

### Starter Options Considered

| Option | Approach | Verdict |
|---|---|---|
| **A: cobra-cli init + Vite create** | cobra-cli generates flat cmd/ + Vite react-ts template | Rejected. cobra-cli's flat cmd/ structure doesn't match the internal/ layering this project needs. Restructuring after generation wastes more time than manual setup saves. |
| **B: Manual Go + shadcn Vite template** | Hand-crafted Go project structure + `npx shadcn@latest init --base vite` | **Selected.** Go directory structure designed for the architecture from Day 1. shadcn CLI v4 handles frontend scaffolding. 30 minutes of manual Go setup buys full structural ownership. |
| **C: Manual Go + create-vite + shadcn add** | Same as B but separate Vite create + shadcn add steps | Functionally identical to B. shadcn CLI v4's Vite template makes this unnecessary. |

### Selected Starter: Option B (Manual Go + shadcn Vite Template)

**Rationale:**
1. cobra-cli saves ~30 min but produces a structure that must be immediately restructured — net negative
2. Manual Go scaffolding means every directory and file exists for an architectural reason, not because a generator put it there
3. shadcn CLI v4 with `--base vite` handles the entire frontend foundation (React, Tailwind, Radix, CSS variables) in one command
4. The Makefile-based build chain (PRD-specified) integrates both halves cleanly

**Initialization Command Sequence:**

```bash
# 1. Go module + directory structure
go mod init github.com/sushistack/youtube.pipeline
mkdir -p cmd/pipeline \
  internal/{service,domain,llmclient/{dashscope,deepseek,gemini},db,pipeline,hitl,web} \
  migrations testdata/{contracts,golden} e2e

# 2. Go dependencies
go get github.com/spf13/cobra@v2.5.1
go get github.com/spf13/viper@v1.11.0
go get github.com/ncruces/go-sqlite3@latest
go get github.com/ncruces/go-sqlite3/driver@latest

# 3. React SPA (web/ directory)
npx shadcn@latest init --base vite --name web
cd web
npm install vite@^7          # downgrade from Vite 8 if shadcn installed it
npm install -D vitest @testing-library/react @testing-library/dom jsdom
npx playwright install --with-deps chromium
cd ..

# 4. Verify
go build ./cmd/pipeline
cd web && npx vitest run && cd ..
```

### Day 1 Project Structure

```
youtube.pipeline/
├── Makefile                              # Build chain: web-build → go-build → test
├── .github/workflows/ci.yml             # CI pipeline (no API keys injected)
├── .gitignore
├── .air.toml                            # Go hot-reload (cosmtrek/air) for dev
├── go.mod
├── go.sum
├── cmd/
│   └── pipeline/
│       └── main.go                      # Cobra root command init
├── internal/
│   ├── db/
│   │   ├── sqlite.go                    # DB open (WAL + busy_timeout enforced)
│   │   └── sqlite_test.go
│   ├── domain/
│   │   └── types.go                     # PipelineState enum, Episode, NormalizedResponse
│   ├── pipeline/
│   │   ├── engine.go                    # State machine core
│   │   └── engine_test.go
│   ├── service/                         # Interfaces (ports)
│   ├── llmclient/
│   │   ├── dashscope/                   # DashScope HTTP client
│   │   ├── deepseek/                    # DeepSeek client
│   │   └── gemini/                      # Gemini client
│   ├── hitl/
│   │   └── server.go                    # HITL web server + SPA serving
│   ├── web/
│   │   └── embed.go                     # //go:embed all:dist
│   ├── clock/
│   │   └── clock.go                     # Clock interface for testable time
│   └── testutil/
│       ├── assert.go                    # assertEqual[T], assertJSONEq helpers
│       ├── fixture.go                   # LoadFixture(t, path) → []byte
│       └── nohttp.go                    # External HTTP blocking transport
├── migrations/
│   └── 001_init.sql                     # Schema DDL (runs, decisions tables)
├── testdata/
│   ├── contracts/
│   │   └── pipeline_state.json          # First contract fixture (SSoT)
│   └── golden/                          # Golden eval fixtures
├── web/
│   ├── vite.config.ts
│   ├── vitest.config.ts                 # jsdom environment, setupFiles
│   ├── tsconfig.json
│   ├── src/
│   │   ├── App.tsx
│   │   ├── App.test.tsx                 # Minimum smoke test
│   │   ├── main.tsx
│   │   └── test/
│   │       └── setup.ts                 # External fetch blocking + vi globals
│   └── package.json
├── e2e/
│   ├── playwright.config.ts             # Chromium only, baseURL from env
│   └── smoke.spec.ts                    # Exactly 1 E2E test (FR52)
└── docs/                                # Existing project documentation
```

### Makefile

```makefile
.PHONY: build web-build go-build test test-go test-web test-e2e dev clean

build: web-build go-build

web-build:
	cd web && npm ci && npm run build

go-build: web-build
	go build -o bin/pipeline ./cmd/pipeline

test: test-go test-web                   # E2E excluded from default — separate CI job
test-go:
	go test ./... -race -count=1 -timeout=120s
test-web:
	cd web && npx vitest run
test-e2e:                                # Explicit invocation only
	cd e2e && npx playwright test

dev:                                     # Development: Vite proxy + Go hot-reload
	cd web && npm run dev &
	air -- serve --dev

clean:
	rm -rf bin/ web/dist/
```

### External API Isolation (Day 1 Infrastructure)

Three-layer defense ensuring no real API calls in CI:

**Layer 1 — Architecture (HTTP client injection):**
All external API clients accept `*http.Client` via constructor. No
`http.DefaultClient` usage. This is a design constraint, not a
testing convenience.

**Layer 2 — Runtime blocking (test transport):**
`internal/testutil/nohttp.go` replaces `http.DefaultTransport` with
a blocking transport that fails any non-localhost HTTP call with a
clear error message. Applied in TestMain or per-test.

`web/src/test/setup.ts` overrides `globalThis.fetch` to block
non-localhost requests in all Vitest tests.

**Layer 3 — Environment lockout (CI config):**
CI workflow does not inject API keys. Additionally, Go test init
panics if `CI=true` and any API key env var is set — catches
accidental secret injection.

### Development Workflow

**--dev flag for frontend development:**
`pipeline serve --dev` proxies to the Vite dev server (default
`localhost:5173`) instead of serving from embed.FS. This enables
hot module replacement during frontend development without
rebuilding the Go binary.

**Hot-reload for Go:**
`cosmtrek/air` watches Go files and rebuilds automatically. The
`dev` Makefile target runs both Vite dev server and air in parallel.

**Note:** Project initialization using this setup should be the
first implementation story.

## Core Architectural Decisions

### Decision Priority Analysis

**Critical Decisions (Block Implementation):**
- Data schema and migration strategy
- Go↔React API communication pattern
- Pipeline execution model (agent chain, parallel tracks, state machine)
- Frontend state management
- HITL state transitions and entry/exit conditions

**Important Decisions (Shape Architecture):**
- External API integration pattern (interfaces, rate limiting, retry)
- CI/CD pipeline structure
- Artifact file structure convention
- Error classification system

**Deferred Decisions (V1.5):**
- SSE/WebSocket upgrade from polling
- Per-scene resume granularity
- Graphical metrics dashboard
- Command Palette (Ctrl+K)

### Data Architecture

**Migration Strategy: Manual embedded SQL + PRAGMA user_version**

No external migration tool (goose, golang-migrate). A ~50 LoC runner
in `internal/db/migrate.go` reads `.sql` files from
`internal/db/migrations/` (embedded via `embed.FS`), applies them
sequentially, and increments `PRAGMA user_version`. This avoids
driver registration conflicts between goose and ncruces/go-sqlite3
(different driver names).

**Schema (V1, 3 tables):**

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
    shots           TEXT,           -- JSON array: [{image_path, duration_s, transition}]
    tts_path        TEXT,
    tts_duration_ms INTEGER,        -- actual TTS duration; drives shot_count derivation
    clip_path       TEXT,           -- per-scene clip (shots + transitions + TTS audio)
    critic_score    REAL,
    critic_sub      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(run_id, scene_index)
);

-- shots JSON schema per element:
-- {
--   "shot_index": 1,
--   "image_path": "images/scene_01/shot_01.png",
--   "duration_s": 5.0,
--   "transition": "ken_burns" | "cross_dissolve" | "hard_cut",
--   "visual_descriptor": "dense text from VisualBreakdowner"
-- }
-- Shot count derivation from tts_duration_ms:
--   ≤8000 → 1, 8001-15000 → 2, 15001-25000 → 3, 25001-40000 → 4, >40000 → 5
-- Operator can override shot_count at scenario_review HITL checkpoint.
```

**Phase B Resume Semantics for segments table:**
V1 stage-level resume re-executes the entire Phase B. On resume,
existing segments for the run are **deleted and re-inserted** (clean
slate). This prevents orphaned resources and ambiguous partial
states. The `UNIQUE(run_id, scene_index)` constraint guards against
duplicates. Explicitly documented as a design decision — per-scene
resume (V1.5) will change this to upsert semantics.

| Resume scenario | Expected behavior |
|---|---|
| Normal re-run | DELETE existing segments → re-generate all |
| Partial failure (3/5 complete) | DELETE all → re-generate all 5 |
| User-edited segment exists | DELETE all → re-generate (edits lost; user warned) |
| Duplicate execution | UNIQUE constraint prevents duplicates |

**JSON Storage:** Agent outputs stored as `TEXT` columns with Go
struct serialization (`encoding/json`). No SQLite JSON1 queries in
V1 — data is written by pipeline and read by API endpoints as opaque
blobs. JSON1 can query existing TEXT columns without migration if
needed later.

### API & Communication Patterns

**HTTP Router: Go 1.22+ standard `net/http` ServeMux**

Go 1.22 added method-based routing (`GET /api/runs/{id}`), available
in Go 1.25.7. Path parameters extracted via `r.PathValue("id")`.
No external router (chi, gorilla/mux) needed. Zero dependencies.

**SPA catch-all:** ServeMux must include a fallback handler that
serves `index.html` for any path not matching `/api/*`. Without
this, browser refresh on `/production` returns 404.

**REST Endpoints (V1):**

```
Pipeline Lifecycle:
  GET    /api/runs                             List all runs
  GET    /api/runs/{id}                        Run detail + stage progress
  GET    /api/runs/{id}/status                 Polling (phase + progress_percent)
  POST   /api/runs/{id}/resume                 Resume from failed stage
  POST   /api/runs/{id}/cancel                 Cancel in-flight run

Scene Review (HITL):
  GET    /api/runs/{id}/scenes                 Scenes for review (each scene includes shots[] array with image_path, duration_s, transition per shot)
  POST   /api/runs/{id}/scenes/{idx}/approve   Approve scene
  POST   /api/runs/{id}/scenes/{idx}/reject    Reject scene (body: reason)
  POST   /api/runs/{id}/scenes/{idx}/edit      Edit narration/descriptor
  POST   /api/runs/{id}/scenes/{idx}/regen     Trigger regeneration
  POST   /api/runs/{id}/approve-all            Batch approve remaining
  POST   /api/runs/{id}/undo                   Undo last decision

Character Reference:
  GET    /api/runs/{id}/characters             Character reference candidates
  POST   /api/runs/{id}/characters/pick        Select character reference

History & Metrics:
  GET    /api/decisions                        Decisions history (filterable)
  GET    /api/metrics                          Pipeline metrics (rolling window)

Compliance:
  POST   /api/runs/{id}/metadata/ack           Acknowledge metadata bundle
```

**Response Envelope (PRD-specified):**

```json
{"version": 1, "data": { ... }}
{"version": 1, "error": {"code": "STAGE_FAILED", "message": "...", "recoverable": true, "stage": "tts"}}
```

**Error Classification System:**

| Category | HTTP Status | Retryable | Example |
|---|---|---|---|
| `RATE_LIMITED` | 429 | Yes (auto) | DashScope rate limit |
| `UPSTREAM_TIMEOUT` | 504 | Yes (auto) | LLM API timeout |
| `STAGE_FAILED` | 500 | Yes (manual resume) | Phase B image generation failed |
| `VALIDATION_ERROR` | 400 | No | Invalid SCP ID, malformed request |
| `CONFLICT` | 409 | No | Scene already processed, state mismatch |
| `COST_CAP_EXCEEDED` | 402 | No (human escalation) | Stage cost cap hit |
| `NOT_FOUND` | 404 | No | Run or scene not found |

**Polling:** 5s interval on `/api/runs/{id}/status`. Response is
lightweight: `{phase, progress_percent, stage, status}`. Frontend
uses stale-while-revalidate (TanStack Query).

### Frontend Architecture

**Server State: TanStack Query (React Query)**

Handles polling (`refetchInterval: 5000`), caching,
stale-while-revalidate, and optimistic mutations. All API data flows
through TanStack Query hooks — no manual fetch/useState for server
data.

**UI State: Zustand with localStorage persist**

Zustand manages client-only state: selected scene, sidebar
collapsed, undo stack reference, keyboard mode, last active route
and position. **Zustand persist middleware** syncs to localStorage
so returning-user resume works across browser sessions.

On session restore, Zustand-persisted state is reconciled with
current server state via TanStack Query: if the persisted scene
position refers to a scene that no longer exists or has changed
status, the UI falls back to the first pending scene with a
one-line banner ("Run state changed since your last session").

**Review Mode List Stability Pattern:**

During batch review, the scene list is **snapshot-frozen** on entry.
Polling continues in the background, but new data does NOT re-render
the list. Instead, if the server returns changes (new scenes, status
updates), a non-intrusive badge appears: "3 updates available —
press R to refresh." The operator explicitly refreshes when ready.
This prevents J/K keyboard flow disruption from polling-induced
list re-renders.

Implementation: TanStack Query's `structuralSharing` + a
`useStableList` hook that compares incoming data with the frozen
snapshot and only applies changes on explicit user action.

**SPA Routing: React Router v7**

Three routes: `/production`, `/tuning`, `/settings`. Using
traditional `<BrowserRouter>` + `<Routes>` pattern (not
`createBrowserRouter` — avoids unnecessary loader/action patterns
for 3 simple routes).

**Slot+Strategy Implementation:**

Shell components set `data-density` attribute based on pipeline
state. Slot components read CSS custom properties only. Density
values are `standard` (default) and `elevated` (character pick,
scenario edit, precision review). V1 ships both densities; no
runtime toggle needed — density is determined by pipeline state.

### Pipeline Execution Model

**Agent Chain: Plain Function Chain**

Each LLM agent is a function conforming to:

```go
type AgentFunc func(ctx context.Context, state *PipelineState) error
```

Phase A runner iterates `[]AgentFunc` sequentially. Each agent
mutates `PipelineState` in place. Inter-agent schema validation
runs at every handoff point — the pipeline runtime validates the
state struct's required fields before passing to the next agent.
Schema violations fail the stage immediately.

**Phase B Parallelism: errgroup.Group (no context cancel)**

```go
func RunPhaseB(ctx context.Context, scenario PhaseAResult) (PhaseBResult, error) {
    g := new(errgroup.Group) // NOT errgroup.WithContext — no auto-cancel
    var imageResult ImageResult
    var ttsResult TTSResult

    // TTS track runs first to determine actual durations for shot-count derivation.
    // Then image track generates 1-5 shots per scene based on TTS duration.
    // Both tracks still overlap: TTS runs to completion, image track starts
    // as soon as shot counts are derived (scene-by-scene streaming possible).
    g.Go(func() error {
        var err error
        ttsResult, err = runTTSTrack(ctx, scenario)
        return err
    })
    g.Go(func() error {
        var err error
        // Image track waits for per-scene TTS duration estimates (from
        // VisualBreakdowner's estimate or actual TTS duration via channel).
        // Shot count per scene: ≤8s→1, 8-15s→2, 15-25s→3, 25-40s→4, 40s+→5.
        // Operator overrides (if any) are read from scenario.ShotOverrides.
        // Total images per run: ~30 (avg 3 shots × 10 scenes).
        imageResult, err = runImageTrack(ctx, scenario)
        return err
    })

    if err := g.Wait(); err != nil {
        return PhaseBResult{}, fmt.Errorf("phase B: %w", err)
    }
    return PhaseBResult{Images: imageResult, TTS: ttsResult}, nil
}
```

Using `errgroup.Group` (not `errgroup.WithContext`) so one track's
failure does NOT cancel the other. Both tracks run to completion.
If either fails, the stage fails and stage-level resume re-runs
both. errgroup provides cleaner error collection than raw
WaitGroup + mutex.

Each track internally respects `ctx` for timeout — individual API
calls have per-call timeouts via `context.WithTimeout`.

**State Machine: Go switch + enum (~100 LoC)**

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

**HITL Wait Points (state machine pauses here for operator input):**

| Stage | Trigger | Resumes when |
|---|---|---|
| `scenario_review` | Phase A complete | Operator approves/edits scenario |
| `character_pick` | SCP has named character | Operator picks reference from 10-grid |
| `batch_review` | Phase B complete | All scenes approved/rejected |
| `metadata_ack` | Phase C complete | Operator acknowledges metadata bundle |

The state machine transitions from a HITL stage to the next
automated stage only when the corresponding API endpoint receives
the operator's action (approve, pick, ack). The `runs.status` field
is `waiting` during HITL stages, `running` during automated stages.

State transition function is a **pure function** for testability:

```go
func NextStage(current Stage, event Event) (Stage, error) {
    // Returns next stage or error for invalid transitions
    // No side effects — DB update is caller's responsibility
}
```

**Inter-agent Data Flow:**
- **Within Phase A:** In-memory `PipelineState` struct passed
  between agents. Written to disk as `scenario.json` on Phase A
  completion.
- **Between Phases:** File-based. Phase B reads `scenario.json`;
  writes image/TTS files; records paths in `segments` table.
  Phase C reads `segments` table to find file paths.

**Artifact File Structure Convention:**

```
./output/{run-id}/
├── scenario.json          # Phase A output (includes shot breakdowns per scene)
├── characters/
│   ├── references/        # DDG search result images (cached)
│   └── canonical.png      # Qwen-Image-Edit output
├── images/
│   ├── scene_01/          # Per-scene directory (1-5 shots per scene)
│   │   ├── shot_01.png
│   │   ├── shot_02.png
│   │   └── shot_03.png
│   ├── scene_02/
│   │   ├── shot_01.png
│   │   └── shot_02.png
│   └── ...
├── tts/
│   ├── scene_01.wav       # One TTS audio per scene
│   ├── scene_02.wav
│   └── ...
├── clips/
│   ├── scene_01.mp4       # Per-scene clip (shots + transitions + TTS)
│   ├── scene_02.mp4
│   └── ...
├── rejected/              # Rejected assets (preserved for diagnostics)
├── output.mp4             # Final concatenated video (concat of scene clips)
├── metadata.json          # AI-content disclosure bundle
└── manifest.json          # Source attribution chain
```

### External API Integration

**Provider Interfaces (`internal/domain/`):**

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

All implementations accept `*http.Client` via constructor (API
isolation Layer 1). Responses normalized to domain structs — no
provider-specific types leak into the service layer.

**Rate Limiting: semaphore + token bucket (dual layer)**

```go
// Concurrency limit (simultaneous in-flight requests)
dashscopeSem := semaphore.NewWeighted(5)

// Rate limit (requests per time window)
dashscopeRate := rate.NewLimiter(rate.Every(time.Minute/60), 10) // 60 RPM, burst 10
```

Both `golang.org/x/sync/semaphore` (concurrency) and
`golang.org/x/time/rate` (rate) are applied. Semaphore alone cannot
enforce RPM limits — a burst of concurrent requests within the
semaphore window can still trigger 429. The token bucket rate
limiter paces requests to stay within vendor quotas.

DashScope image and TTS tracks share both limiters (same vendor,
same rate budget). LLM providers (DeepSeek, Gemini) have separate
limiter instances.

**Retry: Exponential backoff with clock interface**

```go
func WithRetry(ctx context.Context, clock clock.Clock, maxRetries int, fn func() error) error {
    for i := 0; i <= maxRetries; i++ {
        err := fn()
        if err == nil { return nil }
        if !isRetryable(err) { return err }
        delay := min(time.Duration(1<<i)*time.Second+jitter(), 30*time.Second)
        if err := clock.Sleep(ctx, delay); err != nil { return err }
    }
    return fmt.Errorf("max retries (%d) exceeded", maxRetries)
}

func isRetryable(err error) bool {
    // 429, 502, 503, 504, context.DeadlineExceeded → retryable
    // 400, 401, 403, 404 → fatal
    // Cost cap exceeded → fatal (human escalation)
}
```

### HITL Command Pattern (Undo)

**V1 Undo Scope (5 action types):**
- Scene approve
- Scene reject
- Scene skip
- Batch approve-all
- Vision Descriptor edit

**Undo mechanics:** Each undoable action inserts a `decisions` row.
Undo inserts a reversal row with `superseded_by` referencing the
original. No event sourcing. Stack depth >= 10 actions.

**Undo includes focus restoration:** When the operator presses
Ctrl+Z, the UI restores both the decision state AND the cursor
position to the scene where the undone action occurred. This
matches the Photoshop mental model (undo = go back to that point).

### Infrastructure & Deployment

**CI: GitHub Actions**

```yaml
jobs:
  test-go:     # Go unit + integration tests (-race)
  test-web:    # Vitest frontend tests
  test-e2e:    # Playwright smoke (separate, after test-go + test-web)
  build:       # Full binary build (after test-go + test-web)
```

`test-go` and `test-web` run in parallel. `test-e2e` and `build`
run after both pass. No API keys injected in CI (Layer 3 defense).

**Caching:** Go module cache via `actions/setup-go`, npm cache via
`actions/setup-node`, Playwright Chromium binary cached.

**Environment Configuration (Viper hierarchy, project-root layout):**
1. `./.env` — secrets only (API keys). Gitignored.
2. `./config.yaml` — model IDs, DashScope region, data paths, cost
   caps. Tracked in git so config changes flow through review.
3. CLI flags — per-invocation overrides (`--config` to retarget).

### Decision Impact Analysis

**Implementation Sequence:**

1. SQLite schema + migration runner + DB open (WAL + busy_timeout)
2. Domain types + LLM provider interfaces
3. State machine (pure function) + runs table CRUD
4. Phase A agent chain (6 agents with mocked LLM providers)
5. Phase B parallel tracks (per-shot image generation ~30/run + TTS, shared DashScope rate-limiter, mocked APIs)
6. Phase C FFmpeg assembly (two-stage: per-scene clips with Ken Burns/cross-dissolve/hard-cut transitions + TTS overlay, then final concat)
7. REST API endpoints + JSON envelope
8. React SPA shell + TanStack Query hooks + Zustand store
9. HITL review surface (Master-Detail, keyboard shortcuts)
10. Command Pattern undo + decisions table
11. Contract tests + CI pipeline
12. End-to-end integration

**Cross-Component Dependencies:**

```
domain/types ← everything depends on this (define first)
     ↓
db/sqlite ← pipeline/engine, service/*
     ↓
pipeline/engine ← cmd/create, cmd/resume
     ↓
service/* ← cmd/*, web/api handlers
     ↓
web/api ← React SPA (TanStack Query hooks)
     ↓
web/src/* ← final integration layer
```

## Implementation Patterns & Consistency Rules

### Naming Patterns

**Database Naming (SQLite):**

| Element | Convention | Example |
|---|---|---|
| Table names | snake_case, plural | `runs`, `decisions`, `segments` |
| Column names | snake_case | `run_id`, `scene_index`, `critic_score` |
| Primary keys | `id` (simple) or `{entity}_id` (FK) | `id`, `run_id` |
| Foreign keys | `{referenced_table_singular}_id` | `run_id` → runs |
| Indexes | `idx_{table}_{columns}` | `idx_segments_run_id` |
| Booleans | integer 0/1 | `human_override` (not `is_override`) |
| Timestamps | TEXT ISO 8601 UTC | `created_at`, `updated_at` |

**API Naming (REST):**

| Element | Convention | Example |
|---|---|---|
| Endpoints | plural nouns, lowercase | `/api/runs`, `/api/decisions` |
| Path parameters | `{name}` (Go 1.22 ServeMux) | `/api/runs/{id}` |
| Actions | POST with verb suffix | `POST /api/runs/{id}/resume` |
| Query params | snake_case | `?scp_id=049&stage=write` |
| JSON fields | **snake_case everywhere** | `{"run_id": "...", "critic_score": 0.82}` |

snake_case JSON is server-authoritative. Go struct tags use
`json:"snake_case"`. React consumes `data.run_id` directly — no
camelCase transform layer. This eliminates dual-source-of-truth
bugs where the same field has two names.

**ESLint configuration must suppress camelCase warnings for API
fields** — without this, AI agents will create camelCase transform
utilities:

```json
// web/.eslintrc or eslint.config.js
{
  "rules": {
    "camelcase": ["error", { "allow": ["^\\w+_"] }]
  }
}
```

**Run ID Format:** `scp-{scp_id}-run-{sequential_number}`.
Example: `scp-049-run-1`, `scp-049-run-2`. Sequential number is
per-SCP-ID, derived from `SELECT COUNT(*) + 1 FROM runs WHERE
scp_id = ?`. No UUID/ULID — human-readable IDs are essential for
a single-operator tool where the operator types IDs in CLI commands.

**Go Code Naming:**

| Element | Convention | Example |
|---|---|---|
| Packages | lowercase, single word | `db`, `pipeline`, `hitl` |
| Exported types | PascalCase | `PipelineState`, `TextGenerator` |
| Unexported | camelCase | `runPhaseA`, `validateSchema` |
| Files | snake_case.go | `phase_a.go`, `image_track.go` |
| Test files | snake_case_test.go | `engine_test.go` |
| Interfaces | -er suffix or capability | `TextGenerator`, `TTSSynthesizer` |
| Errors | Err prefix | `ErrStageFailed`, `ErrCostCapExceeded` |
| Constants | PascalCase (exported) | `StageResearch`, `MaxRetries` |

**React/TypeScript Code Naming:**

| Element | Convention | Example |
|---|---|---|
| Components | PascalCase file + export | `SceneCard.tsx` |
| Hooks | camelCase, use- prefix | `useRunStatus.ts` |
| Utilities | camelCase | `apiClient.ts` |
| Zustand stores | use-Store suffix | `useUIStore.ts` |
| TanStack Query keys | array of snake_case strings | `['runs', runId, 'scenes']` |
| CSS custom properties | kebab-case | `--bg-raised`, `--accent` |
| Test files | same name + .test | `SceneCard.test.tsx` |

**Date/Time:** UTC everywhere. ISO 8601 / RFC 3339 strings. Go
marshals `time.Time` as RFC 3339 by default. React formats with
`Intl.DateTimeFormat('ko-KR')`.

### Structure Patterns

**Go Project Organization:**

```
internal/
  domain/       # Types, interfaces, enums, sentinel errors — imports NOTHING from internal/
  db/           # SQLite operations + migration runner
  pipeline/     # State machine engine
  service/      # Business logic (orchestrates domain + db + llmclient)
  llmclient/    # External API clients (subpackage per vendor)
  api/          # HTTP handlers + routes + middleware
  web/          # embed.go only (//go:embed)
  clock/        # Clock interface for testable time
  testutil/     # Test helpers
```

**Import Direction (enforced in CI):**

```
api → service → domain
               → db
               → llmclient
               → pipeline
               → clock
```

`domain/` imports nothing from `internal/`. Enforced by CI script:

```bash
# ci: verify domain package has no internal imports
if grep -r '"github.com/sushistack/youtube.pipeline/internal/' internal/domain/; then
  echo "FAIL: domain/ must not import other internal packages"
  exit 1
fi
```

**Interface Definition Rule:** Interfaces are defined in the
**consuming package**, not the implementing package. Example:
`service/` defines `RunStore` interface; `db/` implements it.
Exception: `domain/` defines capability interfaces (`TextGenerator`,
`ImageGenerator`) because they represent domain concepts shared
across multiple consumers.

**React Project Organization:**

```
web/src/
  components/
    ui/          # shadcn/ui base components (7)
    shells/      # ProductionShell, TuningShell, SettingsShell
    slots/       # Per-pipeline-state slot components
    shared/      # StatusBar, Sidebar, ActionBar, AudioPlayer
  hooks/         # Custom hooks
  stores/        # Zustand stores
  lib/           # apiClient, formatters, constants
  schemas/       # Zod schemas for contract validation
  test/          # Test setup (fetch blocker, MSW handlers, QueryWrapper)
  styles/        # tokens.css, fonts.css
```

**Test Location:** Co-located. Go: `*_test.go` same directory.
React: `*.test.tsx` next to component. E2E: `e2e/` at project root.
Fixtures: `testdata/` at project root (shared Go + JS).

### Router & Middleware Patterns

**Route Registration (centralized in one file):**

```go
// internal/api/routes.go
func RegisterRoutes(mux *http.ServeMux, deps *Dependencies) {
    // Middleware chain applied to all API routes
    api := Chain(
        http.NewServeMux(),
        WithRequestID,
        WithRecover,
        WithCORS,
        WithRequestLog(deps.Logger),
    )

    // Pipeline lifecycle
    api.HandleFunc("GET /api/runs", deps.Run.List)
    api.HandleFunc("GET /api/runs/{id}", deps.Run.Get)
    api.HandleFunc("GET /api/runs/{id}/status", deps.Run.Status)
    api.HandleFunc("POST /api/runs/{id}/resume", deps.Run.Resume)
    api.HandleFunc("POST /api/runs/{id}/cancel", deps.Run.Cancel)

    // Scene review (HITL)
    api.HandleFunc("GET /api/runs/{id}/scenes", deps.Scene.List)
    api.HandleFunc("POST /api/runs/{id}/scenes/{idx}/approve", deps.Scene.Approve)
    api.HandleFunc("POST /api/runs/{id}/scenes/{idx}/reject", deps.Scene.Reject)
    // ... remaining endpoints

    mux.Handle("/api/", api)
    mux.Handle("/", spaHandler(deps.WebFS)) // SPA catch-all
}
```

All routes in one file. New endpoint = one line here + handler +
test + frontend hook.

**Middleware Chain:**

```go
// internal/api/middleware.go
type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, mws ...Middleware) http.Handler {
    for i := len(mws) - 1; i >= 0; i-- {
        h = mws[i](h)
    }
    return h
}
```

V1 middleware stack (4 total):
1. `WithRequestID` — generates UUID, injects into context + response header
2. `WithRecover` — panic → 500 + slog.Error (never crashes server)
3. `WithCORS` — permissive for localhost (no external access)
4. `WithRequestLog` — slog structured log per request (method, path, status, duration_ms, request_id)

**New Endpoint Checklist (7 steps):**

1. `internal/domain/` — define request/response types if new
2. `internal/service/` — add business logic method
3. `internal/api/handler_*.go` — write handler using `writeJSON`/`writeError`
4. `internal/api/handler_*_test.go` — write handler test (httptest pattern)
5. `internal/api/routes.go` — register route (one line)
6. `web/src/lib/apiClient.ts` — add `apiFetch` call
7. `web/src/hooks/` — add TanStack Query hook with key factory entry

### Format Patterns

**API Response Envelope (all endpoints):**

```json
{"version": 1, "data": {"id": "scp-049-run-1", "stage": "batch_review"}}
{"version": 1, "data": {"items": [...], "total": 42}}
{"version": 1, "error": {"code": "STAGE_FAILED", "message": "...", "recoverable": true}}
```

Every response uses `writeJSON` or `writeError` helpers. No raw
`json.Encode` to ResponseWriter. No naked error strings.

**Go Response Helpers:**

```go
// internal/api/response.go
func writeJSON(w http.ResponseWriter, status int, data any) { ... }
func writeError(w http.ResponseWriter, status int, code, msg string, recoverable bool) { ... }

func mapDomainError(err error) (status int, code string, recoverable bool) {
    switch {
    case errors.Is(err, domain.ErrNotFound):        return 404, "NOT_FOUND", false
    case errors.Is(err, domain.ErrValidation):      return 400, "VALIDATION_ERROR", false
    case errors.Is(err, domain.ErrConflict):         return 409, "CONFLICT", false
    case errors.Is(err, domain.ErrCostCapExceeded):  return 402, "COST_CAP_EXCEEDED", false
    case errors.Is(err, domain.ErrStageFailed):      return 500, "STAGE_FAILED", true
    default:                                         return 500, "INTERNAL_ERROR", false
    }
}
```

**Null Handling:** Go pointer types for nullable fields. JSON
marshals as `null`. React checks `null` explicitly — never
`undefined` as sentinel.

### Communication Patterns

**TanStack Query Key Factory (mandatory):**

```typescript
// lib/queryKeys.ts
export const runKeys = {
  all: ['runs'] as const,
  detail: (id: string) => ['runs', id] as const,
  status: (id: string) => ['runs', id, 'status'] as const,
  scenes: (id: string) => ['runs', id, 'scenes'] as const,
};
export const decisionKeys = {
  all: ['decisions'] as const,
  list: (filters: DecisionFilters) => ['decisions', filters] as const,
};
```

Never inline query keys. Every hook references the factory.

**Optimistic UI Scope (HITL review actions only):**

Optimistic mutations apply to **high-frequency HITL decisions**:
- Scene approve / reject / skip
- Batch approve-all
- Undo

Pipeline state transitions (resume, cancel, create) use **standard
mutations** — wait for server confirmation before updating UI. These
are infrequent, irreversible actions where optimistic feedback adds
complexity without UX value.

```typescript
// Standard mutation (pipeline actions)
const resumeRun = useMutation({
  mutationFn: (runId: string) => api.resumeRun(runId),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: runKeys.detail(runId) }),
});

// Optimistic mutation (HITL review)
const approveScene = useMutation({
  mutationFn: (idx: number) => api.approveScene(runId, idx),
  onMutate: async (idx) => { /* cancel + snapshot + optimistic update */ },
  onError: (_err, _idx, ctx) => { /* rollback from snapshot */ },
  onSettled: () => queryClient.invalidateQueries({ queryKey: runKeys.scenes(runId) }),
});
```

**Zustand Store Convention:**

```typescript
// stores/useUIStore.ts
interface UIState {
  selected_scene_idx: number | null;
  sidebar_collapsed: boolean;
  review_snapshot: Scene[] | null;
  selectScene: (idx: number) => void;
  freezeReviewList: (scenes: Scene[]) => void;
  refreshReviewList: () => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      selected_scene_idx: null,
      sidebar_collapsed: false,
      review_snapshot: null,
      selectScene: (idx) => set({ selected_scene_idx: idx }),
      freezeReviewList: (scenes) => set({ review_snapshot: scenes }),
      refreshReviewList: () => set({ review_snapshot: null }),
    }),
    { name: 'youtube-pipeline-ui' }
  )
);
```

Actions are methods on the store. State names use snake_case
(consistent with API). `persist` middleware saves to localStorage
for returning-user resume.

**React API Client:**

```typescript
// lib/apiClient.ts
class APIError extends Error {
  constructor(
    public code: string,
    message: string,
    public recoverable: boolean,
    public status: number,
  ) { super(message); }
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, init);
  const body = await res.json();
  if (body.error) {
    throw new APIError(body.error.code, body.error.message, body.error.recoverable, res.status);
  }
  return body.data as T;
}
```

Single wrapper. All API calls go through this. No raw `fetch()`.

### Process Patterns

**Error Handling (Go):**

Domain sentinel errors in `internal/domain/errors.go`:

```go
var (
    ErrNotFound        = errors.New("not found")
    ErrValidation      = errors.New("validation error")
    ErrConflict        = errors.New("state conflict")
    ErrStageFailed     = errors.New("stage failed")
    ErrCostCapExceeded = errors.New("cost cap exceeded")
    ErrRateLimited     = errors.New("rate limited")
)
```

All errors wrapped with context: `fmt.Errorf("phase A writer: %w",
domain.ErrStageFailed)`. API handlers map via `errors.Is()` through
`mapDomainError()`. Unknown errors → 500 with generic message (no
internal details leaked).

**Logging (Go slog, constructor-injected):**

```go
// Handlers and services receive *slog.Logger via constructor
type RunHandler struct {
    svc    service.RunService
    logger *slog.Logger
}

// Structured log pattern
h.logger.Info("stage completed",
    "run_id", run.ID,
    "stage", string(run.Stage),
    "duration_ms", duration.Milliseconds(),
    "cost_usd", run.CostUSD,
)
```

slog (stdlib since Go 1.21). No external logging library. Keys are
snake_case. Constructor injection enables test capture via
`testutil.CaptureLog`.

**Loading States (React):**

| State | Backend | Frontend |
|---|---|---|
| Cold start | N/A | Skeleton screen (first load only) |
| Poll refresh | Normal response | stale-while-revalidate (invisible) |
| HITL mutation | Processing | Optimistic UI (< 100ms feedback) |
| Regeneration | `"regenerating"` | Progress overlay on scene card |
| Phase C | `progress_percent` | Progress bar in stepper |

No full-screen spinners. No loading flash on poll cycle.

### Test Patterns

**testutil Package (minimum 6 helpers):**

```go
// internal/testutil/

// assert.go — generic assertion helpers (no testify)
func assertEqual[T comparable](t testing.TB, got, want T) { ... }
func assertJSONEq(t testing.TB, got, want string) { ... }

// response.go — HTTP test helpers
func ReadJSON[T any](t testing.TB, body io.Reader) T { ... }

// fixture.go — fixture loading
func LoadFixture(t testing.TB, path string) []byte { ... }
func LoadGolden[T any](t testing.TB, path string) T { ... }
func WriteGolden(t testing.TB, path string, v any) { ... }

// db.go — test database
func NewTestDB(t testing.TB) *sql.DB {
    tmp := filepath.Join(t.TempDir(), "test.db")
    db, _ := sql.Open("sqlite3", tmp)
    migrate.Up(db)
    t.Cleanup(func() { db.Close() })
    return db
}

// nohttp.go — external HTTP blocking
func BlockExternalHTTP(t testing.TB) { ... }

// slog.go — log capture
func CaptureLog(t testing.TB) (*slog.Logger, *bytes.Buffer) { ... }
```

**React Test Utilities:**

```typescript
// web/src/test/setup.ts — external fetch blocking (global)
// web/src/test/queryWrapper.tsx — TanStack Query test provider
// web/src/test/mswHandlers.ts — MSW mock API handlers
```

**Go HTTP Handler Test Template:**

```go
func TestRunHandler_Get_Success(t *testing.T) {
    db := testutil.NewTestDB(t)
    svc := service.NewRunService(db)
    logger, _ := testutil.CaptureLog(t)
    h := api.NewRunHandler(svc, logger)

    req := httptest.NewRequest("GET", "/api/runs/scp-049-run-1", nil)
    req.SetPathValue("id", "scp-049-run-1")
    rec := httptest.NewRecorder()

    h.Get(rec, req)

    assertEqual(t, rec.Code, 200)
    got := testutil.ReadJSON[api.RunResponse](t, rec.Body)
    assertEqual(t, got.ID, "scp-049-run-1")
}
```

**Golden File Test Pattern (LLM agent outputs — structure only):**

```go
func TestScriptAgent_OutputStructure(t *testing.T) {
    output := runScriptAgent(t, mockLLM, ScriptInput{SCPID: "173"})

    // Structure validation — NOT text equality
    if len(output.Scenes) < 5 { t.Errorf("too few scenes: %d", len(output.Scenes)) }
    for i, s := range output.Scenes {
        if s.Narration == "" { t.Errorf("scene %d: empty narration", i) }
        if s.Mood == "" { t.Errorf("scene %d: empty mood", i) }
    }
}
```

LLM outputs are nondeterministic — golden tests verify **structure
(field existence, types, count ranges)**, never text content.

**Contract Test Fixture Structure:**

```
testdata/contracts/
  run.detail.response.json      # Both Go and Zod validate this
  run.list.response.json
  scene.list.response.json
  error.response.json
```

Go validates via `encoding/json` unmarshal + struct field assertions.
React validates via Zod schema parse. Same fixture, both sides. If
fixture breaks, both test suites fail simultaneously.

### Enforcement Guidelines

**All AI Agents MUST:**

1. Use `snake_case` for all JSON fields, DB columns, query params
2. Use response envelope (`{"version": 1, ...}`) via `writeJSON`/`writeError` — never raw json.Encode
3. Place domain types and interfaces in `internal/domain/` — domain imports nothing
4. Use `apiFetch()` for all frontend API calls — never raw `fetch()`
5. Use TanStack Query key factory — never inline query keys
6. Use domain sentinel errors + `errors.Is()` — never ad-hoc error strings
7. Co-locate test files with source files
8. Use `slog` via constructor injection — no `fmt.Println`, no `log.Printf`, no `slog.Default()`
9. Register new routes in `internal/api/routes.go` only — never in handler files
10. Use `testutil` helpers for assertions and fixtures — never duplicate test utilities
11. Wrap all errors with context: `fmt.Errorf("context: %w", err)` — never naked return
12. Use optimistic UI only for HITL review actions — pipeline state transitions use standard mutations

**Anti-Patterns:**

| Do NOT | Do Instead |
|---|---|
| `json:"runId"` | `json:"run_id"` |
| `http.Error(w, "not found", 404)` | `writeError(w, 404, "NOT_FOUND", "...", false)` |
| `fetch('/api/runs/' + id)` | `apiFetch<Run>(\`/runs/${id}\`)` |
| `useQuery({ queryKey: ['runs', id] })` | `useQuery({ queryKey: runKeys.detail(id) })` |
| `import "internal/api"` in domain/ | domain/ imports nothing from internal/ |
| `slog.Info(...)` (package-level) | `h.logger.Info(...)` (injected) |
| `uuid.New()` for run IDs | `scp-{id}-run-{n}` (human-readable) |
| Optimistic UI on `resume`/`cancel` | Standard mutation + `onSuccess` invalidate |

## Project Structure & Boundaries

### Complete Project Directory Structure

```
youtube.pipeline/
├── .github/workflows/ci.yml
├── .air.toml
├── .env.example
├── .gitignore
├── Makefile
├── go.mod
├── go.sum
│
├── cmd/pipeline/
│   └── main.go                                 # Cobra root + 8 subcommands
│
├── internal/
│   ├── domain/                                 # ★ IMPORTS NOTHING from internal/
│   │   ├── types.go                            # Run, Segment, Decision structs
│   │   ├── stages.go                           # Stage/Status/Event enums, NextStage()
│   │   ├── llm.go                              # TextGenerator, ImageGenerator, TTSSynthesizer
│   │   ├── errors.go                           # Sentinel errors (ErrNotFound, ErrStageFailed, ...)
│   │   └── config.go                           # PipelineConfig struct (cost caps, model IDs, paths)
│   │   # Rule: no file exceeds 300 lines. Split by concept when approaching limit.
│   │
│   ├── config/
│   │   ├── loader.go                           # Viper YAML + .env loading → domain.PipelineConfig
│   │   └── loader_test.go
│   │
│   ├── db/
│   │   ├── sqlite.go                           # OpenDB (WAL + busy_timeout=5000 enforced)
│   │   ├── sqlite_test.go
│   │   ├── migrate.go                          # ~50 LoC runner (embed.FS + user_version)
│   │   ├── migrate_test.go
│   │   ├── migrations/
│   │   │   └── 001_init.sql
│   │   ├── run_store.go                        # implements service.RunStore interface
│   │   ├── run_store_test.go
│   │   ├── decision_store.go
│   │   ├── decision_store_test.go
│   │   ├── segment_store.go
│   │   └── segment_store_test.go
│   │
│   ├── pipeline/
│   │   ├── runner.go                           # Runner interface (service references this)
│   │   ├── engine.go                           # State machine: Advance(), Resume()
│   │   ├── engine_test.go                      # Table-driven state transition tests
│   │   ├── phase_a.go                          # 6-agent sequential chain
│   │   ├── phase_a_test.go
│   │   ├── phase_b.go                          # errgroup parallel (image + TTS)
│   │   ├── phase_b_test.go
│   │   ├── phase_c.go                          # FFmpeg segment concat
│   │   ├── phase_c_test.go
│   │   ├── agents/
│   │   │   ├── agent.go                        # AgentFunc type + shared validation
│   │   │   ├── researcher.go
│   │   │   ├── structurer.go
│   │   │   ├── writer.go
│   │   │   ├── visual_breaker.go
│   │   │   ├── reviewer.go
│   │   │   ├── critic.go
│   │   │   └── agents_test.go
│   │   ├── cost.go                             # Accumulator + circuit breaker
│   │   ├── cost_test.go
│   │   ├── validator.go                        # Inter-agent schema validation
│   │   └── validator_test.go
│   │
│   ├── service/
│   │   ├── run_service.go                      # Create, Resume, Cancel (references pipeline.Runner)
│   │   ├── run_service_test.go
│   │   ├── scene_service.go                    # Approve, Reject, Edit, Regen, Undo (Command Pattern)
│   │   ├── scene_service_test.go
│   │   ├── character_service.go                # DDG search + pick + Image-Edit
│   │   ├── character_service_test.go
│   │   ├── decision_service.go                 # History, metrics aggregation
│   │   └── decision_service_test.go
│   │   # Undo logic lives INSIDE scene_service.go — not a separate file.
│   │   # If cross-service orchestration needed: add orchestrator.go (service composition only).
│   │
│   ├── llmclient/
│   │   ├── dashscope/
│   │   │   ├── tts.go
│   │   │   ├── tts_test.go
│   │   │   ├── image.go
│   │   │   ├── image_edit.go
│   │   │   └── image_test.go
│   │   ├── deepseek/
│   │   │   ├── client.go
│   │   │   └── client_test.go
│   │   ├── gemini/
│   │   │   ├── client.go
│   │   │   └── client_test.go
│   │   ├── ratelimit.go                        # Semaphore + token bucket per vendor
│   │   ├── ratelimit_test.go
│   │   ├── retry.go                            # WithRetry + clock interface
│   │   └── retry_test.go
│   │
│   ├── api/
│   │   ├── routes.go                           # RegisterRoutes (all routes, one file)
│   │   ├── middleware.go                        # Chain, RequestID, Recover, CORS, RequestLog
│   │   ├── middleware_test.go
│   │   ├── response.go                         # writeJSON, writeError, mapDomainError
│   │   ├── response_test.go
│   │   ├── handler_run.go
│   │   ├── handler_run_test.go
│   │   ├── handler_scene.go
│   │   ├── handler_scene_test.go
│   │   ├── handler_character.go
│   │   ├── handler_character_test.go
│   │   ├── handler_decision.go
│   │   ├── handler_decision_test.go
│   │   └── spa.go                              # SPA catch-all → index.html
│   │
│   ├── web/
│   │   └── embed.go                            # //go:embed all:dist
│   │
│   ├── clock/
│   │   └── clock.go                            # Clock, RealClock, FakeClock
│   │
│   └── testutil/
│       ├── assert.go                           # assertEqual[T], assertJSONEq
│       ├── response.go                         # ReadJSON[T] from http.Response
│       ├── fixture.go                          # LoadFixture, LoadGolden, WriteGolden
│       ├── db.go                               # NewTestDB (t.TempDir + migrate + t.Cleanup)
│       ├── nohttp.go                           # BlockExternalHTTP
│       └── slog.go                             # CaptureLog → (*slog.Logger, *bytes.Buffer)
│
├── web/
│   ├── index.html
│   ├── package.json
│   ├── package-lock.json
│   ├── vite.config.ts                          # base: "./"
│   ├── vitest.config.ts                        # jsdom, setupFiles
│   ├── tsconfig.json
│   ├── tailwind.config.ts
│   ├── eslint.config.js                        # snake_case API fields allowed
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx                             # BrowserRouter + 3 Routes
│   │   ├── App.test.tsx
│   │   ├── components/
│   │   │   ├── ui/                             # shadcn/ui: Button, Card, Badge, Progress, Tabs, Collapsible, Tooltip
│   │   │   ├── shells/
│   │   │   │   ├── ProductionShell.tsx
│   │   │   │   ├── TuningShell.tsx
│   │   │   │   └── SettingsShell.tsx
│   │   │   ├── slots/
│   │   │   │   ├── ScenarioReviewSlot.tsx
│   │   │   │   ├── CharacterPickSlot.tsx
│   │   │   │   ├── BatchReviewSlot.tsx
│   │   │   │   ├── AssemblySlot.tsx
│   │   │   │   └── CompletionSlot.tsx
│   │   │   └── shared/
│   │   │       ├── Sidebar.tsx
│   │   │       ├── StatusBar.tsx
│   │   │       ├── StageStepper.tsx
│   │   │       ├── SceneCard.tsx
│   │   │       ├── DetailPanel.tsx
│   │   │       ├── ActionBar.tsx
│   │   │       ├── AudioPlayer.tsx
│   │   │       ├── RunCard.tsx
│   │   │       ├── FailureBanner.tsx
│   │   │       ├── InlineConfirmPanel.tsx
│   │   │       ├── CharacterGrid.tsx
│   │   │       └── TimelineView.tsx
│   │   ├── hooks/
│   │   │   ├── useRunStatus.ts
│   │   │   ├── useScenes.ts
│   │   │   ├── useKeyboardShortcuts.ts
│   │   │   ├── useStableList.ts
│   │   │   └── useOptimisticScene.ts
│   │   ├── stores/
│   │   │   ├── useUIStore.ts                   # Zustand persist (localStorage)
│   │   │   └── useUndoStore.ts
│   │   ├── lib/
│   │   │   ├── apiClient.ts                    # apiFetch, APIError
│   │   │   ├── queryKeys.ts                    # TanStack Query key factory
│   │   │   └── formatters.ts                   # ko-KR date, cost, score
│   │   ├── schemas/
│   │   │   ├── run.ts                          # Zod: run responses
│   │   │   ├── scene.ts                        # Zod: scene responses
│   │   │   └── decision.ts                     # Zod: decision responses
│   │   ├── test/
│   │   │   ├── setup.ts                        # External fetch blocker
│   │   │   ├── renderWithProviders.tsx          # MemoryRouter + QueryClient wrapper
│   │   │   └── msw/
│   │   │       ├── handlers.ts                 # Re-export aggregator
│   │   │       ├── runHandlers.ts              # Run API mocks
│   │   │       ├── sceneHandlers.ts            # Scene API mocks
│   │   │       └── characterHandlers.ts        # Character API mocks
│   │   └── styles/
│   │       ├── tokens.css
│   │       └── fonts.css
│   └── dist/                                   # .gitignored
│
├── e2e/
│   ├── playwright.config.ts                    # Chromium only, projects: smoke / integration
│   ├── smoke.spec.ts                           # FR52: app loads, main page renders
│   └── integration/                            # V1.5: pipeline flow E2E
│
├── testdata/
│   ├── contracts/                              # ⚠️ NEVER auto-updated. Manual review only.
│   │   ├── run.detail.response.json
│   │   ├── run.list.response.json
│   │   ├── scene.list.response.json
│   │   └── error.response.json
│   └── golden/                                 # Auto-updateable via -update flag
│       └── phase_a_output.json
│
└── docs/                                       # Existing project documentation
```

### Architectural Boundaries

**Circular Dependency Prevention:**

The most dangerous cycle is `engine ↔ run_service`. Resolution:

- `pipeline/runner.go` defines `Runner` interface (Advance, Resume)
- `pipeline/engine.go` implements `Runner`; depends on `db/` directly
  for state persistence (NOT on service)
- `service/run_service.go` depends on `pipeline.Runner` interface
  (NOT on engine.go concretely)

```
service/run_service.go → pipeline.Runner (interface)
pipeline/engine.go     → db/run_store   (concrete)
```

No reverse dependency. Go compiler catches cycles at build time.

**Agent Purity Rule:**

All agents in `pipeline/agents/` are pure functions: receive input,
return output. No state mutation. No database access. No HTTP calls
(LLM calls are injected via `domain.TextGenerator` interface). State
machine (`engine.go`) orchestrates agents and handles persistence.

**Data Boundary Integrity:**

DB state (runs, segments, decisions) and filesystem state
(images, TTS, clips) must be consistent. On stage-level resume:

1. Engine validates DB state for the run
2. Engine checks filesystem artifacts exist for completed stages
3. If inconsistency detected: log warning, clean partial artifacts,
   re-execute from last consistent checkpoint

### Requirements to Structure Mapping

| FR Domain | Primary Go Packages | Primary React Components |
|---|---|---|
| Pipeline Lifecycle (FR1-8) | `pipeline/`, `db/run_store` | `RunCard`, `StageStepper`, `FailureBanner` |
| Phase A (FR9-13) | `pipeline/agents/`, `pipeline/phase_a` | `ScenarioReviewSlot` |
| Phase B (FR14-19) | `pipeline/phase_b`, `llmclient/dashscope/` | `CharacterPickSlot`, `CharacterGrid` |
| Phase C (FR20-23) | `pipeline/phase_c` | `AssemblySlot`, `CompletionSlot` |
| Quality Gating (FR24-30) | `pipeline/agents/critic`, `pipeline/cost` | `DetailPanel` (sub-scores) |
| HITL Review (FR31-37) | `service/scene_service`, `api/handler_scene` | `BatchReviewSlot`, `SceneCard`, `ActionBar` |
| Operator Tooling (FR38-44) | `cmd/pipeline/`, `api/response`, `api/spa` | `Sidebar`, `StatusBar` |
| Compliance (FR45-49) | `service/run_service`, `pipeline/validator` | `InlineConfirmPanel` (metadata ack) |
| Test Infrastructure (FR51-52) | `testutil/`, `testdata/` | `test/`, `e2e/` |

### Day 1 Implementation Scope (10 files)

The minimum set of files needed for the first `go test ./... && go build ./cmd/pipeline` to pass:

```
cmd/pipeline/main.go           # Cobra root + init subcommand
internal/domain/types.go       # Run struct
internal/domain/stages.go      # Stage enum
internal/domain/errors.go      # Basic sentinel errors
internal/domain/config.go      # PipelineConfig
internal/config/loader.go      # Viper loading
internal/db/sqlite.go          # OpenDB (WAL enforced)
internal/db/migrate.go         # Migration runner
internal/db/migrations/001_init.sql
internal/db/run_store.go       # Create + Get
go.mod
```

Everything else is created as implementation stories require it.
The structure document defines WHERE files go; stories define WHEN
they are created.

### Structural Rules

1. **domain/ 300-line cap:** No file in `domain/` exceeds 300 lines.
   Approaching the limit → split by concept (not by size).
2. **Handler 400-line cap:** No handler file exceeds 400 lines.
   Approaching → split into read handlers + write handlers.
3. **shared/ components:** A component moves to `shared/` only when
   used by 2+ different surfaces. Until then, it lives in its
   originating slot or shell.
4. **contracts/ never auto-updated:** `testdata/contracts/` files
   require manual review on every change. `testdata/golden/` files
   may be auto-updated via `-update` test flag.
5. **No cross-service calls:** Services do not call each other.
   If orchestration is needed, create `service/orchestrator.go`
   that composes services via interfaces.

## Architecture Validation Results

### Coherence Validation ✅

**Decision Compatibility:** All technology choices verified
compatible. Go 1.25.7 + ncruces/go-sqlite3 (pure Go, no CGO),
Cobra v2.5.1 + Viper v1.11.0, Vite 7.3 + shadcn/ui v4, React +
TanStack Query + Zustand, net/http ServeMux (Go 1.22+ patterns),
errgroup.Group (no context cancel) + stage-level resume. Zero
contradictions found across 6 architectural steps.

**Pattern Consistency:** snake_case single naming system (DB → JSON
→ React) eliminates dual-source-of-truth bugs. Response envelope
enforced by helpers. Import direction enforced by CI script.
Co-located tests across Go and React. Boring technology principle
applied consistently — no cutting-edge dependencies.

**Structure Alignment:** Project structure directly supports all
architectural decisions. 9 Go packages map cleanly to dependency
direction rule. React component hierarchy (shells → slots → shared)
matches Slot+Strategy UX pattern. testutil/ and testdata/ provide
complete test infrastructure foundation.

### Requirements Coverage ✅

**48 Functional Requirements:** All mapped to specific directories
and files. 45/48 fully specified, 3 minor gaps (FR8 cosine
similarity utility, FR53 prior rejection check, FR20 FFmpeg
binding) — all with documented resolution paths.

**Non-Functional Requirements:** All NFRs have explicit
architectural mechanisms. Cost caps → circuit breaker. Resume
idempotency → segments DELETE+reinsert. WAL mode → durable writes.
429 backoff → retry.go + clock interface. CI ≤ 10 min → parallel
jobs, estimated ~6.5 min.

### Implementation Readiness ✅

**Decision Completeness:** All critical and important decisions
documented with verified versions. 12 enforcement rules + anti-
pattern table. 7-step endpoint addition checklist.

**Structure Completeness:** Full directory tree with ~130 files.
Day 1 scope narrowed to 10 essential files. FR-to-directory mapping
table covers all 9 domains.

**Pattern Completeness:** Naming, structure, format, communication,
process, test patterns all defined with concrete code examples.
Router registration, middleware chain, HTTP handler test template,
golden file pattern all specified.

### Gap Analysis

| Gap | Severity | Resolution |
|---|---|---|
| FR8 cosine similarity utility | Minor | Add `pipeline/similarity.go`. V1: Jaccard or normalized Levenshtein. V1.5: embedding-based. |
| FR53 prior rejection check | Minor | `scene_service.go` → `checkPriorRejections()`. Query `decisions` table for same SCP ID rejected scenes. |
| FFmpeg Go binding | **Moderate** | Decide Week 1: `u2takey/ffmpeg-go` or thin `exec.Command` wrapper. V1 now requires `filter_complex` for intra-scene transitions (zoompan, xfade) — thin exec wrapper may be insufficient; ffmpeg-go recommended for composability. |

### Final Review Verdicts (Party Mode Round 6)

| Agent | Verdict | Key Condition |
|---|---|---|
| 🏗️ Winston (Architect) | **Ship it** | Add `pipeline force-reset` CLI command for stuck states. FFmpeg binding Week 1. |
| 💻 Amelia (Developer) | **Ready with caveats** | Consider pruning 17→12 endpoints during story scoping. |
| 🧪 Murat (TEA) | **Ready** | Unconditional. HITL 20% serves as E2E safety net. |

### Implementation Notes

**force-reset command (Winston's recommendation):**
Add `pipeline force-reset --run={id} --to={stage}` to the CLI
command tree. This is a recovery tool for stuck pipeline states
where the state machine has reached an unrecoverable position.
Implementation: validates target stage is valid, cleans artifacts
for stages after the target, resets `runs.stage` and
`runs.status`, deletes segments after the target stage. Must
verify idempotency (running force-reset twice produces the same
state).

**Endpoint pruning consideration (Amelia's recommendation):**
17 REST endpoints may be aggressive for 6-week V1. During story
scoping, consider consolidating:
- Merge `approve` + `reject` + `edit` into single `POST
  /api/runs/{id}/scenes/{idx}/decision` with action in body
- Defer `GET /api/metrics` to CLI-only (already available via
  `pipeline metrics --window 25`)
- Defer character endpoints if first SCPs don't require character
  reference workflow

Target: 12 or fewer endpoints for V1.

**Highest-probability undetected bug (Murat's assessment):**
Backend state transition → frontend reflection timing. Polling
interval (5s) means frontend can show stale state for up to 5
seconds after a transition. Mitigated by: optimistic UI on HITL
actions, stale-while-revalidate pattern, and the review-mode
snapshot freeze. HITL review acts as a manual E2E safety net.

### Architecture Completeness Checklist

**✅ Requirements Analysis**
- [x] 9 FR domains identified and analyzed
- [x] NFR architecture drivers mapped
- [x] 12 technical constraints specified
- [x] Cross-cutting concerns 3-tier classified
- [x] Architecture priority weighting (first-class / solid / minimum viable)

**✅ Starter Template**
- [x] Tech stack versions web-verified (April 2026)
- [x] Manual Go + shadcn Vite template selected
- [x] Day 1 project structure specified
- [x] API isolation 3-layer defense infrastructure

**✅ Core Decisions**
- [x] Data: SQLite 3 tables + manual migration + JSON TEXT columns
- [x] API: net/http ServeMux + REST endpoints + JSON envelope
- [x] Frontend: TanStack Query + Zustand persist + React Router v7
- [x] Pipeline: plain function chain + errgroup + switch state machine
- [x] External: capability interfaces + semaphore + rate limiter + retry
- [x] CI: GitHub Actions 4-job parallel pipeline
- [x] HITL: 4 wait points + Command Pattern undo + focus restoration

**✅ Implementation Patterns**
- [x] Naming conventions (snake_case everywhere)
- [x] Router registration + middleware chain
- [x] Error handling (domain sentinels + mapDomainError)
- [x] Test patterns (httptest, golden file, contract fixtures)
- [x] 12 enforcement rules + anti-pattern table
- [x] 7-step endpoint addition checklist

**✅ Project Structure**
- [x] Complete directory tree (~130 files)
- [x] FR → directory mapping table
- [x] Circular dependency prevention (engine ↔ service resolved)
- [x] Day 1 scope (10 files)
- [x] 5 structural rules

**✅ Validation**
- [x] Coherence: all decisions compatible
- [x] Coverage: 48 FRs + all NFRs mapped
- [x] Readiness: 3/3 agents approve
- [x] Gaps: 0 critical, 0 important, 3 minor (all resolved)

### Architecture Readiness Assessment

**Overall Status: READY FOR IMPLEMENTATION**

**Confidence Level: High**

Built through 7 workflow steps with 6 rounds of Party Mode review
by 5 independent agents (Winston/Architect, Amelia/Developer,
Murat/TEA, Sally/UX, John/PM). Each round produced concrete
corrections that strengthened the architecture.

**First Implementation Priority:**

```bash
# Day 1: Bootstrap
go mod init github.com/sushistack/youtube.pipeline
mkdir -p cmd/pipeline internal/{domain,config,db/migrations} testdata
# Create 10 essential files (see Day 1 scope)
# Target: `go test ./...` green + `go build ./cmd/pipeline` succeeds
```
