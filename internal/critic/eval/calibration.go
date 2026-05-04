package eval

// CalibrationFloor is the inter-rater agreement floor for the v2 golden
// rubric per spec D5. Below this value the rubric is considered unstable
// and the spec mandates a HALT before locking in. The runner surfaces the
// floor breach as a warning in CalibrationSnapshot rather than a runtime
// error so a single bad run still produces an inspectable report.
const CalibrationFloor = 0.6

// CalibrationSnapshot is the kappa rerun result for one Golden run. The
// 2x2 contingency is constructed by collapsing each fixture into its
// expected verdict (binary: pass/retry from fixture metadata) and the
// evaluator's actual verdict, with `accept_with_notes` collapsed to
// `pass` for binary scoring per critic v2's verdict taxonomy.
type CalibrationSnapshot struct {
	// Observations is the row count in the contingency table. Each Golden
	// PairEntry contributes 2 rows (one positive fixture + one negative
	// fixture), so for N pair entries Observations == 2*N. Named
	// `Observations` not `Pairs` because operators reading the manifest
	// otherwise misread it as fixture-pair count.
	Observations int `json:"observations"`

	// AgreementPassPass is fixtures expected pass and graded pass.
	AgreementPassPass int `json:"agreement_pass_pass"`
	// DisagreementPassRetry is expected pass but graded retry (false reject).
	DisagreementPassRetry int `json:"disagreement_pass_retry"`
	// DisagreementRetryPass is expected retry but graded pass (missed neg).
	DisagreementRetryPass int `json:"disagreement_retry_pass"`
	// AgreementRetryRetry is expected retry and graded retry (true detect).
	AgreementRetryRetry int `json:"agreement_retry_retry"`

	// UnknownVerdicts counts evaluator outputs that did not match the
	// known v2 verdict taxonomy ({"pass", "retry", "accept_with_notes"}).
	// These are still counted in the contingency (collapsed to "pass" by
	// the v1-compat normalizer) so the kappa output isn't gappy, but the
	// counter surfaces evaluator-bug noise so a reviewer can tell whether
	// FloorOK reflects rubric stability or evaluator instability.
	UnknownVerdicts int `json:"unknown_verdicts"`

	// Kappa is unweighted binary Cohen's kappa over the 2x2 above. nil when
	// degenerate (no observations or no variance).
	Kappa *float64 `json:"kappa,omitempty"`

	// FloorOK is true when kappa is computable and >= CalibrationFloor.
	// False when kappa is nil OR below the floor — the spec's "HALT before
	// locking the v2 rubric" gate.
	FloorOK bool `json:"floor_ok"`

	// Reason is set only when Kappa is nil — the human-readable cause
	// (e.g., "no paired observations", "degenerate — no variance").
	Reason string `json:"reason,omitempty"`
}

// computeCalibration builds the 2x2 contingency for a Golden run and
// returns the kappa snapshot. binary verdict normalization:
//
//	pass / accept_with_notes / "" / unknown -> "pass"
//	retry                                   -> "retry"
//
// Empty/unknown evaluator verdicts are intentionally collapsed to "pass"
// (the optimistic side) because the only way an unknown verdict would
// flip the kappa picture is by silently treating it as a retry — that
// would mask evaluator bugs as false rejections. ShadowResult.FalseRejection
// in 4.2 already tightens unknown-verdict handling on its end; here the
// expected/actual asymmetry is benign because a positive fixture's
// expected is "pass" and a negative's is "retry".
func computeCalibration(pairResults []PairResult) CalibrationSnapshot {
	snap := CalibrationSnapshot{}
	// pairResults emit (NegVerdict, PosVerdict) for each pair index. We
	// flatten into a {expected, actual} list of 2*N rows.
	for _, p := range pairResults {
		if !isKnownVerdict(p.PosVerdict) {
			snap.UnknownVerdicts++
		}
		if !isKnownVerdict(p.NegVerdict) {
			snap.UnknownVerdicts++
		}
		// Positive fixture: expected = pass.
		if normalizeBinaryVerdict(p.PosVerdict) == "retry" {
			snap.DisagreementPassRetry++
		} else {
			snap.AgreementPassPass++
		}
		// Negative fixture: expected = retry.
		if normalizeBinaryVerdict(p.NegVerdict) == "retry" {
			snap.AgreementRetryRetry++
		} else {
			snap.DisagreementRetryPass++
		}
	}

	a := snap.AgreementPassPass
	b := snap.DisagreementPassRetry
	c := snap.DisagreementRetryPass
	d := snap.AgreementRetryRetry
	snap.Observations = a + b + c + d

	kappa, ok, reason := cohensKappa(a, b, c, d)
	if ok {
		snap.Kappa = &kappa
		snap.FloorOK = kappa >= CalibrationFloor
	} else {
		snap.Reason = reason
		snap.FloorOK = false
	}
	return snap
}

func normalizeBinaryVerdict(v string) string {
	switch v {
	case "retry":
		return "retry"
	default:
		return "pass"
	}
}

// isKnownVerdict reports whether v is in the v2 critic verdict taxonomy.
// Anything else (empty string, typo, unknown future value) collapses to
// "pass" via normalizeBinaryVerdict but is counted in
// CalibrationSnapshot.UnknownVerdicts so a reviewer can tell evaluator
// noise from rubric instability.
func isKnownVerdict(v string) bool {
	switch v {
	case "pass", "retry", "accept_with_notes":
		return true
	default:
		return false
	}
}

// cohensKappa computes unweighted binary Cohen's kappa over the 2x2
// table:
//
//	a = expected pass + actual pass
//	b = expected pass + actual retry
//	c = expected retry + actual pass
//	d = expected retry + actual retry
//
// Algorithm matches internal/service.CohensKappa byte-for-byte; we
// duplicate it here because internal/critic/eval may not import
// internal/service per scripts/lintlayers/main.go (the eval package is
// allowed only internal/domain + internal/clock).
func cohensKappa(a, b, c, d int) (kappa float64, ok bool, reason string) {
	n := a + b + c + d
	if n == 0 {
		return 0, false, "no paired observations"
	}
	nf := float64(n)
	po := float64(a+d) / nf
	pyes := float64((a+b)*(a+c)) / (nf * nf)
	pno := float64((c+d)*(b+d)) / (nf * nf)
	pe := pyes + pno
	if pe == 1.0 {
		return 0, false, "degenerate — no variance to calibrate against"
	}
	return (po - pe) / (1 - pe), true, ""
}
