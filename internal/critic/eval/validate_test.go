package eval

import (
	"encoding/json"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestValidateFixture_PositiveSampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	raw := testutil.LoadFixture(t, "contracts/golden_eval_fixture.sample.positive.json")
	f, err := ValidateFixture(root, raw)
	if err != nil {
		t.Fatalf("expected positive sample to validate, got: %v", err)
	}
	testutil.AssertEqual(t, "positive", f.Kind)
	testutil.AssertEqual(t, "pass", f.ExpectedVerdict)
	testutil.AssertEqual(t, "post_writer", f.Checkpoint)
}

func TestValidateFixture_NegativeSampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	raw := testutil.LoadFixture(t, "contracts/golden_eval_fixture.sample.negative.json")
	f, err := ValidateFixture(root, raw)
	if err != nil {
		t.Fatalf("expected negative sample to validate, got: %v", err)
	}
	testutil.AssertEqual(t, "negative", f.Kind)
	testutil.AssertEqual(t, "retry", f.ExpectedVerdict)
}

func TestValidateFixture_RejectsInvalidInput(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	// Fixture whose outer envelope is valid but nested input fails writer_output.schema.json
	raw := []byte(`{
		"fixture_id": "bad-input-001",
		"kind": "positive",
		"checkpoint": "post_writer",
		"input": {"not_a_narration": true},
		"expected_verdict": "pass",
		"category": "known_pass"
	}`)
	_, err := ValidateFixture(root, raw)
	if err == nil {
		t.Fatal("expected validation error for invalid input field, got nil")
	}
}

func TestValidateFixture_RejectsMalformedJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	_, err := ValidateFixture(root, []byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestValidateFixture_RejectsKindVerdictMismatch_PositiveWithRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	raw := buildFixtureJSONWithKindVerdict(t, root, "positive", "retry")
	_, err := ValidateFixture(root, raw)
	if err == nil {
		t.Fatal("expected error for positive fixture with retry verdict, got nil")
	}
}

func TestValidateFixture_RejectsKindVerdictMismatch_NegativeWithPass(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	raw := buildFixtureJSONWithKindVerdict(t, root, "negative", "pass")
	_, err := ValidateFixture(root, raw)
	if err == nil {
		t.Fatal("expected error for negative fixture with pass verdict, got nil")
	}
}

func TestValidateFixture_RejectsUnknownCheckpoint(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	inputBytes := extractInputFromSample(t, "contracts/golden_eval_fixture.sample.positive.json")

	var envelope map[string]json.RawMessage
	envelope = map[string]json.RawMessage{
		"fixture_id":       json.RawMessage(`"bad-checkpoint-001"`),
		"kind":             json.RawMessage(`"positive"`),
		"checkpoint":       json.RawMessage(`"post_reviewer"`),
		"input":            inputBytes,
		"expected_verdict": json.RawMessage(`"pass"`),
		"category":         json.RawMessage(`"known_pass"`),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = ValidateFixture(root, raw)
	if err == nil {
		t.Fatal("expected error for unsupported checkpoint, got nil")
	}
}

func buildFixtureJSONWithKindVerdict(t testing.TB, root string, kind, verdict string) []byte {
	t.Helper()
	inputBytes := extractInputFromSample(t, "contracts/golden_eval_fixture.sample.positive.json")
	envelope := map[string]json.RawMessage{
		"fixture_id":       json.RawMessage(`"test-mismatch-001"`),
		"kind":             json.RawMessage(`"` + kind + `"`),
		"checkpoint":       json.RawMessage(`"post_writer"`),
		"input":            inputBytes,
		"expected_verdict": json.RawMessage(`"` + verdict + `"`),
		"category":         json.RawMessage(`"test"`),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}

func extractInputFromSample(t testing.TB, fixturePath string) json.RawMessage {
	t.Helper()
	raw := testutil.LoadFixture(t, fixturePath)
	var wrapper struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("extract input from %s: %v", fixturePath, err)
	}
	return wrapper.Input
}
