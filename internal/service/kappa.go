package service

// CohensKappa computes unweighted binary Cohen's kappa (2x2 table):
//
//	a = Critic pass + operator approve
//	b = Critic pass + operator reject
//	c = Critic fail + operator approve
//	d = Critic fail + operator reject
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
