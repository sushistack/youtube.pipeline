---
title: 'D5 — golden eval rubric refit (per-act monologue scoring)'
type: 'feature'
created: '2026-05-04'
status: 'draft'
context:
  - '_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md'
  - '_bmad-output/implementation-artifacts/spec-d1-domain-types-and-writer-v2.md'
  - '_bmad-output/implementation-artifacts/4-2-shadow-eval-runner.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Golden eval rubric (`spec 4-2-shadow-eval-runner.md`) scores per-scene narration. After D1–D4, scenes don't exist as a runtime unit — `state.Narration.Acts[].Monologue` is the unit. v1 rubric run against v2 output is shape-mismatched and the scores are not comparable.

**Approach:** Refit the golden eval rubric to score per-act monologue. Reshape golden samples (`testdata/golden/eval/*`) to v2 (`ActScript[]`). Re-run a full golden eval pass on the v2 baseline (post-D4, polisher NOT yet in line per P5) and document the score. Verify v1→v2 score comparability before claiming improvement — if the rubric weights changed, the absolute number is not directly comparable to v1's last-cycle score; document the comparison method (e.g., normalized rank, per-criterion delta).

## Boundaries & Constraints

**Always:**
- Golden samples reshaped to v2 are stored alongside v1 samples in version-tagged directories (`testdata/golden/eval/v1/`, `testdata/golden/eval/v2/`) so v1's last-cycle score remains reproducible for the comparability claim.
- Per-act scoring criteria must remain semantically aligned with v1's per-scene criteria where possible. New criteria specific to v2 (e.g., "act-seam continuity", "monologue rune-cap utilization") are added as separate scored dimensions, not folded into existing v1 criteria.
- `manifest.json` bumps `version` to 2 with v2 samples set; v1 retained for archive.
- Calibration data (Cohen's kappa, see `4-3-cohens-kappa-calibration-tracking.md`) re-run on v2 sample set. Inter-rater agreement floor: ≥0.6 for v2 to be trusted.

**Ask First:**
- If kappa on v2 first pass falls below 0.6, HALT before locking the v2 rubric — the criteria are unstable.
- If reshape from v1 sample to v2 sample requires inventing per-act monologue text not derivable from v1 per-scene narration concatenation, HALT and confirm the synthesis rule.
- If v1→v2 score comparison shows v2 < v1 by ≥10 points, HALT and route to a D plan retrospective — D may not have delivered.

**Never:**
- No deletion of v1 golden samples — archive only.
- No silent rubric weight changes — every weight delta is documented in the spec change log.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error Handling |
|---|---|---|---|
| v2 rubric run on v2 baseline | `testdata/golden/eval/v2/`, post-D4 dogfood | per-act scores + run-level aggregate; manifest updated | N/A |
| Kappa < 0.6 on first calibration | shaky criteria | HALT, do not lock rubric | manual triage |
| v2 sample reshape produces empty `acts[].Monologue` for an SCP | bad reshape | reshape pipeline fails loud | `ErrValidation` |

</frozen-after-approval>

## Code Map

- `_bmad-output/implementation-artifacts/4-2-shadow-eval-runner.md` -- refresh with v2 rubric description (this spec's refit lives here in implementation-artifact form).
- `internal/eval/...` (or wherever shadow eval runner lives) -- per-act scoring code path.
- `testdata/golden/eval/manifest.json` -- bump `version` to 2; reference v2 samples.
- `testdata/golden/eval/v1/` -- archive existing samples here (move not delete).
- `testdata/golden/eval/v2/` -- new reshaped samples (ActScript[] shape).
- `internal/eval/calibration.go` (or similar) -- kappa rerun on v2 samples.

## Tasks & Acceptance

**Execution:**
- [ ] Move existing `testdata/golden/eval/*.json` (other than manifest) into `testdata/golden/eval/v1/`.
- [ ] Reshape v1 samples → v2 samples (per-scene narration concatenated within act → `acts[].Monologue`; preserve fact_tags / mood / key_points).
- [ ] `internal/eval/...` -- per-act scoring path; v1 path retained as archive-mode (read-only against `v1/` dir).
- [ ] `testdata/golden/eval/manifest.json` -- bump version + v2 sample references.
- [ ] First v2 calibration run; record kappa.
- [ ] First v2 baseline scoring run on post-D4 dogfood; record score.
- [ ] Document v1→v2 score comparability method in `4-2-shadow-eval-runner.md` refresh.

**Acceptance Criteria:**
- Given clean repo post-D4, when `go build ./...` and `go test ./...` run, then all green.
- Given the v2 rubric calibration run, when kappa is computed, then kappa ≥ 0.6 across all per-act criteria.
- Given v1's last-cycle score and v2's first baseline score, when the comparability method is applied, then either (a) v2 score ≥ v1 + 15 absolute points (per plan acceptance signal #6), or (b) v2 score < v1 + 5 → D retrospective triggered per plan.
- Given `testdata/golden/eval/v1/`, when v1 archive scoring is invoked, then v1 last-cycle score remains reproducible byte-identical.

## Design Notes

D5 is the only story where "verify before claim" is the hardest constraint. The plan flags the failure mode explicitly: "this may reveal that golden samples themselves were scored against a v1-shaped rubric — verify before claiming the new scores are comparable." The comparability method is documented in `4-2-shadow-eval-runner.md` as part of D5's deliverable, not assumed. Per-criterion delta is the safest comparison; absolute aggregate is allowed only if rubric weights are unchanged.

Per plan resolution P5, the v2 baseline measured here is **without polisher in line**. D7 (polisher v2, separate cycle) will re-measure on top of this baseline to justify its existence.

## Verification

**Commands:**
- `go build ./...` + `go test ./...`
- v2 calibration run (CLI to be confirmed during planning) -- kappa ≥ 0.6.
- v2 baseline scoring run -- record + compare to v1.

**Manual checks:**
- Compare v1→v2 score per-criterion deltas in the runner refresh document. Flag any criterion where v2 < v1 for retrospective.
