package comfyui

import (
	"encoding/json"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// Required label sets for the two workflow variants. Construction of an
// ImageClient validates that the embedded JSON contains every required label.
// A missing label is an operator error (workflow re-export omitted the
// `_meta.title` Annotation on a node) — fail-fast at server startup is the
// correct response.
const (
	labelPositivePrompt = "POSITIVE_PROMPT"
	labelLatentWidth    = "LATENT_WIDTH"
	labelLatentHeight   = "LATENT_HEIGHT"
	labelKSampler       = "KSAMPLER"
	labelReferenceImage = "REFERENCE_IMAGE"
	labelOutputImage    = "OUTPUT_IMAGE"
)

// Required class_types. The KSAMPLER label is wired to the RandomNoise node
// (the FLUX.2 sampler graph derives the seed from RandomNoise). Other labels
// have a single fixed class_type except POSITIVE_PROMPT, which has two valid
// shapes — see prepareWorkflow.
const (
	classRandomNoise            = "RandomNoise"
	classPrimitiveInt           = "PrimitiveInt"
	classPrimitiveStringML      = "PrimitiveStringMultiline"
	classCLIPTextEncode         = "CLIPTextEncode"
	classLoadImage              = "LoadImage"
	classSaveImage              = "SaveImage"
)

// validateWorkflow parses the embedded JSON to confirm every required label is
// present and bound to an expected class_type. Called once at construction
// (fail-fast). It does not mutate the source bytes.
func validateWorkflow(raw []byte, requireReference bool) error {
	if len(raw) == 0 {
		return fmt.Errorf("comfyui workflow: %w: embedded JSON is empty", domain.ErrValidation)
	}

	var graph map[string]json.RawMessage
	if err := json.Unmarshal(raw, &graph); err != nil {
		return fmt.Errorf("comfyui workflow: %w: parse: %v", domain.ErrValidation, err)
	}

	required := map[string][]string{
		labelPositivePrompt: {classPrimitiveStringML, classCLIPTextEncode},
		labelLatentWidth:    {classPrimitiveInt},
		labelLatentHeight:   {classPrimitiveInt},
		labelKSampler:       {classRandomNoise},
		labelOutputImage:    {classSaveImage},
	}
	if requireReference {
		required[labelReferenceImage] = []string{classLoadImage}
	}

	found := map[string]string{}
	for _, raw := range graph {
		var node nodeShape
		if err := json.Unmarshal(raw, &node); err != nil {
			return fmt.Errorf("comfyui workflow: %w: parse node: %v", domain.ErrValidation, err)
		}
		title := node.Meta.Title
		if title == "" {
			continue
		}
		// Only required-label duplicates are an error. Non-required titles
		// (e.g. ReferenceLatent x2 inside the edit graph's primitive widget
		// pattern) are workflow internals and must be ignored.
		if _, ok := required[title]; !ok {
			continue
		}
		if _, dup := found[title]; dup {
			return fmt.Errorf("comfyui workflow: %w: label %q appears more than once", domain.ErrValidation, title)
		}
		found[title] = node.ClassType
	}

	for label, allowedClasses := range required {
		got, ok := found[label]
		if !ok {
			return fmt.Errorf("comfyui workflow: %w: missing required label %q", domain.ErrValidation, label)
		}
		if !classMatches(got, allowedClasses) {
			return fmt.Errorf(
				"comfyui workflow: %w: label %q has class_type %q, expected one of %v",
				domain.ErrValidation, label, got, allowedClasses,
			)
		}
	}
	return nil
}

func classMatches(got string, allowed []string) bool {
	for _, c := range allowed {
		if c == got {
			return true
		}
	}
	return false
}

// nodeShape is the minimal portion of a ComfyUI node we need for label
// lookup and class_type assertion. The full shape carries `inputs` whose
// schema varies per class — substitution touches it as a generic map.
type nodeShape struct {
	ClassType string `json:"class_type"`
	Meta      struct {
		Title string `json:"title"`
	} `json:"_meta"`
}

// substitution carries the values to inject into a deep-copy of the workflow
// before submission. ReferenceImageName is empty for t2i.
type substitution struct {
	Prompt              string
	Width               int
	Height              int
	Seed                int64
	ReferenceImageName  string
	RequireReference    bool
}

// prepareWorkflow deep-copies the embedded workflow bytes, applies the
// substitution at every required label, and returns the JSON-encoded result
// plus the node ID of the OUTPUT_IMAGE node (used as the history outputs key).
//
// The original byte slice is never mutated — re-entrant safe.
func prepareWorkflow(raw []byte, sub substitution) ([]byte, string, error) {
	if len(raw) == 0 {
		return nil, "", fmt.Errorf("comfyui workflow: %w: embedded JSON is empty", domain.ErrValidation)
	}
	var graph map[string]map[string]any
	if err := json.Unmarshal(raw, &graph); err != nil {
		return nil, "", fmt.Errorf("comfyui workflow: %w: parse: %v", domain.ErrValidation, err)
	}

	// Build label → nodeID lookup. Duplicate detection is scoped to the
	// substitution-required labels — non-required titles like "ReferenceLatent"
	// can legitimately appear twice in the edit workflow's primitive widget
	// graph and must not block construction.
	requiredLabels := map[string]struct{}{
		labelPositivePrompt: {},
		labelLatentWidth:    {},
		labelLatentHeight:   {},
		labelKSampler:       {},
		labelOutputImage:    {},
	}
	if sub.RequireReference {
		requiredLabels[labelReferenceImage] = struct{}{}
	}
	labelToID := map[string]string{}
	for nodeID, node := range graph {
		meta, _ := node["_meta"].(map[string]any)
		if meta == nil {
			continue
		}
		title, _ := meta["title"].(string)
		if title == "" {
			continue
		}
		if _, isRequired := requiredLabels[title]; !isRequired {
			continue
		}
		if _, dup := labelToID[title]; dup {
			return nil, "", fmt.Errorf("comfyui workflow: %w: label %q appears more than once", domain.ErrValidation, title)
		}
		labelToID[title] = nodeID
	}

	// Substitute each required label.
	if err := setNodeValue(graph, labelToID, labelPositivePrompt,
		[]string{classPrimitiveStringML, classCLIPTextEncode},
		func(node map[string]any, class string) error {
			inputs := ensureInputs(node)
			switch class {
			case classPrimitiveStringML:
				inputs["value"] = sub.Prompt
			case classCLIPTextEncode:
				inputs["text"] = sub.Prompt
			}
			return nil
		}); err != nil {
		return nil, "", err
	}

	if err := setNodeValue(graph, labelToID, labelLatentWidth,
		[]string{classPrimitiveInt},
		func(node map[string]any, _ string) error {
			ensureInputs(node)["value"] = sub.Width
			return nil
		}); err != nil {
		return nil, "", err
	}

	if err := setNodeValue(graph, labelToID, labelLatentHeight,
		[]string{classPrimitiveInt},
		func(node map[string]any, _ string) error {
			ensureInputs(node)["value"] = sub.Height
			return nil
		}); err != nil {
		return nil, "", err
	}

	if err := setNodeValue(graph, labelToID, labelKSampler,
		[]string{classRandomNoise},
		func(node map[string]any, _ string) error {
			ensureInputs(node)["noise_seed"] = sub.Seed
			return nil
		}); err != nil {
		return nil, "", err
	}

	if sub.RequireReference {
		if err := setNodeValue(graph, labelToID, labelReferenceImage,
			[]string{classLoadImage},
			func(node map[string]any, _ string) error {
				ensureInputs(node)["image"] = sub.ReferenceImageName
				return nil
			}); err != nil {
			return nil, "", err
		}
	}

	// Validate OUTPUT_IMAGE presence and capture its node ID — used as the
	// history outputs key downstream.
	outputID, ok := labelToID[labelOutputImage]
	if !ok {
		return nil, "", fmt.Errorf("comfyui workflow: %w: missing required label %q", domain.ErrValidation, labelOutputImage)
	}
	outputNode := graph[outputID]
	class, _ := outputNode["class_type"].(string)
	if class != classSaveImage {
		return nil, "", fmt.Errorf("comfyui workflow: %w: label %q has class_type %q, expected %q",
			domain.ErrValidation, labelOutputImage, class, classSaveImage)
	}

	encoded, err := json.Marshal(graph)
	if err != nil {
		return nil, "", fmt.Errorf("comfyui workflow: encode: %w", err)
	}
	return encoded, outputID, nil
}

// setNodeValue locates a label and asserts class_type, then invokes mutate
// to write the substitution. Errors map to ErrValidation (variant detection).
func setNodeValue(
	graph map[string]map[string]any,
	labelToID map[string]string,
	label string,
	allowedClasses []string,
	mutate func(node map[string]any, class string) error,
) error {
	id, ok := labelToID[label]
	if !ok {
		return fmt.Errorf("comfyui workflow: %w: missing required label %q", domain.ErrValidation, label)
	}
	node, ok := graph[id]
	if !ok {
		return fmt.Errorf("comfyui workflow: %w: node %q referenced by label %q is missing", domain.ErrValidation, id, label)
	}
	class, _ := node["class_type"].(string)
	if !classMatches(class, allowedClasses) {
		return fmt.Errorf("comfyui workflow: %w: label %q has class_type %q, expected one of %v",
			domain.ErrValidation, label, class, allowedClasses)
	}
	return mutate(node, class)
}

func ensureInputs(node map[string]any) map[string]any {
	inputs, _ := node["inputs"].(map[string]any)
	if inputs == nil {
		inputs = map[string]any{}
		node["inputs"] = inputs
	}
	return inputs
}
