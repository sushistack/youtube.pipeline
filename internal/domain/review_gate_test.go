package domain

import "testing"

func TestReviewStatus_IsValidSet(t *testing.T) {
	valid := []ReviewStatus{
		ReviewStatusPending,
		ReviewStatusWaitingForReview,
		ReviewStatusAutoApproved,
		ReviewStatusApproved,
		ReviewStatusRejected,
	}
	for _, status := range valid {
		if !status.IsValid() {
			t.Fatalf("expected %q to be valid", status)
		}
	}
	if ReviewStatus("bogus").IsValid() {
		t.Fatal("expected bogus status to be invalid")
	}
}
