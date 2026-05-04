package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// emitAgentTrace records one TraceEntry for an LLM call attempt. Best-
// effort: trace failures are logged through cfg.Logger (when present)
// but never propagated, so an LLM retry cannot be derailed by a trace
// I/O issue.
//
// stage is the canonical sub-stage name used by audit.log (e.g.
// "writer_monologue", "post_writer_critic"). prompt is the full
// rendered string with no truncation. resp is the provider response
// (may be zero on early call failures). parsed is the decoded domain
// output for the success path; pass nil when validation/decode failed
// or when the call returned an error. callErr is the closure-final
// error (nil on success). startedAt is the start of the attempt;
// LatencyMs is computed against time.Now() at emission.
//
// verdict is populated for critic-class stages where the response
// carries a typed pass/retry decision; pass "" for non-critic stages.
func emitAgentTrace(
	ctx context.Context,
	cfg TextAgentConfig,
	stage, prompt string,
	resp domain.TextResponse,
	parsed any,
	verdict string,
	callErr error,
	startedAt time.Time,
) {
	if cfg.TraceWriter == nil {
		return
	}
	entry := domain.TraceEntry{
		Stage:          stage,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		PromptRendered: prompt,
		Provider:       cfg.Provider,
		Model:          cfg.Model,
		ResponseRaw:    resp.Content,
		CostUSD:        resp.CostUSD,
		LatencyMs:      time.Since(startedAt).Milliseconds(),
		Verdict:        verdict,
	}
	// Provider/model may be overridden by the response if the provider
	// reflects an alias (DashScope normalizes some model strings).
	if resp.Provider != "" {
		entry.Provider = resp.Provider
	}
	if resp.Model != "" {
		entry.Model = resp.Model
	}
	if parsed != nil {
		if data, err := json.Marshal(parsed); err == nil {
			entry.ResponseParsed = data
		}
	}
	if callErr != nil {
		entry.Error = callErr.Error()
	}
	if err := cfg.TraceWriter.Write(ctx, entry); err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("agent trace write failed",
			"stage", stage, "error", err.Error())
	}
}
