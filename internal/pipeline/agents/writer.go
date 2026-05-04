package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// TextAgentConfig is the shared per-agent configuration carrier. Both the
// stage-1 writer (qwen-max) and the stage-2 segmenter (qwen-plus) use this
// shape; the writer agent constructor takes two TextAgentConfig values.
type TextAgentConfig struct {
	Model       string
	Provider    string
	MaxTokens   int
	Temperature float64
	// Concurrency caps the number of in-flight LLM calls when an agent fans
	// out work in parallel (today: visual_breakdowner per-scene). Writer
	// stage 1 cascades serially (each act sees its actual predecessor's
	// monologue tail for canon Lever B). Stage 2 of act N can run as soon
	// as stage 1 of act N completes, but for D1 the simpler implementation
	// runs both stages of one act before moving on. Zero or negative falls
	// back to a per-agent default; sequential agents ignore this.
	Concurrency int
	AuditLogger domain.AuditLogger
	Logger      *slog.Logger
	// TraceWriter, when non-nil, receives one entry per LLM call attempt
	// with the rendered prompt, raw provider response, parsed output, and
	// per-attempt cost / latency / error. Retry-loop agents (writer,
	// visual_breakdowner) emit one entry per attempt so the operator can
	// see how a failed first attempt's output differed from the retry.
	// Nil → no traces written; the writer's own retry path is unchanged.
	TraceWriter domain.TraceWriter
}

// writerMonologueResponse is the stage-1 LLM response shape: a single
// continuous Korean monologue for one act, plus mood/key_points metadata
// the segmenter consumes.
type writerMonologueResponse struct {
	ActID     string   `json:"act_id"`
	Monologue string   `json:"monologue"`
	Mood      string   `json:"mood"`
	KeyPoints []string `json:"key_points"`
}

// writerSegmenterResponse is the stage-2 LLM response shape: 8–10 ordered
// BeatAnchor slices into the just-written monologue.
type writerSegmenterResponse struct {
	ActID string              `json:"act_id"`
	Beats []domain.BeatAnchor `json:"beats"`
}

// writerActSpec captures everything the writer needs to render and validate
// both stages for a single act.
type writerActSpec struct {
	Index int // 0..3 in domain.ActOrder
	Act   domain.Act
}

// actCallMeta carries the upstream response metadata (model/provider) for
// the metadata-fill step. Only the first successful stage-1 response's
// metadata is used (drift across acts is silently tolerated, matching v1).
type actCallMeta struct {
	resp domain.TextResponse
}

const (
	writerPerStageRetryBudget = 1   // one retry per stage on schema/validation failure
	priorActTailRuneCap       = 240 // bound on the prior-act monologue tail injected into the next act's stage-1 prompt
	beatCountMin              = 8   // plan resolution P2: 8–10 beats per act
	beatCountMax              = 10
)

// defaultAgentConcurrency is the fan-out cap when TextAgentConfig.Concurrency
// is unset for parallel agents.
const defaultAgentConcurrency = 4

