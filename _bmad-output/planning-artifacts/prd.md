---
stepsCompleted:
  - step-01-init
  - step-02-discovery
  - step-02b-vision
  - step-02c-executive-summary
  - step-03-success
  - step-04-journeys
  - step-05-domain
  - step-06-innovation-skipped
  - step-07-project-type
  - step-08-scoping
  - step-09-functional
  - step-10-nonfunctional
  - step-11-polish
  - step-12-complete
completedAt: 2026-04-13
vision:
  statement: "혼자 운영하는 SCP 유튜버가 SCP ID 하나만 입력하면 프로급 호러 영상이 나오는, 장기 운영 가능한 개인 프로덕션 파이프라인. 80% 자동화 + 20% HITL."
  targetUser: "Jay 개인 전용 (확장 가능성 열어둠)"
  hiddenMotivation: "AI-era career hedge — 장기 운영 가능한 프로덕션 도구로 설계, 야매 접근 거부"
  whyNow: "Qwen3-TTS (2025-11) + Qwen-Image-Edit로 한국어 solo creator 스택이 처음으로 viable"
  differentiators:
    - name: "SCP 장르 전용 프롬프트 튜닝"
      detail: "Incident-first 4-act, Hook library (Q/S/M/C), KR 호러 유튜버 voice hybrid (살리의 방 + TheVolgun + TheRubber)"
      load_bearing: true
    - name: "시각 일관성 (Frozen Descriptor)"
      detail: "Qwen-Image-Edit 레퍼런스 전파로 씬 간 캐릭터/개체 일관성"
      load_bearing: true
    - name: "품질 게이트 (LLM-as-judge critic retry loop)"
      detail: "Option D (정석) — Writer≠Critic 교차 모델 + Golden eval set 20개 + rubric sub-scores + regression harness + human-LLM calibration + cost budget cap"
      load_bearing: true
    - name: "파이프라인 + 상태머신 설계 rigor"
      detail: "V1: SQLite runs 테이블 + stage/status/retry_count + Go switch + resume 명령어 (~100 LoC). V2: auto retry policy, transition validation, partial rollback. Bright line: '전이 다이어그램 그리기 시작하면 과잉 설계'"
      load_bearing: true
  coreInsight: "LLM들을 그냥 엮으면 위키 요약. 장르-특화 프롬프트 체인 + 비주얼 앵커링 + 검증된 품질 게이트 + 견고한 상태머신 + UX 1급 HITL을 조합하면 프로 채널 품질에 근접. LLM 능력 문제가 아니라 도메인 지식 + 워크플로우 엔지니어링을 아키텍처로 번역하는 문제."
  hitlDesign:
    model: "Sally 3-tier"
    tiers:
      - "자동 승인 (critic score 기반 고신뢰)"
      - "배치 리뷰 (전체 씬 한 화면, 예외만 터치)"
      - "정밀 리뷰 (캐릭터 첫 등장, 핵심 씬)"
    principle: "20%가 '가장 반복적인 20%'가 아닌 '가장 창의적인 20%'가 되도록"
  qualityPillars:  # Option D full stack (Step 4 Party Mode 후 강화)
    - "Writer ≠ Critic cross-model (e.g., DeepSeek writer × Gemini critic)"
    - "Golden eval set ≥ 20 cases (Day 1 수작업 구축, format_guide.md 기반); positive:negative 비율 1:1 유지"
    - "Rule-based pre-checks (JSON schema + forbidden keywords regex)"
    - "Rubric-based LLM judge (hook_strength / fact_accuracy / emotional_variation / immersion weighted)"
    - "Regression harness (go test ./critic -run Golden, 검출률 ≥ 80%)"
    - "Shadow-eval (go test ./critic -run Shadow): Critic prompt 변경 시 최근 합격 씬 10개 dry-run, 오탈락 1건도 PR 블로킹"
    - "Human-LLM calibration tracking (Jay override 기록 → Cohen's kappa ≥ 0.7); rolling 25-run window, n<25는 'provisional' 라벨"
    - "Anti-progress detection (V1 승격): retry 간 출력 cosine similarity > 0.92면 early-stop → 인간 에스컬레이션"
    - "Observability Day 1 (pipeline_runs: stage, duration_ms, token_in/out, retry_count, retry_reason, critic_score, cost_usd, human_override) — 8 컬럼"
    - "Stage별 cost budget cap (hard stop → human)"
    - "decisions 테이블 schema hook: Jay의 모든 HITL 결정 이력 기록 (V1엔 기록만, learning은 V1.5)"
  successMetric:
    automationTarget: "(자동 완료 stage / 전체 stage) 최근 10개 run 평균 ≥ 80% @ Day 90"
    businessMetric: "단일 KPI 아님 — 조회수, 유지율, 구독 반응, Jay 만족도 종합 판단 (분기별 리뷰)"
  namedRisks:
    - "SCP Foundation CC BY-SA 3.0 — 상업적 파생 공개 의무 회색지대"
    - "YouTube AI 생성 콘텐츠 라벨링/수익화 정책 (2024+)"
    - "첫 1000명 구독자 획득 전략 PRD에 없음"
    - "DashScope 단일 벤더 의존 — 인터페이스 분리, 구현체는 DashScope 하나"
    - "FFmpeg 어셈블리 단계 과소평가 위험"
    - "Critic retry loop 비용 폭발 위험 (cost cap으로 완화)"
partyModeInsights:
  agents: ["John (PM)", "Winston (Architect)", "Sally (UX)", "Mary (Analyst)", "Murat (TEA)"]
  rounds: 2
  outcome: "Differentiator #4 (state machine) V1/V2 bright line 확정, Differentiator #3 (critic) Option D 정석 경로 확정, HITL 3-tier 채택, 6개 리스크 명명, Option D quality pillars 8종 전부 V1 스코프"
classification:
  projectType: cli_tool+web_app
  projectTypeNote: "hybrid — Go CLI pipeline engine + local web UI for HITL/monitoring"
  domain: general
  domainNote: "AI media content automation / YouTube production pipeline (SCP vertical)"
  complexity: medium-high
  projectContext: greenfield
inputDocuments:
  - docs/prd.md
  - docs/analysis/scp.yt.channels.analysis.md
  - docs/images/image.gen.policy.md
  - docs/tts/qwen3.tts.vc.md
  - docs/prompts/scenario/01_research.md
  - docs/prompts/scenario/02_structure.md
  - docs/prompts/scenario/03_writing.md
  - docs/prompts/scenario/03_5_visual_breakdown.md
  - docs/prompts/scenario/04_review.md
  - docs/prompts/scenario/critic_agent.md
  - docs/prompts/scenario/format_guide.md
  - docs/prompts/image/01_shot_breakdown.md
  - docs/prompts/image/02_shot_to_prompt.md
  - docs/prompts/tts/scenario_refine.md
  - docs/prompts/vision/descriptor_enrichment.md
  - docs/ui.examples/dashboard.png
  - docs/ui.examples/pipe.png
  - docs/ui.examples/scenes.png
documentCounts:
  briefs: 0
  research: 0
  brainstorming: 0
  projectDocs: 18
workflowType: 'prd'
---

# Product Requirements Document - youtube.pipeline

**Author:** Jay
**Date:** 2026-04-13

## Executive Summary

- **Product**: `youtube.pipeline` — a solo-operator production system
  that turns an SCP Foundation article ID into a publishable
  Korean-language horror YouTube video.
- **Automation split**: ~80% automated pipeline, ~20% reserved for
  first-class human-in-the-loop (HITL) review.
- **Primary user**: a single creator, producing their own Korean SCP
  horror channel.
- **Why now**: Qwen3-TTS (Nov 2025) + Qwen-Image-Edit + low-cost
  multi-LLM access make a Korean solo-creator stack first-time viable.
- **Positioning**: Top SCP YouTube channels (TheRubber 3.5M+, 三好屋
  900K+) publish 1–2 videos/week via manual production pipelines. The
  differentiation thesis: an AI-automated pipeline lets a solo
  operator match or exceed that publication cadence while preserving
  professional quality.
- **MVP boundary**: First publishable video end-to-end from the
  pipeline. Target: first-video-shipped within ~6 weeks of V1 kickoff
  (see §Risk Register for the week-by-week allocation). V1.5
  extensions (auto-retry policies, transition validation, partial
  rollback) are gated on five videos shipped through V1.
- **Goal framing**: Extend one creator's production leverage with AI.
  This is not generic AI video generation — the hard problem is
  translating a narrow genre's craft (incident-first pacing, hook
  libraries, visual consistency, horror-YouTuber voice) into a
  pipeline architecture that preserves those properties across
  nondeterministic LLM stages while keeping solo-operator cognitive
  load sustainable.
- **Risks**: 7 material risks identified (content licensing, platform
  policy, audience acquisition, vendor lock-in, assembly automation,
  cost amplification, quality-floor). Details → §Risk Register.

### What Makes This Special

Four load-bearing differentiators. Removing any one is predicted to
make the product fail:

1. **SCP genre-specific prompt chain** — the only viewer-facing
   differentiator. Incident-first 4-act structure, Hook type library,
   Korean horror-YouTuber voice hybrid (살리의 방 + TheVolgun +
   TheRubber), forbidden-term enforcement at each stage.

2. **Visual consistency via Frozen Descriptor + Qwen-Image-Edit** —
   eliminates the standard AI-video failure mode where entities drift
   between shots and scenes, by generating a verbatim-reused visual
   descriptor and propagating it through reference-based image
   generation across all shots in all scenes.

