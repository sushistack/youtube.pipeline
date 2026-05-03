# Next session — Structure narrative roles (hook → tension → reveal → bridge)

Run with `/bmad-quick-dev`. A draft spec already exists at
[`_bmad-output/implementation-artifacts/spec-structure-narrative-roles.md`](../implementation-artifacts/spec-structure-narrative-roles.md)
with `status: draft`. Step-01 will detect it and route to step-02 to resume planning.
**Read that spec first** — this file is just the orientation for whoever picks it up.

---

## Why this work exists

Dogfood after the prior `next-session-enhance-prompts.md` cycle showed three problems
with the generated SCP scripts:

1. **No 기승전결 arc.** Scripts read flat — no clear hook → tension → reveal →
   bridge progression.
2. **Uniform pacing.** Every act gets the same scene budget and the same 220-rune
   narration cap, so the climactic act can't breathe and the cold-open hook
   rambles.
3. **Visuals don't match the narration.** (Deferred — see
   [`next-session-visual-breakdown-alignment.md`](next-session-visual-breakdown-alignment.md).
   Do not pull this into the current cycle.)

Root cause for problems 1 + 2 is a single file:
[`internal/pipeline/agents/structurer.go`](../../internal/pipeline/agents/structurer.go).
It assigns dramatic beats to acts by `index % 4` and uses a fixed `scenesPerBeat = 2`
constant, so the writer receives placeholder synopses and uniform budgets — there
is no narrative intent in the data flowing into the writer prompt.

---

## User-confirmed decisions (do not re-litigate)

These were decided 2026-05-03 in the planning conversation and locked into the spec.
The new session should treat them as fixed:

1. **Role classification is LLM-based.** Researcher gains one extra LLM call (the
   "role classifier") that labels each beat with one of `hook | tension | reveal |
   bridge`. The classifier piggybacks on `WriterModel` / `WriterProvider` config —
   no new model defaults, no new `.env` keys.
2. **No fallback to broken modulo.** The current round-robin assignment is exactly
   the bug being fixed; falling back to it on classifier failure would emit a
   broken arc silently. So:
   - Classifier prompt mandates "every role must appear ≥1 time across the
     classifications."
   - Validation rejects: malformed JSON, beat-count mismatch, duplicate indices,
     unknown role tokens, **or any of the four roles missing**.
   - Retry budget = 3 (1 attempt + 2 retries).
   - Budget exhausted → return `domain.ErrStageFailed`. Run fails. Operator
     decides whether to re-trigger.
   - Old `assignedBeatIDs` and `scenesPerBeat = 2` are **deleted**, not preserved.
3. **Single code path, no feature flags.** Jay is the sole operator; only one
   behavior should exist. Do not add `use_role_based_assignment` /
   `use_act_specific_pacing` flags or any other toggle. The original input spec
   suggested flags but Jay overrode that on review — dead-layer policy applies.
4. **SourceVersion bump to `v1.2-roles`.** Existing Phase A caches invalidate; first
   run after merge re-derives researcher and structurer outputs. No multi-version
   handling — just one bump.
5. **Per-act constants live in domain.** `internal/domain/scenario.go` holds:
   - `ActScenesPerBeat` map: `{incident:1, mystery:2, revelation:3, unresolved:1}`
   - `ActNarrationRuneCap` map: `{incident:100, mystery:220, revelation:320, unresolved:180}`
   - Korean role labels: `{hook:"흥미로운 상황", tension:"급박한 상황",
     reveal:"SCP 설명", bridge:"부연 / 다른 SCP와의 관계"}`
6. **Fixtures are overwritten, not split.** No "OFF mode regression" fixture —
   `testdata/contracts/researcher_output.sample.json` and
   `testdata/contracts/structurer_output.sample.json` are regenerated from the
   SCP-TEST corpus with the new code and committed.
7. **Out-of-scope (do not touch):** the visual_breakdown stage; the writer's
   per-act LLM call count (still one); critic rubric thresholds; any dirty file in
   `internal/api/`, `internal/service/`, `web/`, or the untracked
   `internal/pipeline/engine_cancel*.go` (those are Jay's other in-progress work
   kept uncommitted by his commit-scope policy).

---

## Territory map (where the work lives)

- `internal/domain/scenario.go` — extend types + add maps + bump SourceVersion.
- `internal/pipeline/agents/researcher.go` — wire the classifier call with retry
  + balanced-role validation.
- `docs/prompts/scenario/01_5_role_classifier.md` (NEW) — the classifier prompt.
- `internal/pipeline/agents/structurer.go` — replace round-robin with role-based
  assignment; **delete** old code.
- `internal/pipeline/agents/writer.go` — `validateWriterActResponse` reads cap
  from `domain.ActNarrationRuneCap[spec.Act.ID]`.
- `cmd/pipeline/serve.go` — wire the new TextAgentConfig for researcher.
- `testdata/contracts/{researcher,structurer}_output.{schema,sample}.json` — schema
  + sample fixture updates.

---

## Acceptance signals (what "done" looks like)

- Healthy classifier run on SCP-TEST: act 1 holds only `hook` beats, act 2 only
  `tension`, etc.; synopses begin with `[ROLE: <korean>]`; act budgets reflect
  per-act multipliers.
- All-retries-fail case (stub a broken classifier in tests): researcher returns
  `domain.ErrStageFailed`; **no degraded structurer output emitted**.
- `grep -rn "scenesPerBeat\|assignedBeatIDs" internal/` returns zero matches (old
  code fully deleted, not merely unreached).
- Writer 320-rune narration: passes in revelation, rejects in incident.
- `go test ./...` clean.
- One end-to-end Phase A on SCP-173 with the new code: audit log shows one extra
  `text_generation` event labeled `role_classifier`; resulting structurer JSON has
  `role` populated on every act.

---

## Spec checkpoint state at last session

The spec passed step-01 (route = plan-code-review) and step-02 drafting. Token
count was ~2200-3200 (over the 1600 advisory limit) but the goal is genuinely
single-and-indivisible (role classification, role-based assignment, and per-act
pacing must ship together or the intermediate state is broken). Jay was about to
choose `[K] keep full spec` at CHECKPOINT 1 when this hand-off was written.

The new session should re-enter step-02, re-read the draft spec, give the user a
quick refresh of the CHECKPOINT 1 options (K / S / T), then continue from there.