// NewWriter constructs the v2 two-stage writer agent. Stage 1 (writerCfg,
// qwen-max in serve.go) writes the act monologue; stage 2 (segmenterCfg,
// qwen-plus) segments it into BeatAnchors. Per-act fan-out: stage 1 cascades
// serially (Act N sees Act N-1's monologue tail for continuity), and each
// act's stage 2 runs immediately after that act's stage 1 completes.
//
// gen drives stage 1; segmenterGen drives stage 2. Both must be non-nil.
// They may be the same client when WriterProvider == SegmenterProvider, but
// when the providers differ (e.g. deepseek writer + dashscope segmenter)
// passing one client routes the segmenter model to the wrong API and
// triggers a 400 from the receiving provider.
//
// Atomicity: state.Narration is set ONLY when every act of every stage
// succeeds. A partial result (some acts have monologue but no beats) NEVER
// persists. D6 owns resume implications.
func NewWriter(
	gen domain.TextGenerator,
	segmenterGen domain.TextGenerator,
	writerCfg TextAgentConfig,
	segmenterCfg TextAgentConfig,
	prompts PromptAssets,
	validator *Validator,
	terms *ForbiddenTerms,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state == nil:
			return fmt.Errorf("writer: %w: state is nil", domain.ErrValidation)
		case state.Research == nil:
			return fmt.Errorf("writer: %w: research is nil", domain.ErrValidation)
		case state.Structure == nil:
			return fmt.Errorf("writer: %w: structure is nil", domain.ErrValidation)
		case writerCfg.Model == "":
			return fmt.Errorf("writer: %w: stage-1 model is empty", domain.ErrValidation)
		case writerCfg.Provider == "":
			return fmt.Errorf("writer: %w: stage-1 provider is empty", domain.ErrValidation)
		case segmenterCfg.Model == "":
			return fmt.Errorf("writer: %w: stage-2 model is empty", domain.ErrValidation)
		case segmenterCfg.Provider == "":
			return fmt.Errorf("writer: %w: stage-2 provider is empty", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("writer: %w: stage-1 generator is nil", domain.ErrValidation)
		case segmenterGen == nil:
			return fmt.Errorf("writer: %w: stage-2 generator is nil", domain.ErrValidation)
		case validator == nil:
			return fmt.Errorf("writer: %w: validator is nil", domain.ErrValidation)
		case terms == nil:
			return fmt.Errorf("writer: %w: forbidden terms are nil", domain.ErrValidation)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		specs, err := planWriterActs(state.Structure)
		if err != nil {
			return err
		}

		qualityFeedback := state.PriorCriticFeedback

		acts := make([]domain.ActScript, len(specs))
		var firstMeta actCallMeta
		priorTail := ""
		for i, spec := range specs {
			monoResp, monoMeta, err := runWriterActMonologue(ctx, gen, writerCfg, prompts, terms, state, spec, priorTail, qualityFeedback)
			if err != nil {
				return err
			}
			if i == 0 {
				firstMeta = monoMeta
			}
			beatsResp, _, err := runWriterActBeats(ctx, segmenterGen, segmenterCfg, prompts, state, spec, monoResp)
			if err != nil {
				return err
			}
			acts[i] = domain.ActScript{
				ActID:     spec.Act.ID,
				Monologue: monoResp.Monologue,
				Beats:     beatsResp.Beats,
				Mood:      monoResp.Mood,
				KeyPoints: monoResp.KeyPoints,
			}
			priorTail = summarizePriorActMonologue(monoResp.Monologue)
		}

		title := state.Research.Title
		if title == "" {
			title = state.SCPID
		}
		script := domain.NarrationScript{
			SCPID: state.SCPID,
			Title: title,
			Acts:  acts,
		}
		fillNarrationMetadata(&script, firstMeta.resp, writerCfg, terms)
		if err := validator.Validate(script); err != nil {
			return fmt.Errorf("writer: %w", err)
		}
		hits := terms.MatchNarration(&script)
		if len(hits) > 0 {
			return fmt.Errorf("writer: %s: %w", formatForbiddenTermHits(hits), domain.ErrValidation)
		}

		state.Narration = &script
		return nil
	}
}

func planWriterActs(structure *domain.StructurerOutput) ([]writerActSpec, error) {
	if structure == nil || len(structure.Acts) != len(domain.ActOrder) {
		return nil, fmt.Errorf("writer: %w: structure must have %d acts", domain.ErrValidation, len(domain.ActOrder))
	}
	specs := make([]writerActSpec, len(structure.Acts))
	for i, act := range structure.Acts {
		if act.ID != domain.ActOrder[i] {
			return nil, fmt.Errorf("writer: %w: act %d id=%s, want %s", domain.ErrValidation, i, act.ID, domain.ActOrder[i])
		}
		specs[i] = writerActSpec{Index: i, Act: act}
	}
	return specs, nil
}

