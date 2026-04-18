package service

// CohensKappa computes Cohen's kappa for a 2x2 agreement table:
//   a = yes/yes, b = yes/no, c = no/yes, d = no/no.
func CohensKappa(a, b, c, d int) (kappa float64, ok bool, reason string) {
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
