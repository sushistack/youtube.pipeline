---
title: 'D4 — critic v2 (consume ActScript[]; per-beat / per-paragraph checks)'
type: 'feature'
created: '2026-05-04'
status: 'draft'
context:
  - '_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md'
  - '_bmad-output/implementation-artifacts/spec-d1-domain-types-and-writer-v2.md'
  - '_bmad-output/implementation-artifacts/spec-d2-visual-breakdowner-v2.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** After D1/D2, critic still consumes `[]NarrationScene` via `LegacyScenes()`. Critic is the **last bridge consumer** — once D4 ships, the bridge is deleted in this same PR (per D1's Design Notes invariant: bridge dies with the last caller).

**Approach:** Rewrite critic to consume `[]ActScript`. Per-scene checks become per-beat or per-act-paragraph, depending on the check semantics. Forbidden-term scan continues to operate on text content (now monologue text instead of scene narration text) — semantics unchanged, input shape changed. Drop the `LegacyScenes()` bridge and the `NarrationScene` type from `narration.go` in the same commit as critic v2 — clean cut, zero callers.

## Boundaries & Constraints

**Always:**
- Forbidden-term scan operates on `ActScript.Monologue` text (whole-act). Per-act findings are aggregated into the run-level critic verdict.
- Per-act metadata gate (mood, key_points non-empty) carries from D1's writer validator into critic-time post-checks.
- Cycle-C critic config (commit `2ef1d3c` — max_tokens, timeout) carries unchanged. Critic still runs once per run; semantics unchanged, only signature changes (per plan out-of-scope: "Critic feedback loop redesign").
- After D4 lands, `NarrationScene` and `LegacyScenes()` are removed in this same PR. `git grep -n "NarrationScene\|LegacyScenes"` returns zero functional matches (only this spec's history may reference them).

**Ask First:**
- If a per-scene check semantically does NOT map to per-beat or per-paragraph (e.g., a v1 check whose only meaningful unit was scene-level visual continuity), HALT before deleting the check — clarify whether to retain at act-level, drop, or move to D2.
- If `LegacyScenes()` removal breaks a non-critic caller missed during D2 audit, HALT and treat as D2 scope leak (route back via Spec Change Log).

**Never:**
- No critic feedback loop redesign (plan out-of-scope).
- No per-beat critic feedback surfacing in HITL UI (plan defers Frontend rewrite to a separate cycle).
- No retention of `NarrationScene` for "future use" — clean cut.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error Handling |
|---|---|---|---|
| 4 acts, all checks pass | `state.Narration.Acts` v2 | critic verdict `pass`, `state.Critic.PostReviewer` populated | N/A |
| Forbidden-term hit in `acts[k].Monologue` | term match | verdict `retry` or `fail` per existing critic-verdict rules; finding cites act_id + rune offset of match | N/A |
| Per-act metadata empty (mood / key_points) | bad upstream | verdict `retry` with explicit field cited | N/A |
| Critic LLM truncation | provider over-cap | retry per cycle-C policy; on exhaustion fail | retry → escalate |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/critic.go` -- consume `[]ActScript`; rewrite per-scene check loops as per-beat / per-act-paragraph; forbidden-term scan retargeted to `Monologue` text.
- `internal/pipeline/agents/critic_test.go` -- rewrite end-to-end; preserve cycle-C truncation-retry coverage.
- `docs/prompts/scenario/04_critic.md` (or wherever critic prompt lives) -- rewrite for v2 input shape.
- `testdata/contracts/critic_*.{schema,sample}.json` -- rewrite for v2 input.
- `internal/domain/narration.go` -- DELETE `NarrationScene`, `LegacyScenes()`, and any v1 helpers gated by them.
- All other callers of `NarrationScene` (audit during planning) -- migrate or delete.

## Tasks & Acceptance

**Execution:**
- [ ] Audit `git grep -n "NarrationScene\|LegacyScenes"` — list all remaining callers. Each must be either migrated in D4 or already migrated by D2/D3.
- [ ] `internal/pipeline/agents/critic.go` -- consume `[]ActScript`, retarget forbidden-term scan to monologue text.
- [ ] `docs/prompts/scenario/04_critic.md` -- rewrite for v2 input.
- [ ] `testdata/contracts/critic_*.{schema,sample}.json` -- v2 rewrite.
- [ ] `internal/pipeline/agents/critic_test.go` -- rewrite end-to-end + I/O matrix coverage.
- [ ] `internal/domain/narration.go` -- delete `NarrationScene`, `LegacyScenes()`, `// Deprecated` markers.
- [ ] Final audit: re-run `git grep -n "NarrationScene\|LegacyScenes"` — expected zero functional matches.

**Acceptance Criteria:**
- Given clean repo on `feat/monologue-mode-v2` post-D1/D2/D3, when `go build ./...` runs, then it succeeds with `NarrationScene` and `LegacyScenes` fully removed.
- Given the same repo, when `go test ./...` and `go test -race ./...` run, then all green.
- Given an SCP-049 dogfood, when phase A reaches critic, then the critic verdict is computed against `state.Narration.Acts` directly (no `LegacyScenes()` call site exists).
- Given `git grep -n "NarrationScene\|LegacyScenes"` post-D4 commit, then output is empty (or limited to historical references in `_bmad-output/`).

## Design Notes

D4 is the bridge-removal milestone. If the audit step uncovers a stranded caller (e.g., assets.go, policy.go, a test fixture, a serve.go path), that caller's migration is in scope for D4 — not deferred. The bridge invariant from D1 ("dies with last caller") cannot be relaxed without flipping the bridge into durable cruft, which Jay's "no dead layers" memory forbids.

Forbidden-term scan retargeting is mechanical: v1 iterated `scenes[].Narration`, v2 iterates `acts[].Monologue`. Per-act offset reporting is more useful than v1's per-scene offset because it scales with audio playback (one act ≈ 2.5min of voice-over).

## Verification

**Commands:**
- `go build ./...` + `go test ./...` + `go test -race ./...`
- `git grep -n "NarrationScene\|LegacyScenes" -- ':!_bmad-output' ':!docs/scenarios'` -- expected: zero matches.
- SCP-049 phase-A dogfood through critic.

**Manual checks:**
- Inspect `state.Critic.PostReviewer.Findings` for v2 dogfood: each finding cites `act_id` + rune offset, not `scene_num`.