// runWriterActMonologue is stage 1: one LLM call (qwen-max) producing the
// continuous Korean monologue for `spec.Act`. Truncation retries are
// preserved per stage (`finish_reason=length` is retryable). Validator
// rejects act_id mismatch, missing monologue/mood/key_points, or rune
// count over the per-act cap.
func runWriterActMonologue(
	ctx context.Context,
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	terms *ForbiddenTerms,
	state *PipelineState,
	spec writerActSpec,
	priorTail string,
	qualityFeedback string,
) (writerMonologueResponse, actCallMeta, error) {
	prompt, err := renderWriterActPrompt(state, prompts, terms, spec, priorTail, qualityFeedback)
	if err != nil {
		return writerMonologueResponse{}, actCallMeta{}, err
	}

	type result struct {
		decoded writerMonologueResponse
		meta    actCallMeta
	}

	opts := retryOpts{
		Stage:  "writer_monologue",
		Budget: writerPerStageRetryBudget,
		Logger: cfg.Logger,
		BaseAttrs: []slog.Attr{
			slog.String("run_id", state.RunID),
			slog.String("act_id", spec.Act.ID),
		},
	}

	out, err := runWithRetry(ctx, opts, func(attempt int) (result, retryReason, error) {
		callStart := time.Now()
		if cfg.Logger != nil {
			cfg.Logger.Info("writer monologue attempt start",
				"run_id", state.RunID,
				"act_id", spec.Act.ID,
				"attempt", attempt,
				"provider", cfg.Provider,
				"model", cfg.Model,
				"prompt_chars", utf8.RuneCountInString(prompt),
			)
		}
		var (
			resp     domain.TextResponse
			parsed   any
			finalErr error
		)
		defer func() {
			emitAgentTrace(ctx, cfg, "writer_monologue", prompt, resp, parsed, "", finalErr, callStart)
		}()
		var err error
		resp, err = gen.Generate(ctx, domain.TextRequest{
			Prompt:      prompt,
			Model:       cfg.Model,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		})
		if err != nil {
			finalErr = err
			if cfg.Logger != nil {
				cfg.Logger.Error("writer monologue attempt failed",
					"run_id", state.RunID, "act_id", spec.Act.ID, "attempt", attempt,
					"duration_ms", time.Since(callStart).Milliseconds(), "error", err.Error())
			}
			return result{}, retryReasonAbort, err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("writer monologue attempt complete",
				"run_id", state.RunID, "act_id", spec.Act.ID, "attempt", attempt,
				"duration_ms", time.Since(callStart).Milliseconds(),
				"finish_reason", resp.FinishReason,
				"tokens_in", resp.TokensIn, "tokens_out", resp.TokensOut)
		}
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "writer_monologue",
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}
		if isTruncatedFinishReason(resp.FinishReason) {
			finalErr = fmt.Errorf(
				"writer: act %s monologue: provider truncated completion (finish_reason=%q): %w",
				spec.Act.ID, resp.FinishReason, domain.ErrValidation,
			)
			return result{}, retryReasonTruncation, finalErr
		}
		var decoded writerMonologueResponse
		if err := decodeJSONResponse(resp.Content, &decoded); err != nil {
			finalErr = fmt.Errorf("writer: act %s monologue: %w", spec.Act.ID, err)
			return result{}, retryReasonJSONDecode, finalErr
		}
		if err := validateWriterMonologueResponse(spec, decoded); err != nil {
			finalErr = err
			return result{}, retryReasonSchemaValidation, err
		}
		parsed = decoded
		return result{decoded: decoded, meta: actCallMeta{resp: resp}}, "", nil
	})
	if err != nil {
		return writerMonologueResponse{}, actCallMeta{}, err
	}
	return out.decoded, out.meta, nil
}

func validateWriterMonologueResponse(spec writerActSpec, decoded writerMonologueResponse) error {
	if decoded.ActID != spec.Act.ID {
		return fmt.Errorf("writer: act %s monologue: response act_id=%q: %w", spec.Act.ID, decoded.ActID, domain.ErrValidation)
	}
	if strings.TrimSpace(decoded.Monologue) == "" {
		return fmt.Errorf("writer: act %s monologue: empty: %w", spec.Act.ID, domain.ErrValidation)
	}
	if strings.TrimSpace(decoded.Mood) == "" {
		return fmt.Errorf("writer: act %s monologue: mood is empty: %w", spec.Act.ID, domain.ErrValidation)
	}
	if len(decoded.KeyPoints) == 0 {
		return fmt.Errorf("writer: act %s monologue: key_points is empty: %w", spec.Act.ID, domain.ErrValidation)
	}
	cap, ok := domain.ActMonologueRuneCap[spec.Act.ID]
	if !ok {
		return fmt.Errorf("writer: act %s monologue: no monologue cap configured: %w", spec.Act.ID, domain.ErrValidation)
	}
	if n := utf8.RuneCountInString(decoded.Monologue); n > cap {
		return fmt.Errorf("writer: act %s monologue: rune length=%d exceeds cap=%d: %w",
			spec.Act.ID, n, cap, domain.ErrValidation)
	}
	return nil
}

