package domain

import (
	"context"
	"encoding/json"
)

// TraceEntry captures one LLM call attempt with the full rendered prompt,
// raw provider response, and parsed output. Used for offline prompt
// debugging — fully untruncated, in contrast to AuditEntry which truncates
// Prompt to 2048 runes for log-size discipline.
//
// One file per attempt under {runDir}/traces/{stage}.{NNN}.json. Retry
// loops inside agents (writer, visual_breakdowner) produce multiple
// files for one logical call so the operator can compare attempts.
//
// The interface lives in domain so agents (which import domain) can
// emit trace entries without an import cycle through pipeline.
type TraceEntry struct {
	Stage             string          `json:"stage"`
	AttemptNum        int             `json:"attempt_num"` // assigned by writer; callers leave 0
	Timestamp         string          `json:"timestamp"`   // RFC3339Nano
	PromptTemplateSHA string          `json:"prompt_template_sha,omitempty"`
	PromptRendered    string          `json:"prompt_rendered"`
	Provider          string          `json:"provider"`
	Model             string          `json:"model"`
	RequestPayload    json.RawMessage `json:"request_payload,omitempty"`
	ResponseRaw       string          `json:"response_raw,omitempty"`
	ResponseParsed    json.RawMessage `json:"response_parsed,omitempty"`
	CostUSD           float64         `json:"cost_usd,omitempty"`
	LatencyMs         int64           `json:"latency_ms,omitempty"`
	Verdict           string          `json:"verdict,omitempty"`
	Error             string          `json:"error,omitempty"`
}

// TraceWriter is the port for recording per-attempt LLM traces. The
// interface lives in domain (mirrors AuditLogger pattern) so agents can
// emit traces without depending on pipeline.
//
// RunID routing: implementations source the run ID from a context value
// set by pipeline.WithTraceRunID at the top of Phase A. This keeps the
// agent contract free of an extra parameter on every Write call.
type TraceWriter interface {
	Write(ctx context.Context, entry TraceEntry) error
}