3. **Validated quality gate** — an industry-standard LLM-as-judge
   evaluation framework (cross-model writer/critic, hand-built golden
   eval set, rubric sub-scores, regression harness, human–LLM
   calibration, per-stage cost caps). Shortcutting this ("ship first,
   validate later") is explicitly rejected. Numeric targets and
   acceptance thresholds → §Success Criteria.

4. **Bounded pipeline + state machine rigor** — V1 is deliberately
   minimal (status tracking + resumable transitions on SQLite); V2
   extensions are feature-gated on production experience.
   Over-engineering is bounded by an explicit bright line
   (no transition diagrams in V1). Implementation detail →
   §Architecture.

HITL is treated as a first-class UX surface, not a fallback. A
three-tier review model operates across every project: auto-approve
on high-confidence outputs, batch review for bulk scene screens, and
precision review for high-leverage moments (first character
appearance, hook scenes, act-boundary scenes). Goal: the 20% manual
contribution is the *most creative* 20%, not the most repetitive.

### Key Terms

- **Frozen Descriptor** — a dense physical description of an SCP
  entity authored once in the research stage and reused verbatim in
  every image prompt; the primary mechanism for cross-scene and
  cross-shot visual consistency.
- **Shot** — a single visual moment within a scene. Each scene
  contains 1–5 shots, determined automatically by TTS duration
  (≤8s→1, 8–15s→2, 15–25s→3, 25–40s→4, 40s+→5) with operator
  override. Each shot has its own image, estimated duration, and
  transition type (Ken Burns pan/zoom, cross-dissolve, or hard cut).
- **Scene Clip** — the intermediate video artifact produced per scene:
  shot images composed with their transitions, timed to their
  durations, and overlaid with the scene's TTS audio. The final
  video is a deterministic concatenation of all scene clips.
- **HITL 3-tier** — auto-approve (critic-score gated) / batch review
  (bulk scene overview) / precision review (first appearances,
  act-boundary scenes).
- **Option D Critic Stack** — the project's internal label for the
  industry-standard LLM-as-judge evaluation framework described in
  differentiator #3.
- **Bounded State Machine (V1)** — SQLite-backed pipeline run tracking
  with resumable stage transitions; intentionally scoped below the
  threshold where full workflow-engine patterns (Temporal, Airflow)
  apply.
- **V1 / V2** — V1 = first shippable pipeline; V2 = extensions
  unlocked after five videos have been produced end-to-end through V1.

## Project Classification

- **Project Type**: Hybrid `cli_tool` (primary) + `web_app`
  (secondary). Go Cobra CLI is the canonical pipeline execution path;
  a local web UI on the same SQLite store serves HITL review and
  monitoring. CLI-first development order; web UI as a thin wrapper
  over CLI capabilities.
- **Domain**: `general` — AI-driven media content automation,
  specifically long-form YouTube horror video production in the SCP
  Foundation vertical.
- **Complexity**: medium-high. Nondeterministic multi-stage LLM
  orchestration (Phase A's six-agent chain — Researcher, Structurer,
  Writer, VisualBreakdowner, Reviewer, Critic — plus Phase B's
  parallel image and TTS tracks and Phase C assembly); multi-vendor
  API integration (DashScope TTS / Qwen-Image / Qwen-Image-Edit plus
  multiple LLM providers for writer/critic separation); multimodal
  asset pipeline (text, images, audio, video via FFmpeg);
  industry-standard LLM evaluation infrastructure; sustained HITL UX
  requirements.
- **Project Context**: Greenfield, well-specified. No production code
  exists. Extensive upstream preparation: 11 prompt-stage
  specifications, format guide, channel-format competitive analysis,
  external API specs, UI reference imagery.

## Success Criteria

### User Success

The single user is the operator. Success is defined by sustained
operability, not first-use novelty. The "this was worth building"
moments, in order of weight:

- **Time leverage** (primary): producing a second video of the same
  SCP class in ≥50% less wall-clock time than the first, with the gap
  attributable to the pipeline (not to the operator's growing skill).
- **Long-run stability** (load-bearing): the tool is still in active
  use 6 months after V1, with no rebuild required and recovered-from
  failures below a defined threshold.
- **Automation confidence**: 5 consecutive end-to-end runs reach
  Phase C with no operator intervention beyond the planned HITL
  3-tier touchpoints.

### Business Success

Channel-side metrics (subscriber count, revenue, watch-hour
thresholds) are deliberately out of scope for this PRD. This document
specifies a tool, not a content business plan. Tool-side business
success is measured indirectly: the operator continues to produce
videos through the pipeline as their primary production path, rather
than reverting to manual workflow or a competing tool.

### Technical Success

All thresholds below are measured starting Day 1; targets become
*acceptance gates* on Day 90.

All metrics use a **rolling 25-run window** (n=25). Earlier in the
project, when fewer than 25 runs have completed, dashboard values are
shown with a **"provisional"** label and are not used for V1.5
gating. Rationale: at n=10 the standard error on kappa is ~0.15,
which is too noisy for threshold decisions; n=25 reduces SE to ~0.09.

| Metric | Definition | Day 90 Target |
|---|---|---|
| Automation rate | (auto-completed stages / total stages), averaged across last 25 runs | ≥ 80% |
| Critic calibration | Cohen's kappa between critic verdict and operator override decisions, rolling 25-run window | ≥ 0.7 |
| Critic regression detection | Detection rate on the ≥20-case Golden eval set, run via `go test ./critic -run Golden` | ≥ 80% |
| Defect escape rate | Fraction of critic auto-passed scenes that the operator subsequently rejected, rolling 25-run window | ≤ 5% |
| Stage-level resume idempotency | Same input + `resume` from an arbitrary failed *stage* produces functionally equivalent output | 100% (functional equivalence; bit-equivalence not required for nondeterministic LLM stages) |

Cost and per-video time targets are intentionally **measured but
unquantified** until V1.5 — the operator will set thresholds based on
real distribution data from the first five videos. Per-stage cost
hard caps (hard-stop on overrun) are enforced from Day 1 as a survival
guard, separate from the eventual cost target.

### Measurable Outcomes

| Milestone | Outcome |
|---|---|
| Week 6 (V1 ship) | First publishable video produced end-to-end through the pipeline. |
| Day 90 | All Technical Success thresholds above met on rolling 25-run window (or "provisional" if n<25). |
| Month 6 | Tool still in active use; cumulative production ≥ 25 videos; no architectural rebuild has occurred. |

## Product Scope

This project intentionally uses a **2-layer scope model with an
explicit deferred backlog**, plus a single **V0 operator
prerequisite**, rather than the conventional MVP/Growth/Vision split.
Rationale: in a single-operator project, "Growth" and "Vision" tiers
tend to never be executed. We name only what we plan to ship, and
explicitly mark deferred work as deferred — including the
possibility that it never happens.

### V0 — Operator Prerequisite (Week 1, Day-1 task)

A `LICENSE_STANCE.md` document, written by the operator, must exist
in the repository before any pipeline code that touches license
metadata is implemented. The document answers: **"How does CC BY-SA
3.0 attribution and ShareAlike apply when SCP source articles are
processed through AI to produce derivative narration and imagery?"**
Lawyer review is optional but the position must be written down.

Rationale: if this position later inverts (e.g., "ShareAlike
applies and the entire pipeline output must be CC BY-SA"), the
pipeline architecture changes — license metadata flow, monetization
model, even output formats. Discovering this after V1 is sunk cost.

This is not a coding deliverable; it is treated as a Week-1 task
absorbed inside the V1 budget, not a separate phase. See §Risk
Register > V1 Wall-Clock Budget.

The product is **Korean-language only**. Multi-language extension
hooks are not scoped into V1 architecture; if multi-language is
pursued later, it will be a structural revision, not a flag flip.

### V1 — Single Shippable Scope (~6 weeks)

**Pipeline structure (macro)**:

- **Phase A — Scenario generation (multi-agent)**: input is an SCP
  ID plus the local data corpus at `/mnt/data/raw` (raw article +
  fact sheet + related materials). Six agents in sequence —
  Researcher, Structurer, Writer, VisualBreakdowner, Reviewer,
  Critic — implemented as a plain function chain (no workflow
  framework). The Critic emits pass/retry/accept-with-notes verdicts;
  the Reviewer runs a separate fact-coverage and storytelling-quality
  check. Critic is invoked at two points: immediately after Writer,
  and after the full Phase A output assembles.
- **Phase B — Image and TTS generation (parallel tracks, sync at
  assembly)**: the VisualBreakdowner in Phase A determines shot count
  per scene (1–5 shots based on estimated TTS duration: ≤8s→1,
  8–15s→2, 15–25s→3, 25–40s→4, 40s+→5; operator override available).
  Image track: per-shot image generation (~30 images per run) via
  Qwen-Image / Qwen-Image-Edit (the latter for character-bearing
  shots, seeded by a DuckDuckGo-derived character reference and a
  Vision Descriptor pass). TTS track: scenario refinement (numerals,
  English terms → Korean orthography) → DashScope qwen3-tts-flash
  (one TTS audio per scene). Both tracks run independently via
  `errgroup.Group` and share the DashScope rate-limit budget.
- **Phase C — Assembly (two-stage)**: (1) per-scene clip assembly —
  each scene's shot images are composed with transitions (Ken Burns
  pan/zoom, cross-dissolve, or hard cut), timed to shot durations,
  and overlaid with the scene's TTS audio to produce a scene clip;
  (2) final concatenation of all scene clips into the output MP4.
  V1 includes three transition types; audio fade and BGM are V1.5.

**Cross-cutting V1 capabilities**:

- Bounded state machine (SQLite `runs` table; resumable from any
  failed *stage* via a `resume` command). **Granularity is stage
  level only** in V1; per-track and per-scene resume are deferred to
  V1.5.
- Option D Critic Stack: cross-model writer/critic, ≥20-case golden
  eval set with positive:negative ratio maintained at 1:1, rule-based
  pre-checks (JSON schema + forbidden-term regex), rubric-weighted
  sub-scores, regression harness via `go test ./critic -run Golden`,
  shadow-eval gate (`go test ./critic -run Shadow` blocking any
  Critic prompt PR that produces ≥1 false reject on the last 10
  passed scenes), per-stage cost budget caps.
- **Anti-progress detection** (V1, promoted from V1.5): cosine
  similarity of consecutive retry outputs >0.92 triggers early-stop
  and human escalation. Mitigates the failure mode where the Critic
  loops indefinitely on a structural defect the writer cannot fix.
- HITL 3-tier review surface: auto-approve / batch / precision.
- **Decision history schema** (`decisions` table): every operator
  HITL action recorded (target scene id, decision type, timestamp,
  context). V1 records only; the learning layer (style presets,
  recommendations) is V1.5. The schema must exist in V1 so V1.5 can
  build on real history rather than a cold start.
- **Mode switching surface**: the operator-mode indicator is the web
  UI's top-level tab structure (Production / Tuning / Settings &
  History). Prevents the Mode 1 ↔ Mode 3 context monolith. A
  dedicated CLI command for mode indication was considered and
  dropped; see §Risk Register > Rejected Items.
- Observability: per-stage `pipeline_runs` rows capturing
  `duration_ms`, `token_in`, `token_out`, `retry_count`,
  `retry_reason` (TEXT, NULLABLE), `critic_score`, `cost_usd`,
  `human_override` — eight columns. Day-90 dashboards in V1 are SQL
  views accessed via CLI output; a graphical metrics dashboard is
  V1.5.
- CLI-first surface (Cobra). Local web UI implemented as a thin
  wrapper providing the HITL review screens and run monitoring.

### V1.5 — Stability Polish (1–2 weeks; gated on five videos shipped through V1)

A timeboxed polish sprint, scoped to convert measured V1 weaknesses
into product hardening:

- Automatic retry policy (exponential backoff, per-stage strategies).
- HITL UX polish (similarity-based batch approval, style presets,
  decision memory built on the V1 `decisions` schema).
- Critic ensemble (multi-judge voting on borderline verdicts).
- **Per-track and per-scene resume** granularity (extends V1's
  stage-level resume).
- **Graphical metrics dashboard** with rolling-window aggregation,
  threshold-based pass/fail surfacing, V1.5-gate evaluation views.
- FFmpeg assembly upgrades (audio fade, simple BGM layer, advanced
  transitions beyond V1's Ken Burns / cross-dissolve / hard cut set).
- Cost and per-video time targets formalized as acceptance gates,
  using the V1 distribution data.
- Defect Escape Rate reaches the ≤5% gate.
- **`doctor` checks expanded**: Golden eval set load verification,
  per-key parsing/format validation (V1 ships with the 3 essential
  checks only — keys present, FS paths, FFmpeg).
- **DuckDuckGo manual URL-input fallback** in the precision-review
  UI (V1 ships with aggressive cache only; if cache misses or DDG
  is blocked, the operator opens a browser).
- **OS keyring secret management** (V1 uses `.env`).

### Backlog — Deferred (treated as "deferred = potentially never")

Items below are recognized as plausible future directions but are
not committed to a release. They are listed for traceability, not as
a roadmap.

- YouTube AI-content policy automation (auto-labeling, monetization
  status checks).
- Critic golden-set auto-augmentation (turn operator rejects into new
  fixtures automatically).
- Cross-format SCP variants (short-form, role-play, lore-deep
  formats).
- Multi-creator or licensed-distribution mode.

Multi-language extension is **explicitly excluded** from scope at all
tiers, including this backlog. The system is built for Korean
operation; if multi-language is pursued later, that is a separate
project with structural implications, not a feature flip.

## User Journeys

This product has a single operator. Journeys are not separated by
persona — they are separated by **the mode in which the operator
interacts with the system**. Nine operating modes are mapped below;
collectively they reveal the product's full capability surface.

External user types that conventional PRDs map (admin, support, API
consumer) are **explicitly out of scope**: the system exposes no
external API, has no support function, and the operator is also the
administrator.

To make mode boundaries operational, the system surfaces a top-level
tab split in the local web UI (Production / Tuning / Settings &
History). This prevents the operator from drifting between production
work and quality-stack maintenance without consciously switching
context.

### Mode 1: Operator — Producing a Video (happy path)

**Scene.** Tuesday afternoon. The operator starts:
`youtube-pipeline create scp-049`. The CLI confirms the run ID and
opens the local web UI in Production tab.

**Rising action.** Phase A unfolds in the background: Researcher
pulls SCP-049's article + fact sheet + related materials from
`/mnt/data/raw`; Structurer drafts scenes; Writer produces Korean
narration; the Critic emits *pass*; VisualBreakdowner runs;
Reviewer fact-checks. UI ticks each sub-stage from grey to green.

A notification: Phase B has reached the **character reference
prerequisite** for SCP-049 (the Plague Doctor — a defined character).
Image track halts here briefly; the system shows 10 DuckDuckGo
results as a thumbnail grid. This is a precision-tier HITL touchpoint.
The operator picks one, edits one sentence in the auto-extracted
Vision Descriptor, and clicks Approve. Qwen-Image-Edit upgrades it
into the canonical character reference. The image track resumes;
the TTS track has been running in parallel the whole time.

When both tracks complete, the operator opens the **batch review
screen**, which composes per-scene cards (scene clip mini-video
showing 1–5 shots with transitions + narration text + audio playback).
Critic auto-pass scenes are collapsed by default; only two scenes are
flagged for precision review. The operator sees the scene clips play
through, approves both. Phase C assembles per-scene clips into the
final MP4.

Every approval, edit, and pick is written to the `decisions` table.

**Resolution.** 47 minutes total wall-clock; 11 operator-attended.
Output handed off to the upload queue.

*Capabilities revealed:* CLI run lifecycle, web-UI run monitoring,
HITL 3-tier (auto / batch / precision), character reference
prerequisite (Phase B image-track gate), per-scene composed review
cards, decision history capture, output handoff.

### Mode 1-Repeat: Operator at the 30th Video (wear pattern)

**Scene.** Three months in. The operator opens batch review for
SCP-3008. The screen looks familiar — too familiar.

**Rising action.** Of 11 scenes, 9 are auto-approved by Critic. The
operator scans the two flagged scenes (a hook variant and an
act-3 reveal), approves both in 40 seconds, hits "Approve all
remaining" without watching individually. The system shows a
confirmation: "9 scenes will be approved using your last 5 videos'
acceptance pattern." The operator confirms.

**Resolution.** Total operator time on this 30th video: 4 minutes.
The 20% manual share has compressed into the highest-leverage
moments only — not because of fatigue but by design.

*Capabilities revealed:* batch "approve remaining" with confidence
context, decision-pattern surfacing (V1.5 will close the loop into
auto-approval suggestion), repeat-use ergonomics over novelty
ergonomics.

### Mode 2: Operator — Recovering From a Failure (edge case)

**Scene.** Mid-run, a banner: "TTS track failed at scene 7 —
DashScope upstream timeout." Cost-cap dashboard is green.

**Rising action.** The run's `tts` stage is marked `failed`; image
artifacts (a separate stage) are intact. The operator runs
`youtube-pipeline resume scp-049-run-42`. Resume is **stage-level**
in V1: the TTS stage as a whole re-executes from scratch with the
same inputs, producing the same audio for scenes 1–6 again plus the
remaining scenes. Per-scene resume is V1.5; for now, this trade-off
is acceptable because TTS is the cheapest stage.

**Resolution.** Run completes 14 minutes later. Cost-cap dashboard
shows the additional cost of one full TTS stage re-run, logged as
expected.

*Capabilities revealed:* stage-level failure isolation, persistent
cross-stage artifacts, granular `resume` command, cost
observability, recovery without operator panic, explicit V1
limitation acknowledged in product surface.

### Mode 3: Maintainer — Tuning the Critic Prompt

**Scene.** Defect Escape Rate dashboard reads 8.2% over the last 25
runs. The operator switches to **Tuning** tab.

**Rising action.** They identify a recurring miss (Critic auto-passes
wiki-style framings disguised as questions), edit the Critic prompt,
add a forbidden pattern, then run:

```
go test ./critic -run Golden    # regression: must hit ≥80% detection
go test ./critic -run Shadow    # shadow: must produce 0 false rejects
                                # against the last 10 passed scenes
```

Both pass: Golden returns 18/20; Shadow returns 0 regressions. The
operator commits the prompt diff together with one new positive
fixture and one new negative fixture (positive:negative ratio
preserved at 1:1).

**Resolution.** Over the next 10 runs, kappa rises 0.65 → 0.74. The
change is regression-tested in two dimensions (recall and
specificity), not vibes-edited.

*Capabilities revealed:* Tuning tab as separate UX surface, Critic
prompt versioning, Golden + Shadow eval execution, calibration
metric tracking, fixture-with-PR discipline, ratio-balanced fixture
governance.

### Mode 4: Diagnostician — Investigating a Cost Anomaly

**Scene.** Nightly summary email reports yesterday's run cost 4× the
rolling average.

**Rising action.** The operator queries `pipeline_runs` directly.
Stage `writer` has `retry_count = 4` and abnormal `token_in/out`.
The new `retry_reason` column shows the same complaint repeated four
times: "act-3 reveal too soft." Cost cap eventually hard-stopped at
retry 4.

This is exactly the case the V1 **anti-progress detector** is
supposed to catch (cosine similarity between consecutive retries
> 0.92 → early-stop). The operator checks: it did fire — but on
retry 3, after three wasteful calls. The retry diff hash logs
confirm the early-stop saved retry 4 from running. The remaining
loss is the cost of getting to similarity detection in the first
place.

**Resolution.** Investigation took 9 minutes. The operator escalates
the structural-defect pattern to Mode 3 (maintainer) for a Critic
prompt fix, and to Mode 6 (Golden Set Curation) for a new fixture.

*Capabilities revealed:* observability schema sufficiency
(`retry_reason` column), per-stage cost attribution, anti-progress
detector telemetry, raw SQL access as a diagnosis tool, cross-mode
escalation routing.

### Mode 5: Reviewer — Day-90 Acceptance Gate Evaluation

**Scene.** Day 90. 27 videos shipped. The operator opens a CLI
report (`pipeline metrics --window 25`) — V1 ships SQL views over
`pipeline_runs`, not yet a graphical dashboard (that's V1.5).

**Rising action.** The text output shows the rolling 25-run window:

- Automation rate: 84% (≥ 80% — pass)
- Critic kappa: 0.74 (≥ 0.7 — pass)
- Critic regression detection: 85% (≥ 80% — pass)
- Defect escape rate: 6% (≤ 5% — **fail**)
- Stage-level resume idempotency: 100% (pass)

Four of five gates met. The operator opens the V1.5 backlog file,
adds one item driven by Mode 4 findings ("anti-progress detector
threshold tuning — current 0.92 fired at retry 3 instead of 2") on
top of the prepared V1.5 list.

**Resolution.** V1.5 sprint scoped, dated, and timeboxed to two
weeks. A decision was produced, not a discussion.

*Capabilities revealed:* rolling-window metric aggregation as SQL
views in V1, CLI-driven gate evaluation, threshold pass/fail
surfacing, V1.5 backlog authoring tied to observed gaps. Graphical
dashboard explicitly deferred to V1.5.

### Mode 6: Maintainer — Golden Set Curation

**Scene.** A defect escape sneaks past the Critic, but the operator
decides the Critic prompt is fine — the Golden set just lacks an
example of this failure mode.

**Rising action.** No prompt edit. The operator authors a single new
negative fixture from the rejected scene. To preserve the 1:1
ratio rule, they also author a paired positive fixture (a similar
scene that should pass). They run `go test ./critic -run Golden` to
confirm both new fixtures behave correctly with the *current*
Critic. They commit.

**Resolution.** The eval set has grown from 22 to 24 cases. No
prompt was changed; the bar moved. This is a distinct workflow from
Mode 3 (Critic prompt change) — what's evolving is the *quality
specification*, not the quality enforcer.

*Capabilities revealed:* fixture-only PR workflow, ratio-rule
enforcement at commit time, distinction between "evaluator change"
and "evaluation criteria change" in versioning.

### Mode 7: Operator — First-Time Setup (onboarding)

**Scene.** A new machine. Empty `~/.youtube-pipeline/` directory.

**Rising action.** `go install` builds the binary.
`youtube-pipeline init` creates the SQLite database, the
`/mnt/data/raw` directory pointer, and an empty `.env` template
listing required keys: `DASHSCOPE_API_KEY`, `DEEPSEEK_API_KEY` (or
equivalent writer key), `GEMINI_API_KEY` (or equivalent critic
key). The operator fills these.

`youtube-pipeline doctor` runs preflight: pings each API with a
trivial call, confirms `/mnt/data/raw` is readable, confirms the
Golden eval set is loaded. Each check is green or fails loudly with
a remediation hint.

**Resolution.** Total time from clone to "ready for first
`create`": under 10 minutes if all keys are at hand.

*Capabilities revealed:* `init` and `doctor` subcommands, secret
preflight, dependency-readiness check, fail-loud diagnostics.

### Mode 8: Operator — Returning After a Pause

**Scene.** The operator hasn't used the tool in two weeks. They
reopen the web UI.

**Rising action.** The Production tab shows: 2 runs in `pending`
state (one was paused mid-precision-review, one failed and was
never resumed), 1 partial draft never completed. Each card has a
last-action timestamp and a one-line "what was happening" summary
auto-generated from the last `decisions` row + run status.

The operator picks the failed run, sees the failure reason, runs
`pipeline resume`. The paused run resumes from the precision-review
screen exactly where they left off.

**Resolution.** No "where was I?" friction. The system carries the
state so the operator doesn't have to.

*Capabilities revealed:* run inventory with state-aware summaries,
resumability of paused HITL touchpoints (not just failed stages),
session continuity surface.

### Journey Requirements Summary

The nine modes collectively require the following capability surfaces:

- **Run lifecycle**: create / resume / cancel / inspect at run,
  phase, and stage level (Modes 1, 2, 8). Per-track / per-scene
  resume is V1.5, not V1 (Mode 2 explicitly).
- **Mode switching surface**: top-level Production / Tuning /
  Settings & History tab split in the web UI (cross-cutting).
- **HITL 3-tier review surface**: auto-approve, batch (with composed
  per-scene cards showing scene clips with multi-shot transitions),
  precision (Modes 1, 1-Repeat). Pause-and-resume on precision
  touchpoints (Mode 8).
- **Character reference prerequisite workflow**: search → pick →
  Vision Descriptor edit → image-edit upgrade. Lives at the
  Phase B image-track gate, not in Phase A (Mode 1).
- **Stage-level failure isolation with persistent cross-stage
  artifacts** (Mode 2).
- **Critic infrastructure (Tuning surface)**: prompt versioning,
  Golden eval and Shadow eval execution via `go test`, fixture
  authoring with positive:negative 1:1 enforcement (Modes 3, 6).
- **Anti-progress detection**: cosine-similarity early-stop on
  retry loops, with telemetry visible to diagnosticians (Mode 4).
- **Observability schema**: `pipeline_runs` 8-column row including
  `retry_reason`, sufficient for raw-SQL diagnosis without external
  tooling (Mode 4).
- **Decision history schema**: `decisions` table records every
  HITL action; V1 records only, V1.5 reads for learning (Modes 1,
  1-Repeat, 8).
- **Cost telemetry and hard caps** at the stage level (Modes 2, 4).
- **Day-90 metric aggregation as SQL views**: V1 ships CLI report
  output (`pipeline metrics --window 25`); graphical dashboard is
  V1.5 (Mode 5).
- **First-run setup**: `init` and `doctor` subcommands; secret and
  dependency preflight (Mode 7).
- **Returning-user surface**: run inventory with state-aware
  summaries, paused-HITL resume (Mode 8).

## Domain-Specific Requirements

The classification step assigned this product to the `general`
domain at `medium-high` complexity. The standard `general` mapping
does not capture the domain-specific concerns of AI-driven genre
content automation, so the relevant constraints are enumerated
explicitly below. This section is scoped to *domain* concerns
(licensing, content policy, vendor compliance, integration); broader
project risks live in §Risk Register.

The V0 operator prerequisite (a written CC BY-SA stance, see
§Product Scope) must be in place before any item below is
implemented.

### Compliance & Regulatory

- **SCP Foundation source licensing (CC BY-SA 3.0)**: source
  articles are licensed under Creative Commons
  Attribution-ShareAlike 3.0. Commercial use is permitted, but the
  ShareAlike obligation's applicability to AI-derived work is
  legally unresolved at the time of writing. The product must:
  (a) preserve attribution metadata at every stage; (b) keep a
  per-video source manifest that includes **the originating SCP
  article's own attribution chain — any sub-works (images, quoted
  excerpts, embedded media) and their licenses are recorded
  alongside the SCP article itself**; (c) treat the operator's
  monetization activation as out of scope for the tool, while
  treating compliance with licenses and policies that affect
  monetization eligibility as **in scope** for the tool.
- **YouTube AI-generated content disclosure**: as of 2024 YouTube
  requires creators to disclose synthetic or altered media in the
  upload flow. The product must produce, alongside each MP4, a
  metadata bundle declaring AI-generated narration, AI-generated
  imagery, and the underlying models used. **A compliance gate is
  enforced in V1**: the final MP4 is not surfaced as
  "ready-for-upload" until the operator has explicitly acknowledged
  the metadata bundle for that video. Auto-disclosure on upload via
  the YouTube Data API is V1.5; the V1 gate exists to close the
  human-forgetting window.
- **LLM and media-model vendor ToS**: DeepSeek, Gemini, Qwen, and
  DashScope each publish separate commercial-use terms and
  disallowed-content lists. The system records which vendor served
  which generation in `pipeline_runs`, enabling retroactive ToS
  audit when terms change.
- **Korean horror content rating considerations**: the product
  ships forbidden-term lists and hook-library guidance derived from
  Korea Communications Standards Commission (KCSC) criteria, so
  generated narration **can be steered away from age-restricted
  territory by default**. The tool *enables* avoidance; it does not
  *enforce* it. Final tone calibration is the operator's
  responsibility.
- **Minor-depiction safeguards**: the SCP corpus contains material
  involving minors. The Critic includes a pre-check rule that flags
  scenes describing minors in violent, sexualized, or otherwise
  YouTube-Kids-policy-violating contexts. Flagged scenes block
  Phase B until operator review.
- **Voice likeness / deepfake exposure**: TTS output that
  approximates an identifiable real person's voice creates Korean
  portrait-rights exposure. The product commits to using designed
  voice profiles (Qwen3-TTS-VD/VC) only for original synthetic
  voices; cloning a real person's voice is explicitly out of scope,
  and a runtime check verifies that no `voice_profile` references
  an identified-person source.

### Technical Constraints

- **DashScope region commitment**: TTS and image models are accessed
  via DashScope, which has separate Singapore (international) and
  Beijing (China mainland) endpoints with different API keys and
  data residency. The system commits to a **single region per
  install** in V1, set at `init` time. Cross-region failover is not
  supported. **Operating procedure for outage** is documented in
  §Operational Plays: a multi-hour DashScope outage stops production
  for that day; runs resume from the last successful stage when the
  region recovers (stage-level resume only; see "Resume semantics"
  below).
- **Resume semantics (honest framing)**: V1 resume is **stage-level
  only**. A stage-level checkpoint is preserved; *partial work
  inside a failed stage is re-executed*. For example, if 5 of 10
  TTS scenes complete before a 429, the TTS stage on resume
  re-renders all 10. With the multi-shot model (~30 images per run
  instead of ~10), the image stage resume cost is ~3× higher than
  the single-image model; per-stage cost caps are the actual
  loss-limit defense. If cost behavior shows this is too painful,
  per-scene or per-shot resume is the V1.5 upgrade.
- **LLM API rate limits and quotas**: every external call surfaces
  rate-limit headers (when provided) into `pipeline_runs`; per-stage
  cost cap doubles as an implicit rate guard. Stages back off on 429
  by failing the stage and emitting a `retry_reason="rate_limit"`
  row.
- **Single-machine SQLite assumption**: `pipeline_runs`,
  `decisions`, and Golden eval fixtures all live in local SQLite.
  Multi-machine operation is out of scope; if the operator moves to
  a new machine, manual SQLite + filesystem migration is the
  expected path.
- **Secret management**: API keys are stored in a local `.env` file
  loaded by Viper at startup. OS keyring integration is V1.5
  backlog. The `doctor` subcommand refuses to run if any required
  key is missing.
- **Local filesystem dependencies**: `/mnt/data/raw` (source
  corpus), an output asset directory, and the SQLite database file
  are all local. The system fails loudly at `init`/`doctor` if any
  of these paths is missing or unwritable.
- **`Check` interface for preflight**: `doctor` is implemented as an
  orchestrator that walks a registry of `Check` implementations.
  V1 registers exactly three: API key presence, filesystem path
  writability, FFmpeg binary availability. V1.5 registers two more:
  Golden eval set load verification, per-key parsing/format
  validation. Adding more checks does not increase `doctor`'s own
  complexity — only the registry grows.

### Integration Requirements

- **DashScope (single vendor for media)**: Qwen3-TTS-Flash for TTS;
  Qwen-Image for plain image generation; Qwen-Image-Edit for
  reference-seeded character/scene images. SiliconFlow is
  explicitly excluded.
- **Multi-LLM providers (writer ≠ critic)**: at least two distinct
  LLM providers must be configured. Reference configuration:
  DeepSeek (writer) × Gemini (critic). Qwen may substitute on
  either side, but writer and critic must always come from
  different providers. **Enforcement**: `doctor` rejects any config
  where `writer_provider == critic_provider`; the same check runs
  at `run` entry as a defense-in-depth duplicate, in case the
  operator skipped `doctor`.
- **DuckDuckGo image search (character reference workflow)**: used
  by the Phase B image-track prerequisite. **Aggressive caching**:
  results are cached per character query and re-used across runs;
  a cache hit means no external call. Cache misses fall back to the
  operator opening a browser (V1). A built-in manual URL-input
  fallback in the precision-review UI is V1.5.
- **FFmpeg system binary**: required for Phase C. `doctor` detects
  its absence and refuses to start a run.
- **YouTube Data API (V1.5)**: upload automation, AI-content
  disclosure metadata submission, basic metadata population. Out of
  V1 scope; V1 ends at MP4 + metadata-bundle output behind the
  compliance gate.

### Operational Plays

These are documented operating procedures, not features.

- **DashScope regional outage**: pause all new `create` invocations;
  use `pipeline metrics` to identify in-flight runs; let any
  in-flight run that has crossed Phase A continue (Phase A is LLM,
  not DashScope); for runs blocked at Phase B, do nothing — they
  will resume cleanly via stage-level resume once the region
  recovers. Document the outage window in `LICENSE_STANCE.md`'s
  ops appendix.
- **Critic prompt rollback**: if a Mode 3 (maintainer) PR ships and
  kappa drops in the next 5 runs, revert the prompt diff via git;
  no special tooling required. The Golden + Shadow eval gate
  reduces but does not eliminate this risk.
- **Source manifest audit**: per-video source manifests are append
  only and version-controlled alongside the MP4 outputs. A takedown
  request maps to its manifest by video ID; no special tooling.

### Domain-Specific Risk Mitigations

- **LLM model deprecation**: model identifiers live in config, not
  in code. Deprecation requires a config change plus a regression
  run, not a code change.
- **DuckDuckGo result variability or blocking**: aggressive cache
  carries the workflow; manual URL-input fallback is V1.5.
- **YouTube AI-content policy changes**: the metadata bundle
  produced in V1 is the contract surface. If YouTube changes
  required disclosure fields, only the bundle generator changes —
  not the pipeline.
- **Source licensing disputes**: source attribution + per-video
  manifest (with sub-work license chain) gives any takedown request
  a clear evidence trail. The product does not arbitrate licensing
  decisions; it produces an auditable record so the operator can.

## Project-Type Specific Requirements (CLI + Local Web UI Hybrid)

### Project-Type Overview

The product is a **hybrid CLI-primary + local web UI** application
delivered as a single Go binary. The Cobra CLI is the canonical
entry point for every pipeline operation; the local web UI is a thin
SPA layered on the same SQLite store, providing visual surfaces for
HITL review (Production tab), quality-stack maintenance (Tuning
tab), and meta-tasks like decision history and presets (Settings &
History tab) — see §User Journeys.

The two surfaces are not independent products. A package-level
import-direction rule guarantees this:

```
web  → cmd  → service
```

Reverse imports are forbidden, enforced at compile time by Go's
import-cycle check. The web UI never implements pipeline behavior
the CLI lacks; everything flows through the same service layer.
This rule is the actual architectural decision; framework choice
(see Web UI Surface) is downstream of it.

### CLI Surface (Cobra)

**Command tree (V1, eight commands)**:

```
pipeline init                  # first-run setup
pipeline doctor                # preflight Check registry
pipeline create <scp-id>       # start a new run (Phase A → B → C)
pipeline resume <run-id>       # resume from last failed stage
pipeline cancel <run-id>       # abort an in-flight run
pipeline status [<run-id>]     # inspect run(s) state
pipeline metrics --window N    # SQL-view metric report
pipeline serve                 # start local web UI server
```

(A `pipeline mode` command was considered and dropped: its semantics
were ambiguous, and operator-mode awareness is delivered by the web
UI tab structure instead.)

**Interaction style**: interactive primary (one-shot human
invocation per video); scriptable batch is V1.5 backlog. V1's
single-video-at-a-time constraint matches the operator's actual
production cadence (Mode 1).

**Output formats**:

- **Stdout (default)**: human-readable, color-coded, hierarchical
  status (phase → stage → sub-step). Suitable for terminal scanning.
- **`--json` flag**: machine-readable structured output for any
  command, wrapped in a standard envelope:
  `{"version": 1, "data": {...}}`. The version field is the V1.5
  scripting compatibility contract — additive changes increment
  `data` shape; breaking changes increment the version.
- **Output abstraction layer**: implemented as a `Renderer`
  interface set in `cobra.PersistentPreRun`. Each command calls
  `out.Render(value)` and is agnostic to whether output is human or
  JSON. This is a V1 must-have, not a V1.5 polish — bypassing it
  guarantees per-command marshalling drift.
- **File outputs per run**: MP4, metadata bundle (JSON declaring
  AI-generated content, models used, source attribution), source
  manifest (per-video license chain — see §Domain-Specific
  Requirements). The decisions log is **not** a per-run file
  output: the `decisions` SQLite table is the single source of
  truth (SSOT). Operators can export a run's decisions to JSON via
  `pipeline status <run-id> --export decisions` if needed.

**Config method**: Viper hierarchy:

1. `.env` — secrets only (`DASHSCOPE_API_KEY`, writer key, critic
   key). Not version-controlled.
2. `~/.youtube-pipeline/config.yaml` — non-secret configuration:
   model identifiers (e.g. `qwen3-tts-flash-2025-09-18`), DashScope
   region, `/mnt/data/raw` path, output directory, per-stage cost
   caps.
3. CLI flags — per-invocation overrides where appropriate.

**Shell completion**: deferred to V1.5 (Cobra provides this out of
the box; deferring only because V1 has higher-leverage work).

### Web UI Surface

**Architecture**: a single-page application served by the same Go
binary via `pipeline serve`. Static assets are embedded via Go's
`embed.FS` so the binary remains the single deployment unit.

**Top-level structure — three tabs**:

- **Production tab** — Modes 1, 1-Repeat, 2, 8: run lifecycle
  monitoring, HITL 3-tier review surfaces (auto-approve, batch,
  precision), character-reference workflow, paused-run resume.
- **Tuning tab** — Modes 3, 4, 5, 6: Critic prompt versioning,
  Golden + Shadow eval execution surfaces, calibration metrics,
  Golden set fixture management.
- **Settings & History tab** — meta-tasks: a read-only **decisions
  history view** (date / mode / target scene / decision / optional
  note) sourced directly from the `decisions` table, style-preset
  management (V1.5 will read these for auto-approval suggestions),
  pipeline default overrides. The decisions history view is V1
  even though the *learning* layer that consumes the same data is
  V1.5 — without the read-only view in V1, the table is a black
  box to the operator.

The tab boundary is the cognitive mode-switching surface; operators
do not need a separate `pipeline mode` indicator.

**Browser support**: latest version of Chrome, Firefox, and Safari.
Local-only; no IE/Edge-legacy support, no mobile breakpoints, no
SEO. The "approve from a tablet on the couch" scenario is
explicitly out of scope: HITL precision review is scene-clip playback,
shot-sequence review, and grid-pick work that is desktop-native.

**Server lifecycle**: `pipeline serve` runs in the foreground and
binds to localhost (`127.0.0.1`) only. No daemon, no auth, no TLS —
the threat model (single operator, single machine) does not justify
the complexity, and `127.0.0.1` binding excludes external network
access by construction.

**Real-time updates**:

- V1 uses **polling** of the run-status endpoint at a 5-second
  interval. Polling responses include `phase` and
  `progress_percent` fields so the UI can render meaningful
  progress without per-stage events.
- **Optimistic UI updates** are required at HITL touchpoints: when
  the operator presses Enter to accept or reject, the UI shows a
  checkmark animation immediately, without waiting for the
  server's poll-cycle confirmation. The poll cycle eventually
  reconciles (and surfaces an error if the server rejects the
  action). The polling interval itself is not the problem;
  feedback latency at the keypress is.
- SSE / WebSocket upgrade is V1.5 and reuses the same `phase` /
  `progress_percent` message shape established in V1 polling.

**Accessibility (V1, deliberately scoped)**: full WCAG compliance
is out of scope. The product ships **production-speed keyboard
shortcuts** required for a fatigue-resistant HITL surface:

| Key | Action |
|---|---|
| `Enter` | Accept current item |
| `Esc` | Reject / cancel |
| `Ctrl+Z` | Undo most recent decision (required, not optional) |
| `Shift+Enter` | Approve all remaining in current batch |
| `Tab` | Enter edit mode on the current item (e.g., Vision Descriptor sentence) |
| `S` | Skip and remember (records pattern for V1.5 learning) |
| `1`–`9` | Pick a candidate from a 10-grid (e.g., character references) |
| `J` / `K` | Next / previous scene |

Ctrl+Z is non-negotiable: at the 30th video an unintended Enter
is otherwise unrecoverable, and a single such failure in production
breaks the operator's trust in the auto/batch tiers.

**Frontend framework**: a rich SPA framework (React, Svelte, or
Vue) with a mature component ecosystem is the V1 direction. htmx +
server-rendered HTML is **explicitly rejected** for V1; the
operator-experience requirements (3 tabs, optimistic UI on HITL
touchpoints, Ctrl+Z undo, 8-key shortcut surface, decisions history
view, character-reference grid picker) need component-level
interaction state that is awkward to build on htmx. Final framework
choice (React vs Svelte vs Vue) is a §Architecture decision; the
constraint is that V1 must use a tested, production-grade framework
with an established component library and a testing toolchain
(Vitest, Playwright, or equivalent). The `web → cmd → service`
import-direction rule applies regardless of the framework choice.

### Implementation Considerations

- **Output asset directory layout**: a deterministic, per-run
  directory tree (e.g. `~/.youtube-pipeline/output/<run-id>/`)
  containing all stage artifacts. Predictability lets the operator
  inspect / recover artifacts manually without needing the tool.
- **CLI/Web parity contract — enforced by tests**: a
  table-driven contract test asserts that for every state mutation
  performed by a web handler, an equivalent CLI command produces
  the same result. New web functionality without a corresponding
  CLI test fails CI. Convention-and-review is insufficient in a
  one-person project; only the test gate works.
- **Local server lifecycle**: foreground process; CTRL-C terminates
  cleanly after draining in-flight HITL state to the database.
- **Scripting hooks (V1.5 prep)**: the V1 `--json` envelope and
  `Renderer` abstraction are the contract that makes scripting and
  batch operation cheap to add later.
- **`pipeline serve` is the single largest V1 line-item**:
  static-asset embedding + API surface + frontend build + polling
  endpoint + (modest) tab-three views amount to roughly 1 week of
  the 6-week V1 budget. This is a noted scope concentration, not a
  hidden risk.

## Risk Register & V1 Budget

This section is the **single source of truth** for project risks and
for the V1 wall-clock budget. §Product Scope (above) defines *what*
gets built; this section verifies that the scope is achievable —
budget, risks, mitigations, and a cross-step decision audit.

Other sections that mentioned "named risks" or "see §Risk Register"
point here. There is no other risk list in the PRD.

### Why a 2-Layer + Backlog Model

A conventional MVP/Growth/Vision split is rejected because in a
single-operator project, "Growth" and "Vision" tiers tend never to
execute. The 2-layer + Backlog model ships V1, timeboxes V1.5, and
labels Backlog as "deferred = potentially never" so future-self
isn't tempted to move items between tiers instead of shipping.

### V1 Wall-Clock Budget (~6 weeks, single-person serial)

The V1 budget is **6 weeks**, not the 4-week aspirational figure
considered earlier. The 4-week figure assumed ideal-path estimates
on the highest-risk items; the 6-week figure honors realistic
single-developer pacing on `pipeline create` (LLM prompt iteration)
and `pipeline serve` (rich SPA framework setup), plus dedicated
test-infrastructure investment up front.

| Phase | Wall-clock | Deliverable |
|---|---|---|
| Week 1 | 1.0 | V0 prerequisite (`LICENSE_STANCE.md`) + repo bootstrap + test infrastructure (Go testify/gomock, Vitest, Playwright, contract test scaffolding, CI) + `init` and `doctor` (Check registry × 3) |
| Week 2 | 1.0 | `pipeline create` Phase A — 6 LLM agents (Researcher, Structurer, Writer, VisualBreakdowner, Reviewer, Critic) with prompts authored, output-schema validated, and per-agent unit tests. VisualBreakdowner now produces per-scene shot breakdowns (1–5 shots with visual descriptors, durations, transition types) — richer output schema than single-descriptor model |
| Week 3 | 1.0 | Phase A integration — Critic loop wiring (cross-model writer/critic, Golden + Shadow eval harness, anti-progress detector), `decisions` table, `pipeline_runs` 8-column observability, per-stage cost cap |
| Week 4 | 1.0 | Phase B — image track (per-shot generation, ~30 images/run via Qwen-Image / Qwen-Image-Edit, character reference workflow with aggressive DDG cache) and TTS track (scenario refinement → Qwen3-TTS-Flash) running parallel via shared DashScope rate-limiter; Phase C — two-stage FFmpeg assembly: per-scene clip (shots + transitions: Ken Burns / cross-dissolve / hard cut + TTS audio) then final concat (via a Go binding such as `ffmpeg-go`, not raw exec) |
| Week 5 | 1.0 | `pipeline serve` — chosen rich SPA framework setup, Production / Tuning / Settings & History tabs, polling endpoint with `phase` + `progress_percent`, optimistic UI updates at HITL touchpoints, 8-key shortcut surface (Ctrl+Z included), decisions read-only view |
| Week 6 | 1.0 | End-to-end integration, CLI/Web parity contract test suite, first SCP video produced through the pipeline, ship-readiness review against §Success Criteria thresholds |

Operator-side parallel work, not counted in the developer budget:

- **Golden eval set authoring** (≥20 fixtures) — Jay's domain task,
  to be completed before end of Week 3 so the regression harness
  has something to run. If this slips, Critic verification slips.
- **`LICENSE_STANCE.md` writeup** — completed in Week 1.

### V1 Scope Discipline

V1 is committed at the boundary defined in §Product Scope. PM-style
proposals to cut V1 items for timeline (e.g. "ship parity test in
V1.5", "ship metrics in V1.5") are **declined**: testability,
operator usability, and architectural soundness rank above the
6-week ship date. V1.5 is not a parking lot for inconvenient V1
work.

### Technical Risks

| Risk | Mitigation in Scope |
|---|---|
| LLM nondeterminism degrades quality | Cross-model Critic + rubric sub-scores + Golden + Shadow eval; per-stage cost cap as loss-limit |
| Critic loop divergence (no progress across retries) | Anti-progress detector (cosine sim > 0.92 → early-stop), V1 |
| FFmpeg multi-shot assembly (Ken Burns / cross-dissolve / hard cut) proves insufficient for first video | **Explicit fallback**: operator finishes manually in CapCut or equivalent. V1 ships 3 transition types, significantly reducing manual finishing vs. static-image model. V1.5 adds audio fade and BGM. Risk is lower than single-image model because scene clips already have visual rhythm |
| DashScope single-region outage | Stage-level resume + documented operational play (see §Domain-Specific Requirements > Operational Plays); cross-region failover deliberately deferred |
| Stage-level resume re-burns tokens on partial work (~30 images per run on image-stage resume = ~3× cost vs. single-image model) | Honest documentation + per-stage cost cap as actual loss-limit (adjust default cap upward for multi-shot volume); per-scene/per-shot resume is V1.5 |
| Frontend framework wrong choice slows `serve` | `web → cmd → service` import-direction rule means framework is replaceable; CLI/Web parity contract test catches drift |
| Multi-LLM provider misconfiguration (writer == critic) | Defense-in-depth: `doctor` check + `run` entry check |
| LLM prompt iteration takes longer than budgeted | Week 2 buffer is intentionally Phase A only (not Phase B/C); if Phase A slips by ≤3 days, Week 3 absorbs it via reduced testing depth (not skipped — compressed) |

### Resource Risks

| Risk | Mitigation |
|---|---|
| Single point of failure (developer == operator == reviewer) | CLI/Web parity contract test substitutes for code review; Golden + Shadow eval substitutes for quality review |
| Burnout / context loss across 6 weeks | Mode-based UX in V1 reduces context-switching cost; weekly milestone-state target (each Week ends in something demoable) |
| Mid-development pause | Each Week's deliverable is independently runnable (e.g. Week 2's Phase A produces output even without Phase B); resuming after a multi-week pause does not require re-onboarding |
| Scope creep into Backlog items | Backlog labeled "deferred = potentially never"; V1.5 is timeboxed (1–2 weeks) and gated (5 videos shipped through V1); state-machine bright line ("no transition diagrams") |
| Compliance ambiguity blocking ship | V0 prerequisite (`LICENSE_STANCE.md`) closes this in Week 1 |
| Knowledge loss after the operator pauses post-ship | Mode 8 (returning user) journey is V1; run inventory with state-aware summaries surfaces "where was I" without operator memory |

### Market Risk (single line, deliberately minimal)

Channel-side metrics (subscriber count, monetization eligibility,
audience acquisition) are out of scope for this PRD, which specifies
a tool. Audience-acquisition strategy is a known absence and is
the operator's responsibility outside this document; a "channel
growth retro" milestone is recommended at the V1.5 gate (5 videos
shipped) but is not a tool deliverable.

### Domain & Compliance Risks

These are addressed in §Domain-Specific Requirements and not
duplicated here. In summary: SCP CC BY-SA 3.0, YouTube AI-content
disclosure, vendor ToS audit trail, KCSC content rating, minor
depiction safeguards, voice-likeness exposure — each has a
mitigation already encoded in product behavior.

### Scope Decisions Audit

This audit consolidates V1 / V1.5 / Removed decisions made across
prior PRD sections. **It is referenced as the gating checklist at
the V1.5 entry review** (i.e. when 5 videos have shipped through
V1, this table is the ground truth for what V1.5 inherits and what
remains in Backlog).

| Decision | Tier | Origin |
|---|---|---|
| Settings & History tab in web UI | V1 | Project-Type Specific Requirements |
| Decisions table read-only view in web UI | V1 | Project-Type Specific Requirements |
| Anti-progress detector (cosine sim > 0.92) | V1 | User Journeys (Mode 4 review) |
| `retry_reason` column on `pipeline_runs` | V1 | User Journeys (Mode 4 review) |
| Output `Renderer` abstraction + JSON envelope | V1 | Project-Type Specific Requirements |
| CLI/Web parity contract test (CI) | V1 | Project-Type Specific Requirements |
| Ctrl+Z (undo) keyboard shortcut | V1 | Project-Type Specific Requirements |
| Optimistic UI updates at HITL touchpoints | V1 | Project-Type Specific Requirements |
| Compliance gate on metadata bundle | V1 | Domain-Specific Requirements |
| Aggressive DDG cache for character refs | V1 | Domain-Specific Requirements |
| Per-track / per-scene resume | V1.5 | User Journeys (Mode 2 review) |
| Graphical metrics dashboard | V1.5 | User Journeys (Mode 5 review) |
| YouTube Data API auto-disclosure | V1.5 | Domain-Specific Requirements |
| OS keyring secret management | V1.5 | Domain-Specific Requirements |
| DuckDuckGo manual URL-input fallback in UI | V1.5 | Domain-Specific Requirements |
| `doctor` Golden eval set + key-format checks | V1.5 | Domain-Specific Requirements |
| V1 shot transitions (Ken Burns, cross-dissolve, hard cut) | **V1** | Core to multi-shot scene clip assembly; static images without transitions unacceptable for YouTube |
| Audio fade / BGM / advanced transitions | V1.5 | Risk Register (this section) |

### Rejected Items (not in V1, not in V1.5)

- `pipeline mode` CLI command — semantics ambiguous; tab structure
  in the web UI delivers the same orientation.
- Cross-region DashScope failover — complexity outweighs value at
  single-operator scale.
- Multi-language extension — explicitly excluded (see §Product
  Scope).
- htmx + server-rendered HTML for the web UI — interaction
  requirements exceed what htmx delivers ergonomically.

## Functional Requirements

This section is the **capability contract** for the entire product.
Every downstream artifact (UX designs, architecture decisions, epic
breakdowns, implementation stories) must trace back to a requirement
listed here. Any capability not listed here will not exist in the
final product unless this list is amended explicitly.

Each requirement states **what** capability the product provides,
not **how** it is implemented. Quality attributes (performance,
reliability, security thresholds, budget thresholds) live in
§Non-Functional Requirements.

Two actors appear: **Operator** (the human user) and **System**
(automated behavior).

Number gaps and letter suffixes are intentional and preserve
traceability (a number that moved elsewhere is marked; letter
suffixes split a capability into independently testable parts).

### Pipeline Lifecycle

- **FR1** — Operator can start a new pipeline run for a given SCP ID.
- **FR2** — Operator can resume a failed run from the last successful stage.
- **FR3** — Operator can cancel an in-flight run.
- **FR4** — Operator can inspect the state of any individual run or of all runs.
- **FR5** — System persists complete run state for every run, including stage progression, status, retry count, and retry reason.
- **FR6** — System captures per-stage observability data — duration, token in/out, retry count, retry reason, critic score, cost, and human-override flag — for every stage of every run.
- **FR7** — *Moved to §Non-Functional Requirements* (per-stage cost budget cap is a threshold constraint, not a capability).
- **FR8** — System stops a retry loop and escalates to the operator when the cosine similarity of consecutive retry outputs exceeds a configured threshold (default 0.92); the threshold is operator-configurable.
- **FR50** — Operator can view a "what changed since I paused" summary for any run, produced as a diff between the latest state and the state at the operator's most recent interaction timestamp.

### Phase A — Scenario Generation (Multi-Agent)

- **FR9** — System generates research output for an SCP ID by drawing from the local data corpus.
- **FR10** — System produces a structured scene plan, Korean narration text, per-scene shot breakdown (1–5 shots per scene with visual descriptors, estimated durations, and transition types), and review report by composing six distinct LLM agents in sequence (Researcher, Structurer, Writer, VisualBreakdowner, Reviewer, Critic). The VisualBreakdowner determines shot count automatically from TTS duration estimates (≤8s→1, 8–15s→2, 15–25s→3, 25–40s→4, 40s+→5); the operator can override shot count and transition types at the `scenario_review` HITL checkpoint.
- **FR11** — System validates each agent's output against a defined schema before passing it to the next agent.
- **FR12** — System enforces that the Writer LLM provider and the Critic LLM provider are different, both at preflight and at run entry.
- **FR13** — System invokes the Critic agent at two checkpoints: immediately after the Writer, and after Phase A overall completion (Reviewer fact-check pass).

### Phase B — Media Generation (Parallel Tracks)

- **FR14** — System generates per-shot images (1–5 shots per scene, ~30 images per run), preserving cross-scene and cross-shot character continuity through a frozen visual descriptor reused verbatim across all image prompts. Each shot's image prompt is derived from the VisualBreakdowner's shot-level visual descriptor.
- **FR15** — System generates per-scene Korean TTS audio, with numerals and English terms transliterated to Korean orthography prior to synthesis.
- **FR16** — System executes the image and TTS tracks concurrently via `errgroup.Group` (NOT `errgroup.WithContext` — one track's failure must not cancel the other); both tracks complete before the assembly stage begins. Image track volume is ~30 images per run (~3× single-image model); total wall-clock time is captured as an operational metric.
- **FR17** — For SCPs with a named character, System surfaces a character-reference selection prerequisite to the operator (10 candidate images from cached image search) before image generation proceeds for that character.
- **FR18** — System generates the canonical character reference image from a selected source through reference-based image editing.
- **FR19** — System caches character-search results aggressively to avoid repeated external lookups across runs.

### Phase C — Assembly & Output

- **FR20** — System assembles video in two stages: (1) per-scene clip assembly — each scene's shot images (1–5) are composed with their specified transitions (Ken Burns pan/zoom, cross-dissolve, or hard cut), timed to their specified durations, and overlaid with the scene's TTS audio to produce a scene clip (`.mp4`); (2) final concatenation of all scene clips into the output video. V1 transition types: Ken Burns, cross-dissolve, hard cut; audio fade and BGM are V1.5.
- **FR21** — System produces, alongside each video, a metadata bundle declaring AI-generated content (narration, imagery), the models used, and source attribution.
- **FR22** — System produces, alongside each video, a source manifest including the originating SCP article and any sub-work license chain it references.
- **FR23** — System gates the "ready-for-upload" status of any video on explicit operator acknowledgment of its metadata bundle.

### Quality Gating (Critic Stack)

- **FR24** — System produces a verdict (pass / retry / accept-with-notes) from the Critic for each evaluated output, based on a rubric of weighted sub-scores (hook strength, fact accuracy, emotional variation, immersion).
- **FR25** — System runs rule-based pre-checks (schema validation, forbidden-term regex) before any LLM Critic invocation; the forbidden-term list is an operator-authorable, version-controlled artifact (add / remove / revert).
- **FR26** — Operator can author, version, and maintain a Golden eval set of fixtures, with a positive-to-negative ratio enforced at 1:1; System emits a staleness warning when the set has not been re-validated within N days (configurable, default 30) or when the Critic prompt has changed since last validation.
- **FR27** — System runs the Critic against the Golden eval set on demand and reports detection rate.
- **FR28** — System runs the Critic in shadow mode against the N most recently passed scenes (default N = 10) when a Critic prompt change is proposed; reports any false-rejection regressions.
- **FR29** — System tracks a calibration metric between Critic verdicts and operator override decisions over a rolling 25-run window, with a "provisional" indicator while n<25. (The calibration metric is implemented as Cohen's kappa in V1; the FR is technology-agnostic.)
- **FR30** — System flags scene content depicting minors in policy-sensitive contexts and blocks downstream processing until the operator reviews.
- **FR51** — System provides a test-infrastructure capability as a first-class artifact: contract test suite (CLI ↔ web parity), integration test harness (stage-boundary schema verification), Golden / Shadow eval runner, and a seed-able run-state test fixture facility. All of the above execute in CI.
- **FR52** — System provides an end-to-end smoke test that runs the full pipeline on a single canonical seed input (one SCP ID) and must pass before any deployment / release.

### HITL Review Surface

- **FR31a** — System supports auto-approval of review items whose Critic score meets a configured threshold; auto-approved items are recorded with a distinguishing decision type.
- **FR31b** — Operator can batch-review non-auto-approved scenes through composed per-scene cards (scene clip mini-video or shot thumbnail strip + narration text + audio playback).
- **FR31c** — Operator can precision-review high-leverage scenes (character first appearance, hook scene, act-boundary scenes) through an extended-detail surface that shows individual shots within the scene, their transitions, and per-shot image quality.
- **FR32** — Operator can approve, reject, or edit any item presented in any review surface.
- **FR33** — Operator can undo the most recent decision at any review surface.
- **FR34** — Operator can approve all remaining items in the current batch with a single action.
- **FR35** — Operator can mark a decision as "skip and remember" so the pattern is recorded for future learning.
- **FR36** — System records every operator HITL action — target item, decision type, timestamp, optional note — to a persistent decisions store.
- **FR37** — Operator can browse the full decisions history in a read-only view (date / mode / target / decision / note).
- **FR53** — When presenting a scene to the operator for review, System surfaces any prior rejection of a semantically similar scene, with the earlier rejection's reason and target; the operator can act on the warning or dismiss it.

### Operator Tooling & Setup

- **FR38** — Operator can initialize a fresh project: configuration files, database schema, output directory layout, and an `.env` template listing required secrets.
- **FR39** — Operator can run a preflight check that validates secret presence, filesystem path writability, and required system binary availability through an extensible registry.
- **FR40** — Operator can launch the local web UI server, which binds to localhost only.
- **FR41** — System surfaces operator mode (Production, Tuning, Settings & History) as the top-level web UI organization.
- **FR42** — Operator can request machine-readable JSON output for any CLI command, wrapped in a versioned envelope.
- **FR43** — Operator can request human-readable, color-coded, hierarchical status output for any CLI command (default).
- **FR44** — Operator can export per-run decisions or other artifact data to JSON files on demand.

### Compliance & Risk Surface

- **FR45** — System records the LLM provider that served each generation, persistently, to support retroactive ToS audit.
- **FR46** — System rejects any pipeline configuration where the Writer and Critic providers are identical, both at preflight and at run entry.
- **FR47** — System rejects any voice profile whose identifier is present in an operator-maintained "blocked voice-ID" list; the list is the compliance-policy interface for preventing identifiable-real-person voice generation.
- **FR48** — System enforces forbidden-term lists (KCSC-derived) at the narration generation stage so generated content can be steered away from age-restricted territory.
- **FR49** — Operator can replay a paused HITL session from the exact decision point at which it was paused, with a state-aware summary of where the run left off.

## Non-Functional Requirements

This section specifies **how well** the product must perform the
capabilities defined in §Functional Requirements. Categories not
listed are deliberately out of scope; multi-operator, multi-machine,
multi-region, and multi-language concerns are addressed in §Risk
Register > Rejected Items (not restated here).

Thresholds that cannot be quantified before V1 operational data
exists are marked **"V1 baseline measured; threshold formalized
within 30 days of V1.5 entry"**. Placeholders without a formalization
deadline are forbidden.

### Performance

- **NFR-P3** — Pipeline stages must back off on API rate-limit
  responses (HTTP 429) without advancing stage status, and must
  emit `retry_reason = "rate_limit"` in the observability record.
- **NFR-P4** — First-video end-to-end wall-clock time is captured
  in `pipeline_runs` throughout V1; the target threshold is
  formalized within 30 days of V1.5 entry, using the first five
  videos' distribution data.

### Cost & Resource Limits

- **NFR-C1** — Every pipeline stage has a configured maximum cost
  in USD; when a stage's accumulated `cost_usd` exceeds the cap,
  the stage hard-stops and escalates to the operator.
- **NFR-C2** — Every full run has a configured maximum total cost
  in USD; exceeding it hard-stops the run.
- **NFR-C3** — Cost data (`cost_usd`, `token_in`, `token_out`) is
  captured per stage in `pipeline_runs` with no sampling or
  truncation.

### Reliability

- **NFR-R1** — Stage-level resume produces the same downstream
  schema, the same stage-status progression, a non-null required
  metadata bundle, and a validating source manifest, given the
  same inputs. Bit-equivalence on LLM outputs is not required.
- **NFR-R2** — Anti-progress detector false-positive rate is
  **measured throughout V1** on a rolling 50-run window; the
  ≤5% target gate is applied from V1.5 onward. V1 ships with the
  default cosine threshold of 0.92 and a tunable configuration
  field.
- **NFR-R3** — Database writes to `pipeline_runs`, `decisions`,
  and Golden eval fixtures are durable across process restarts
  and crashes mid-stage.
- **NFR-R4** — Web UI client-side state is transient; canonical
  state lives in SQLite. Closing and reopening the web UI
  reconstructs the operator's view from the database without data
  loss.

### Security

- **NFR-S1** — API keys and other secrets are read from a local
  `.env` file in V1 (OS keyring is V1.5); secrets are never
  committed to version control.
- **NFR-S2** — The local web UI server binds exclusively to
  `127.0.0.1`; external network access is blocked at the bind
  interface.
- **NFR-S3** — The SQLite database file is created with
  operator-only read/write permissions (mode 0600 on POSIX or
  the equivalent on Windows).
- **NFR-S4** — V1 assumes localhost-only communication with no
  authentication and no TLS. Any change that exposes the product
  to a network surface requires a recorded Architecture Decision
  Record (ADR) prior to implementation; the presence of an
  approved ADR is the testable artifact.

### Testability (Meta-Principle NFR)

- **NFR-T1** — The CI pipeline executes, on every commit: unit
  tests, integration tests (stage-boundary schema validation),
  contract tests (CLI ↔ web parity), Golden eval (detection rate
  ≥ 80%), Shadow eval (zero false-reject regressions on recent
  passed scenes), and the E2E smoke test (FR52).
- **NFR-T2** — Unit-test line coverage for core pipeline code is
  enforced as a **hard gate** at ≥ 60% from V1, ≥ 70% from V1.5.
  Soft gates are not used; the operator is their own reviewer,
  and a soft gate in a single-operator project is equivalent to
  no gate.
- **NFR-T3** — Every FR has at least one acceptance test or an
  explicit "not-directly-testable, covered by X" annotation.
  The proportion of annotated FRs is **capped at ≤ 15% of total
  FRs**, and each annotation's referenced test ID (X) must resolve
  to an existing test artifact — enforced by CI.
- **NFR-T4** — Seed-able run-state fixtures (SQLite snapshots for
  arbitrary run states) are stored with the repository and
  version-controlled alongside code, sufficient to test FR8
  (anti-progress), FR28 (shadow eval), and FR49 (paused HITL
  replay).
- **NFR-T5** — The Golden / Shadow evaluation runner itself is
  tested as a first-class artifact: at least three canonical
  known-pass and three known-fail fixtures (covering distinct
  failure categories: fact error, descriptor violation, weak
  hook) are included in CI to assert that the evaluation
  infrastructure detects each category. A failure here blocks
  merges on the Critic prompt or evaluation-runner code.
- **NFR-T6** — CI total wall-clock execution time is a **hard
  gate at ≤ 10 minutes**. Exceeding this triggers mandatory test
  parallelization or slow-suite separation; the E2E smoke test
  may be sharded but never skipped.

### Observability

- **NFR-O1** — `pipeline_runs` retains all eight specified columns
  for every stage of every run; no truncation, no sampling.
- **NFR-O2** — Retention of `pipeline_runs` and `decisions` rows
  is indefinite in V1 (no purge mechanism); the operator owns
  manual cleanup if desired.
- **NFR-O3** — Diagnostic queries against `pipeline_runs` through
  the standard SQLite CLI (`sqlite3`) are sufficient for
  operational diagnosis; no external tooling dependency.
- **NFR-O4** — The database migration set includes the SQL indexes
  required for rolling-window metric queries (`pipeline metrics`,
  calibration, defect escape) to complete without full-table
  scans. Index coverage is a reviewed artifact of the migration
  suite, not a runtime latency threshold.

### Maintainability

- **NFR-M1** — Model identifiers (LLM models, TTS voice profiles,
  image generation models) are configuration-driven. No model ID
  is hard-coded in source code; model deprecation requires a
  configuration change plus a regression run, not a code change.
- **NFR-M2** — Authorable artifacts — forbidden-term lists,
  Golden eval fixtures, voice-ID blocklist, pipeline config —
  live in version-controlled files separate from source code.
- **NFR-M3** — Stage-boundary schemas are formally defined (JSON
  Schema or Go struct tags with validation) and referenced in
  stage validation (FR11).
- **NFR-M4** — Layer boundaries (`web → cmd → service → infra`)
  are enforced at CI time by a layer-import linter such as
  `go-cleanarch` or `depguard`; Go's native import-cycle check
  is a necessary but insufficient mechanism. A linter violation
  blocks merge.

### Accessibility (CLI-Only Baseline)

The product is a single-operator local tool; its accessibility
surface is the operator's own production speed under fatigue, not
broad audience reach. Broad WCAG compliance is correctly scoped
out on that basis.

- **NFR-A1** — Keyboard navigation for HITL review surfaces is
  required, covering the 8-key shortcut set specified in §Project-
  Type Specific Requirements (Enter, Esc, Ctrl+Z, Shift+Enter,
  Tab, S, 1–9, J/K).
- **NFR-A2** — Broad WCAG 2.x compliance (screen reader support,
  color-contrast auditing, ARIA attribution) is explicitly out of
  scope for V1.
- **NFR-A3** — Mobile and tablet breakpoints are out of scope;
  HITL review is desktop-only.

### Compliance

These NFRs express threshold constraints that accompany the
compliance capabilities in §Functional Requirements and the
domain obligations in §Domain-Specific Requirements.

- **NFR-L1** — Every video produced by the pipeline must have an
  associated metadata bundle and source manifest before being
  marked "ready-for-upload" (throughput constraint on FR23).
  There is no bypass path.
- **NFR-L2** — Compliance artifacts (metadata bundles, source
  manifests) are version-controlled alongside video outputs in
  the same output directory tree.
- **NFR-L3** — The `provider` field recording which LLM served
  each generation is non-null for every row in `pipeline_runs`
  that represents an LLM call.
- **NFR-L4** — When a YouTube / platform policy change alters
  required disclosure fields, only the metadata bundle generator
  changes; pipeline behavior is unaffected. This is enforced by
  keeping the bundle as the sole platform-policy contract surface.
