package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReviewReport_JSONRoundTrip(t *testing.T) {
	orig := sampleReviewReport()
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round ReviewReport
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(round, orig) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", round, orig)
	}
}

func TestReviewReport_JSONTagsSnakeCase(t *testing.T) {
	assertSnakeCaseJSONTags(t, reflect.TypeOf(ReviewReport{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(ReviewIssue{}))
	assertSnakeCaseJSONTags(t, reflect.TypeOf(ReviewCorrection{}))
}

func TestReviewReport_IssueConstantsStable(t *testing.T) {
	got := strings.Join([]string{
		ReviewIssueFactError,
		ReviewIssueMissingFact,
		ReviewIssueDescriptorViolation,
		ReviewIssueInventedContent,
		ReviewIssueConsistencyIssue,
	}, ",")
	want := "fact_error,missing_fact,descriptor_violation,invented_content,consistency_issue"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func sampleReviewReport() ReviewReport {
	return ReviewReport{
		OverallPass: true,
		CoveragePct: 92.5,
		Issues: []ReviewIssue{{
			SceneNum:    4,
			Type:        ReviewIssueDescriptorViolation,
			Severity:    "warning",
			Description: "Frozen descriptor is incomplete.",
			Correction:  "Restore the full Frozen Descriptor prefix.",
		}},
		Corrections: []ReviewCorrection{{
			SceneNum:  4,
			Field:     "visual_descriptor",
			Original:  "rough stone statue",
			Corrected: "Appearance: Concrete sentinel; Distinguishing features: cracks; Environment: chamber; rough stone statue",
		}},
		ReviewerModel:    "review-model",
		ReviewerProvider: "anthropic",
		SourceVersion:    ReviewSourceVersionV1,
	}
}
