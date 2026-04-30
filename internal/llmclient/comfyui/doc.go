// Package comfyui implements domain.ImageGenerator backed by a local ComfyUI
// 0.12.3 server running FLUX.2 Klein 4B (FP8 distilled). It mirrors the
// structural conventions of internal/llmclient/dashscope: the *http.Client and
// clock.Clock are constructor-injected, atomic temp+rename writes,
// ErrValidation/ErrRateLimited/ErrStageFailed/ErrUpstreamTimeout error
// taxonomy, and a compile-time guard.
//
// The HTTP surface is:
//
//	POST /prompt              — submit a workflow + client_id
//	GET  /history/{prompt_id} — poll for completion (250ms / 300s cap)
//	GET  /view                — fetch the rendered image bytes
//	POST /upload/image        — upload reference image (multipart) for edit
//
// Workflow JSON is embedded at compile time via go:embed and node identity is
// matched on `_meta.title` exact strings (never node IDs or class_type names),
// so an operator-led ComfyUI graph re-export does not silently break wiring.
// Each call deep-copies the embedded byte slice before substitution to keep
// the package re-entrant.
package comfyui
