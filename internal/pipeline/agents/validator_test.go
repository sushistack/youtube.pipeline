package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestValidator_Validate_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "researcher_output.schema.json")
	if err := v.Validate(sampleResearcherOutput()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidator_Validate_MissingRequired(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "researcher_output.schema.json")
	value := map[string]any{
		"title": "SCP-TEST",
	}
	err := v.Validate(value)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "scp_id") {
		t.Fatalf("error missing scp_id: %v", err)
	}
}

func TestValidator_Validate_WrongType(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "researcher_output.schema.json")
	value := map[string]any{
		"scp_id":                 "SCP-TEST",
		"title":                  "SCP-TEST",
		"object_class":           "Euclid",
		"physical_description":   "Stone",
		"anomalous_properties":   []string{"Moves"},
		"containment_procedures": "Watch it",
		"behavior_and_nature":    "Hostile",
		"origin_and_discovery":   "Unknown",
		"visual_identity": map[string]any{
			"appearance":              "Stone",
			"distinguishing_features": []string{"Cracks"},
			"environment_setting":     "Cell",
			"key_visual_moments":      []string{"It waits"},
		},
		"dramatic_beats":    "not-an-array",
		"main_text_excerpt": "Excerpt",
		"tags":              []string{"scp"},
		"source_version":    domain.SourceVersionV1,
	}
	err := v.Validate(value)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestValidator_NewValidator_SchemaNotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	_, err := NewValidator(testutil.ProjectRoot(t), "missing.schema.json")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestValidator_NewValidator_MalformedSchema(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	contractsDir := filepath.Join(root, "testdata", "contracts")
	if err := os.MkdirAll(contractsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contractsDir, "broken.schema.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	_, err := NewValidator(root, "broken.schema.json")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestContract_ResearcherOutput_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "researcher_output.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "researcher_output.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContract_StructurerOutput_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "structurer_output.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "structurer_output.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContract_WriterOutput_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "writer_output.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "writer_output.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContract_CriticPostWriter_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "critic_post_writer.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_writer.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContract_CriticPostReviewer_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "critic_post_reviewer.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_reviewer.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContract_VisualBreakdown_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "visual_breakdown.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "visual_breakdown.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContract_ReviewerReport_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "reviewer_report.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "reviewer_report.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestContract_PhaseAState_SampleValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	v := mustValidator(t, "phase_a_state.schema.json")
	var value any
	if err := json.Unmarshal(testutil.LoadFixture(t, filepath.Join("contracts", "phase_a_state.sample.json")), &value); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func mustValidator(t *testing.T, schema string) *Validator {
	t.Helper()
	v, err := NewValidator(testutil.ProjectRoot(t), schema)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return v
}

func sampleResearcherOutput() domain.ResearcherOutput {
	return domain.ResearcherOutput{
		SCPID:                 "SCP-TEST",
		Title:                 "SCP-TEST",
		ObjectClass:           "Euclid",
		PhysicalDescription:   "Stone sentinel",
		AnomalousProperties:   []string{"Moves when unwatched"},
		ContainmentProcedures: "Observe continuously",
		BehaviorAndNature:     "Predatory",
		OriginAndDiscovery:    "Recovered from a tunnel",
		VisualIdentity: domain.VisualIdentity{
			Appearance:             "Concrete statue",
			DistinguishingFeatures: []string{"Cracks"},
			EnvironmentSetting:     "Tunnel",
			KeyVisualMoments:       []string{"Blink", "Shadow"},
		},
		DramaticBeats: []domain.DramaticBeat{
			{Index: 0, Source: "visual_moment", Description: "Blink", EmotionalTone: "mystery"},
			{Index: 1, Source: "anomalous_property", Description: "Moves when unwatched", EmotionalTone: "horror"},
			{Index: 2, Source: "visual_moment", Description: "Shadow", EmotionalTone: "tension"},
		},
		MainTextExcerpt: "Excerpt",
		Tags:            []string{"scp"},
		SourceVersion:   domain.SourceVersionV1,
	}
}