// runWriterActBeats is stage 2: one LLM call (qwen-plus) segmenting the
// just-written monologue into 8–10 BeatAnchors. Validator enforces the
// ordering / coverage / metadata rules.
func runWriterActBeats(
	ctx context.Context,
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	state *PipelineState,
	spec writerActSpec,
	mono writerMonologueResponse,
) (writerSegmenterResponse, actCallMeta, error) {
	prompt, err := renderSegmenterPrompt(state, prompts, spec, mono)
	if err != nil {
		return writerSegmenterResponse{}, actCallMeta{}, err
	}
	monologueRuneCount := utf8.RuneCountInString(mono.Monologue)

	type result struct {
		decoded writerSegmenterResponse
		meta    actCallMeta
	}

	opts := retryOpts{
		Stage:  "writer_segmenter",
		Budget: writerPerStageRetryBudget,
		Logger: cfg.Logger,
		BaseAttrs: []slog.Attr{
			slog.String("run_id", state.RunID),
			slog.String("act_id", spec.Act.ID),
		},
	}

	out, err := runWithRetry(ctx, opts, func(attempt int) (result, retryReason, error) {
		callStart := time.Now()
		if cfg.Logger != nil {
			cfg.Logger.Info("writer segmenter attempt start",
				"run_id", state.RunID, "act_id", spec.Act.ID, "attempt", attempt,
				"provider", cfg.Provider, "model", cfg.Model,
				"monologue_runes", monologueRuneCount)
		}
		var (
			resp     domain.TextResponse
			parsed   any
			finalErr error
		)
		defer func() {
			emitAgentTrace(ctx, cfg, "writer_segmenter", prompt, resp, parsed, "", finalErr, callStart)
		}()
		var err error
		resp, err = gen.Generate(ctx, domain.TextRequest{
			Prompt:      prompt,
			Model:       cfg.Model,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		})
		if err != nil {
			finalErr = err
			if cfg.Logger != nil {
				cfg.Logger.Error("writer segmenter attempt failed",
					"run_id", state.RunID, "act_id", spec.Act.ID, "attempt", attempt,
					"duration_ms", time.Since(callStart).Milliseconds(), "error", err.Error())
			}
			return result{}, retryReasonAbort, err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("writer segmenter attempt complete",
				"run_id", state.RunID, "act_id", spec.Act.ID, "attempt", attempt,
				"duration_ms", time.Since(callStart).Milliseconds(),
				"finish_reason", resp.FinishReason,
				"tokens_in", resp.TokensIn, "tokens_out", resp.TokensOut)
		}
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "writer_segmenter",
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}
		if isTruncatedFinishReason(resp.FinishReason) {
			finalErr = fmt.Errorf(
				"writer: act %s segmenter: provider truncated completion (finish_reason=%q): %w",
				spec.Act.ID, resp.FinishReason, domain.ErrValidation,
			)
			return result{}, retryReasonTruncation, finalErr
		}
		var decoded writerSegmenterResponse
		if err := decodeJSONResponse(resp.Content, &decoded); err != nil {
			finalErr = fmt.Errorf("writer: act %s segmenter: %w", spec.Act.ID, err)
			return result{}, retryReasonJSONDecode, finalErr
		}
		if err := validateWriterSegmenterResponse(spec, decoded, monologueRuneCount); err != nil {
			finalErr = err
			return result{}, retryReasonSchemaValidation, err
		}
		parsed = decoded
		return result{decoded: decoded, meta: actCallMeta{resp: resp}}, "", nil
	})
	if err != nil {
		return writerSegmenterResponse{}, actCallMeta{}, err
	}
	return out.decoded, out.meta, nil
}

