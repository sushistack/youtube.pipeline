package comfyui

import _ "embed"

// WorkflowT2I is the validated FLUX.2 Klein 4B FP8 distilled text-to-image
// workflow exported from ComfyUI 0.12.3. It is embedded as immutable bytes;
// each Generate call deep-copies before label substitution.
//
//go:embed workflows/image_flux2_klein_text_to_image_4b_distilled.json
var WorkflowT2I []byte

// WorkflowEdit is the validated FLUX.2 Klein 4B FP8 distilled image-edit
// workflow exported from ComfyUI 0.12.3. It is embedded as immutable bytes;
// each Edit call deep-copies before label + reference-image substitution.
//
//go:embed workflows/image_flux2_klein_image_edit_4b_distilled.json
var WorkflowEdit []byte
