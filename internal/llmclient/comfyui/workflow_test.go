package comfyui

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestValidateWorkflow_T2IEmbedded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	if err := validateWorkflow(WorkflowT2I, false); err != nil {
		t.Fatalf("validate t2i: %v", err)
	}
}

func TestValidateWorkflow_EditEmbedded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	if err := validateWorkflow(WorkflowEdit, true); err != nil {
		t.Fatalf("validate edit: %v", err)
	}
}

func TestValidateWorkflow_RejectsMissingLabel(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	mutated := removeLabel(t, WorkflowT2I, "POSITIVE_PROMPT")
	err := validateWorkflow(mutated, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "POSITIVE_PROMPT") {
		t.Fatalf("error must name missing label, got %v", err)
	}
}

func TestValidateWorkflow_RejectsClassTypeMismatch(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	mutated := mutateClassType(t, WorkflowT2I, "KSAMPLER", "TotallyDifferentNode")
	err := validateWorkflow(mutated, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestValidateWorkflow_RejectsDuplicateLabel(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	mutated := duplicateLabel(t, WorkflowT2I, "OUTPUT_IMAGE")
	err := validateWorkflow(mutated, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestValidateWorkflow_RejectsMissingReferenceWhenRequired(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	if err := validateWorkflow(WorkflowT2I, true); err == nil {
		t.Fatal("expected error: t2i has no REFERENCE_IMAGE label")
	}
}

func TestPrepareWorkflow_T2IDeepCopiesAndSubstitutes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	original := append([]byte(nil), WorkflowT2I...)

	encoded, outputID, err := prepareWorkflow(WorkflowT2I, substitution{
		Prompt: "a test prompt",
		Width:  2688,
		Height: 1536,
		Seed:   1234567,
	})
	if err != nil {
		t.Fatalf("prepareWorkflow: %v", err)
	}

	// Embedded byte slice is unchanged (deep copy semantics).
	if !bytes.Equal(WorkflowT2I, original) {
		t.Fatal("prepareWorkflow mutated the embedded byte slice")
	}

	if outputID == "" {
		t.Fatal("outputID is empty")
	}

	var graph map[string]map[string]any
	if err := json.Unmarshal(encoded, &graph); err != nil {
		t.Fatalf("decode encoded workflow: %v", err)
	}
	prompt := lookupValue(t, graph, "POSITIVE_PROMPT", "value")
	if prompt != "a test prompt" {
		t.Fatalf("POSITIVE_PROMPT.inputs.value = %v, want %q", prompt, "a test prompt")
	}
	w := lookupValue(t, graph, "LATENT_WIDTH", "value")
	if asInt(w) != 2688 {
		t.Fatalf("LATENT_WIDTH = %v, want 2688", w)
	}
	h := lookupValue(t, graph, "LATENT_HEIGHT", "value")
	if asInt(h) != 1536 {
		t.Fatalf("LATENT_HEIGHT = %v, want 1536", h)
	}
	seed := lookupValue(t, graph, "KSAMPLER", "noise_seed")
	if asInt64(seed) != 1234567 {
		t.Fatalf("KSAMPLER noise_seed = %v, want 1234567", seed)
	}
}

func TestPrepareWorkflow_EditSubstitutesReferenceAndCLIPText(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	encoded, outputID, err := prepareWorkflow(WorkflowEdit, substitution{
		Prompt:             "edit prompt",
		Width:              2688,
		Height:             1536,
		Seed:               42,
		ReferenceImageName: "ref-test.png",
		RequireReference:   true,
	})
	if err != nil {
		t.Fatalf("prepareWorkflow edit: %v", err)
	}
	if outputID == "" {
		t.Fatal("outputID empty")
	}

	var graph map[string]map[string]any
	if err := json.Unmarshal(encoded, &graph); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Edit workflow has POSITIVE_PROMPT bound to CLIPTextEncode (inputs.text).
	got := lookupValue(t, graph, "POSITIVE_PROMPT", "text")
	if got != "edit prompt" {
		t.Fatalf("POSITIVE_PROMPT.inputs.text = %v, want %q", got, "edit prompt")
	}
	ref := lookupValue(t, graph, "REFERENCE_IMAGE", "image")
	if ref != "ref-test.png" {
		t.Fatalf("REFERENCE_IMAGE.inputs.image = %v, want \"ref-test.png\"", ref)
	}
}

func TestPrepareWorkflow_RejectsMissingLabel(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	mutated := removeLabel(t, WorkflowT2I, "KSAMPLER")
	_, _, err := prepareWorkflow(mutated, substitution{Prompt: "p", Width: 1, Height: 1, Seed: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// helpers ----------------------------------------------------------------

func removeLabel(t *testing.T, raw []byte, label string) []byte {
	t.Helper()
	var graph map[string]map[string]any
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, node := range graph {
		meta, _ := node["_meta"].(map[string]any)
		if meta == nil {
			continue
		}
		if title, _ := meta["title"].(string); title == label {
			delete(meta, "title")
		}
	}
	out, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func mutateClassType(t *testing.T, raw []byte, label, newClass string) []byte {
	t.Helper()
	var graph map[string]map[string]any
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, node := range graph {
		meta, _ := node["_meta"].(map[string]any)
		if meta == nil {
			continue
		}
		if title, _ := meta["title"].(string); title == label {
			node["class_type"] = newClass
		}
	}
	out, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func duplicateLabel(t *testing.T, raw []byte, label string) []byte {
	t.Helper()
	var graph map[string]map[string]any
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Pick the first non-target node and overwrite its title with label.
	for id, node := range graph {
		meta, _ := node["_meta"].(map[string]any)
		if meta == nil {
			meta = map[string]any{}
			node["_meta"] = meta
		}
		if title, _ := meta["title"].(string); title == label {
			continue
		}
		meta["title"] = label
		_ = id
		break
	}
	out, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func lookupValue(t *testing.T, graph map[string]map[string]any, label, field string) any {
	t.Helper()
	for _, node := range graph {
		meta, _ := node["_meta"].(map[string]any)
		if meta == nil {
			continue
		}
		title, _ := meta["title"].(string)
		if title != label {
			continue
		}
		inputs, _ := node["inputs"].(map[string]any)
		if inputs == nil {
			t.Fatalf("label %q has no inputs map", label)
		}
		v, ok := inputs[field]
		if !ok {
			t.Fatalf("label %q has no inputs.%s", label, field)
		}
		return v
	}
	t.Fatalf("label %q not found", label)
	return nil
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	}
	return 0
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	}
	return 0
}