func validateWriterSegmenterResponse(spec writerActSpec, decoded writerSegmenterResponse, monologueRuneCount int) error {
	if decoded.ActID != spec.Act.ID {
		return fmt.Errorf("writer: act %s segmenter: response act_id=%q: %w", spec.Act.ID, decoded.ActID, domain.ErrValidation)
	}
	n := len(decoded.Beats)
	if n < beatCountMin || n > beatCountMax {
		return fmt.Errorf("writer: act %s segmenter: beat count=%d outside [%d, %d]: %w",
			spec.Act.ID, n, beatCountMin, beatCountMax, domain.ErrValidation)
	}
	prevEnd := 0
	for i, beat := range decoded.Beats {
		if beat.StartOffset < 0 {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] start_offset=%d < 0: %w",
				spec.Act.ID, i, beat.StartOffset, domain.ErrValidation)
		}
		if beat.EndOffset > monologueRuneCount {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] end_offset=%d > monologue_rune_count=%d: %w",
				spec.Act.ID, i, beat.EndOffset, monologueRuneCount, domain.ErrValidation)
		}
		if beat.StartOffset >= beat.EndOffset {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] start_offset=%d >= end_offset=%d (zero or inverted slice): %w",
				spec.Act.ID, i, beat.StartOffset, beat.EndOffset, domain.ErrValidation)
		}
		if beat.StartOffset < prevEnd {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] start_offset=%d overlaps prev end_offset=%d: %w",
				spec.Act.ID, i, beat.StartOffset, prevEnd, domain.ErrValidation)
		}
		if strings.TrimSpace(beat.Mood) == "" {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] mood is empty: %w", spec.Act.ID, i, domain.ErrValidation)
		}
		if strings.TrimSpace(beat.Location) == "" {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] location is empty: %w", spec.Act.ID, i, domain.ErrValidation)
		}
		if len(beat.CharactersPresent) == 0 {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] characters_present is empty: %w", spec.Act.ID, i, domain.ErrValidation)
		}
		if strings.TrimSpace(beat.ColorPalette) == "" {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] color_palette is empty: %w", spec.Act.ID, i, domain.ErrValidation)
		}
		if strings.TrimSpace(beat.Atmosphere) == "" {
			return fmt.Errorf("writer: act %s segmenter: beat[%d] atmosphere is empty: %w", spec.Act.ID, i, domain.ErrValidation)
		}
		prevEnd = beat.EndOffset
	}
	return nil
}

func renderWriterActPrompt(
	state *PipelineState,
	prompts PromptAssets,
	terms *ForbiddenTerms,
	spec writerActSpec,
	priorTail string,
	qualityFeedback string,
) (string, error) {
	visualJSON, err := json.MarshalIndent(state.Research.VisualIdentity, "", "  ")
	if err != nil {
		return "", fmt.Errorf("writer: marshal visual identity: %w", domain.ErrValidation)
	}
	forbidden := renderForbiddenTermsSection(terms.Raw)
	keyPoints := renderKeyPoints(spec.Act.KeyPoints)
	exemplar, ok := prompts.ExemplarsByAct[spec.Act.ID]
	if !ok || exemplar == "" {
		return "", fmt.Errorf("writer: act %s: no exemplar narration available: %w", spec.Act.ID, domain.ErrValidation)
	}
	containment := strings.TrimSpace(state.Research.ContainmentProcedures)
	if containment == "" {
		containment = "(none specified — apply default Foundation containment conventions)"
	}
	cap, ok := domain.ActMonologueRuneCap[spec.Act.ID]
	if !ok {
		return "", fmt.Errorf("writer: act %s: no monologue cap configured: %w", spec.Act.ID, domain.ErrValidation)
	}
	priorBlock := priorTail
	if priorBlock == "" {
		priorBlock = "(이 act 가 첫 act 입니다 — origin-first 또는 incident-first 자유롭게 선택하세요.)"
	}
	replacer := strings.NewReplacer(
		"{scp_id}", state.SCPID,
		"{act_id}", spec.Act.ID,
		"{monologue_rune_cap}", strconv.Itoa(cap),
		"{act_synopsis}", spec.Act.Synopsis,
		"{act_key_points}", keyPoints,
		"{prior_act_summary}", priorBlock,
		"{scp_visual_reference}", string(visualJSON),
		"{containment_constraints}", containment,
		"{format_guide}", prompts.FormatGuide,
		"{forbidden_terms_section}", forbidden,
		"{glossary_section}", "",
		"{quality_feedback}", qualityFeedback,
		"{exemplar_scenes}", exemplar,
	)
	return replacer.Replace(prompts.WriterTemplate), nil
}

func renderSegmenterPrompt(
	state *PipelineState,
	prompts PromptAssets,
	spec writerActSpec,
	mono writerMonologueResponse,
) (string, error) {
	if prompts.SegmenterTemplate == "" {
		return "", fmt.Errorf("writer: act %s segmenter: empty template: %w", spec.Act.ID, domain.ErrValidation)
	}
	visualJSON, err := json.MarshalIndent(state.Research.VisualIdentity, "", "  ")
	if err != nil {
		return "", fmt.Errorf("writer: marshal visual identity: %w", domain.ErrValidation)
	}
	keyPoints := renderKeyPoints(mono.KeyPoints)
	factCatalog := renderFactTagCatalog(state.Research)
	monologueRuneCount := utf8.RuneCountInString(mono.Monologue)
	replacer := strings.NewReplacer(
		"{act_id}", spec.Act.ID,
		"{act_mood}", mono.Mood,
		"{act_key_points}", keyPoints,
		"{monologue}", mono.Monologue,
		"{monologue_rune_count}", strconv.Itoa(monologueRuneCount),
		"{scp_visual_reference}", string(visualJSON),
		"{fact_tag_catalog}", factCatalog,
	)
	return replacer.Replace(prompts.SegmenterTemplate), nil
}

func renderKeyPoints(points []string) string {
	if len(points) == 0 {
		return "- (none)"
	}
	lines := make([]string, 0, len(points))
	for _, p := range points {
		lines = append(lines, "- "+p)
	}
	return strings.Join(lines, "\n")
}

func renderForbiddenTermsSection(patterns []string) string {
	if len(patterns) == 0 {
		return "## Forbidden Terms\n- None"
	}
	lines := make([]string, 0, len(patterns)+1)
	lines = append(lines, "## Forbidden Terms")
	for _, pattern := range patterns {
		lines = append(lines, "- "+pattern)
	}
	return strings.Join(lines, "\n")
}

// renderFactTagCatalog distills the researcher output into the canonical
// fact-tag catalog the segmenter can reference. Listed as `key: content`
// lines so the LLM picks unique keys for `fact_tags[].key`.
func renderFactTagCatalog(r *domain.ResearcherOutput) string {
	if r == nil {
		return "(none)"
	}
	lines := []string{}
	if r.ObjectClass != "" {
		lines = append(lines, fmt.Sprintf("- object_class: %s", r.ObjectClass))
	}
	for i, prop := range r.AnomalousProperties {
		lines = append(lines, fmt.Sprintf("- anomaly_%d: %s", i+1, prop))
	}
	if r.OriginAndDiscovery != "" {
		lines = append(lines, fmt.Sprintf("- origin: %s", r.OriginAndDiscovery))
	}
	if len(lines) == 0 {
		return "(none)"
	}
	return strings.Join(lines, "\n")
}

// summarizePriorActMonologue condenses the previous act's monologue tail
// into a continuity hint for the next act's stage-1 prompt. Cap at
// priorActTailRuneCap so prompt cost stays bounded across the cascade.
func summarizePriorActMonologue(monologue string) string {
	tail := truncatePrompt(strings.TrimSpace(monologue), priorActTailRuneCap)
	if tail == "" {
		return ""
	}
	return fmt.Sprintf("이전 act 의 마지막 부분: %s\n\n이 톤·페이싱을 이어받으세요. 개체 재소개·hook 재사용 금지.", tail)
}

func isTruncatedFinishReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens":
		return true
	default:
		return false
	}
}

func truncatePrompt(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func fillNarrationMetadata(script *domain.NarrationScript, resp domain.TextResponse, cfg TextAgentConfig, terms *ForbiddenTerms) {
	if resp.Model == "" {
		resp.Model = cfg.Model
	}
	if resp.Provider == "" {
		resp.Provider = cfg.Provider
	}
	totalBeats := 0
	for _, act := range script.Acts {
		totalBeats += len(act.Beats)
	}
	script.Metadata = domain.NarrationMetadata{
		Language:              domain.LanguageKorean,
		SceneCount:            totalBeats,
		WriterModel:           resp.Model,
		WriterProvider:        resp.Provider,
		PromptTemplate:        filepath.Base(writerPromptPath),
		FormatGuideTemplate:   filepath.Base(formatGuidePath),
		ForbiddenTermsVersion: terms.Version,
	}
	script.SourceVersion = domain.NarrationSourceVersionV2
}
