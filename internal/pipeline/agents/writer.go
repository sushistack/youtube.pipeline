package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type TextAgentConfig struct {
	Model       string
	Provider    string
	MaxTokens   int
	Temperature float64
	// Concurrency caps the number of in-flight LLM calls when an agent fans
	// out work in parallel (today: visual_breakdowner per-scene). Writer is
	// fully serial across acts (cascade requires each act to see its actual
	// predecessor's narration tail), so the writer ignores this value.
	// Zero or negative falls back to a per-agent default; sequential agents
	// ignore it.
	Concurrency int
	// AuditLogger is the optional audit logger. When non-nil, text_generation
	// audit entries are written after each successful Generate call. Nil is
	// allowed (no-op guard) — all existing tests continue to pass without
	// modification because they construct TextAgentConfig without this field.
	AuditLogger domain.AuditLogger
	// Logger is the optional structured logger. When non-nil, the agent emits
	// per-attempt LLM-call boundary events ("attempt start", "attempt complete",
	// retry decisions). Nil falls back to no-op so existing tests stay valid.
	// Used to localize Phase A hangs to the inner LLM call vs. validation step.
	Logger *slog.Logger
}

// writerActResponse is the per-act LLM response shape. The full
// NarrationScript is assembled by the writer agent after merging
// every act's response.
type writerActResponse struct {
	ActID  string                  `json:"act_id"`
	Scenes []domain.NarrationScene `json:"scenes"`
}

// writerActSpec captures everything the writer needs to render and
// validate a single per-act LLM call.
type writerActSpec struct {
	Index      int // 0..3 in domain.ActOrder
	Act        domain.Act
	SceneNumLo int
	SceneNumHi int
}

const (
	writerPerActRetryBudget = 1   // one retry per act on schema violation
	priorActBeatRuneCap     = 240 // bound on the prior-act narration tail injected into the next act's prompt (cascade: Act N+1 sees Act N's tail)
	// Per-act narration rune caps live in domain.ActNarrationRuneCap (see
	// scenario.go). validateWriterActResponse looks up the cap for the
	// current act so cold-open scenes stay tight (incident=120 enforces
	// the ≤15s rule from docs/prompts/scenario/03_writing.md) while the
	// climax has room to breathe (revelation=520). Schema-violation
	// retries re-render the same prompt; persistent overruns fail fast
	// rather than dragging cap drift downstream into TTS/image stages.
)

// defaultAgentConcurrency is the fan-out cap when TextAgentConfig.Concurrency
// is unset for parallel agents that aren't writer-specific (e.g. visual
// breakdowner). Picked to stay within DashScope's 10 RPM / 2 concurrent limit
// while still gaining most of the wall-clock win over fully sequential calls.
const defaultAgentConcurrency = 4

func NewWriter(
	gen domain.TextGenerator,
	cfg TextAgentConfig,
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
		case cfg.Model == "":
			return fmt.Errorf("writer: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("writer: %w: provider is empty", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("writer: %w: generator is nil", domain.ErrValidation)
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

		// PriorCriticFeedback is injected identically into every act's prompt
		// (see spec §4 Critic feedback). Future per-act targeting can split
		// it; today we mirror the legacy single-call writer's behavior.
		qualityFeedback := state.PriorCriticFeedback

		// Cascade: Acts 1→2→3→4 are written sequentially so each act sees
		// its actual predecessor's narration tail. Pure parallel fan-out
		// (the prior design) sent Act 1's tail to Acts 2/3/4 indistinctly,
		// which left Act 3 narrating against Act 1's mood and Act 4 closing
		// against Act 1's hook — cross-act drift the cascade fixes.
		// Wall-clock cost: ~4× a single act's runtime, accepted in exchange
		// for narration coherence. See spec
		// `_bmad-output/implementation-artifacts/spec-writer-continuity-commentary-volume-bridge.md`.
		responses := make([]writerActResponse, len(specs))
		metas := make([]actCallMeta, len(specs))
		priorSummary := ""
		for i, spec := range specs {
			resp, meta, err := runWriterAct(ctx, gen, cfg, prompts, terms, state, spec, priorSummary, qualityFeedback)
			if err != nil {
				return err
			}
			responses[i] = resp
			metas[i] = meta
			priorSummary = summarizePriorAct(resp)
		}

		script, err := mergeWriterActs(state, specs, responses)
		if err != nil {
			return err
		}

		fillNarrationMetadata(&script, metas[0].resp, cfg, terms)
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

// actCallMeta carries the upstream response metadata (model/provider) for
// the metadata-fill step. Only the first successful response's metadata
// is used (drift across acts is silently tolerated, matching the legacy
// single-call writer).
type actCallMeta struct {
	resp domain.TextResponse
}

func planWriterActs(structure *domain.StructurerOutput) ([]writerActSpec, error) {
	if structure == nil || len(structure.Acts) != len(domain.ActOrder) {
		return nil, fmt.Errorf("writer: %w: structure must have %d acts", domain.ErrValidation, len(domain.ActOrder))
	}
	specs := make([]writerActSpec, len(structure.Acts))
	offset := 1
	for i, act := range structure.Acts {
		if act.ID != domain.ActOrder[i] {
			return nil, fmt.Errorf("writer: %w: act %d id=%s, want %s", domain.ErrValidation, i, act.ID, domain.ActOrder[i])
		}
		if act.SceneBudget < 1 {
			return nil, fmt.Errorf("writer: %w: act %s scene_budget=%d (must be >=1)", domain.ErrValidation, act.ID, act.SceneBudget)
		}
		specs[i] = writerActSpec{
			Index:      i,
			Act:        act,
			SceneNumLo: offset,
			SceneNumHi: offset + act.SceneBudget - 1,
		}
		offset += act.SceneBudget
	}
	return specs, nil
}

func runWriterAct(
	ctx context.Context,
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
	terms *ForbiddenTerms,
	state *PipelineState,
	spec writerActSpec,
	priorSummary string,
	qualityFeedback string,
) (writerActResponse, actCallMeta, error) {
	prompt, err := renderWriterActPrompt(state, prompts, terms, spec, priorSummary, qualityFeedback)
	if err != nil {
		return writerActResponse{}, actCallMeta{}, err
	}

	type writerAttemptResult struct {
		decoded writerActResponse
		meta    actCallMeta
	}

	opts := retryOpts{
		Stage:  "writer",
		Budget: writerPerActRetryBudget,
		Logger: cfg.Logger,
		BaseAttrs: []slog.Attr{
			slog.String("run_id", state.RunID),
			slog.String("act_id", spec.Act.ID),
		},
	}

	out, err := runWithRetry(ctx, opts, func(attempt int) (writerAttemptResult, retryReason, error) {
		if cfg.Logger != nil {
			cfg.Logger.Info("writer attempt start",
				"run_id", state.RunID,
				"act_id", spec.Act.ID,
				"attempt", attempt,
				"provider", cfg.Provider,
				"model", cfg.Model,
				"prompt_chars", utf8.RuneCountInString(prompt),
				"has_quality_feedback", qualityFeedback != "",
			)
		}
		callStart := time.Now()
		resp, err := gen.Generate(ctx, domain.TextRequest{
			Prompt:      prompt,
			Model:       cfg.Model,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		})
		if err != nil {
			if cfg.Logger != nil {
				cfg.Logger.Error("writer attempt failed",
					"run_id", state.RunID,
					"act_id", spec.Act.ID,
					"attempt", attempt,
					"duration_ms", time.Since(callStart).Milliseconds(),
					"error", err.Error(),
				)
			}
			// Transport error: propagate immediately. Returning a sentinel
			// retry-aborting reason isn't enough because runWithRetry would
			// still loop; instead, package the error and surface it via the
			// retryAbort signal.
			return writerAttemptResult{}, retryReasonAbort, err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("writer attempt complete",
				"run_id", state.RunID,
				"act_id", spec.Act.ID,
				"attempt", attempt,
				"duration_ms", time.Since(callStart).Milliseconds(),
				"finish_reason", resp.FinishReason,
				"tokens_in", resp.TokensIn,
				"tokens_out", resp.TokensOut,
			)
		}

		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Log(ctx, domain.AuditEntry{
				Timestamp: time.Now(),
				EventType: domain.AuditEventTextGeneration,
				RunID:     state.RunID,
				Stage:     "writer",
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}

		// Truncated completions usually mean the act would not fit in the
		// configured max_tokens. Decoding the half-written JSON would fail
		// with a generic parse error; surface a clearer message and abort
		// without burning the retry budget on the same broken response shape.
		if isTruncatedFinishReason(resp.FinishReason) {
			return writerAttemptResult{}, retryReasonAbort, fmt.Errorf(
				"writer: act %s: provider truncated completion (finish_reason=%q); raise max_tokens or shorten the prompt: %w",
				spec.Act.ID, resp.FinishReason, domain.ErrValidation,
			)
		}

		var decoded writerActResponse
		if err := decodeJSONResponse(resp.Content, &decoded); err != nil {
			return writerAttemptResult{}, retryReasonJSONDecode, fmt.Errorf("writer: act %s: %w", spec.Act.ID, err)
		}
		if err := validateWriterActResponse(spec, decoded); err != nil {
			return writerAttemptResult{}, retryReasonSchemaValidation, err
		}
		return writerAttemptResult{decoded: decoded, meta: actCallMeta{resp: resp}}, "", nil
	})
	if err != nil {
		return writerActResponse{}, actCallMeta{}, err
	}
	return out.decoded, out.meta, nil
}

func validateWriterActResponse(spec writerActSpec, decoded writerActResponse) error {
	if decoded.ActID != spec.Act.ID {
		return fmt.Errorf("writer: act %s: response act_id=%q: %w", spec.Act.ID, decoded.ActID, domain.ErrValidation)
	}
	if len(decoded.Scenes) != spec.Act.SceneBudget {
		return fmt.Errorf("writer: act %s: scene count=%d want=%d: %w", spec.Act.ID, len(decoded.Scenes), spec.Act.SceneBudget, domain.ErrValidation)
	}
	// Look up the per-act cap once before iterating so unrecognized act IDs
	// fail even when the LLM emits zero scenes (a zero-scene response would
	// otherwise skip the loop and silently pass this validator).
	runeCap, ok := domain.ActNarrationRuneCap[spec.Act.ID]
	if !ok {
		return fmt.Errorf("writer: act %s: no narration cap configured: %w", spec.Act.ID, domain.ErrValidation)
	}
	prev := spec.SceneNumLo - 1
	for i, scene := range decoded.Scenes {
		if scene.SceneNum < spec.SceneNumLo || scene.SceneNum > spec.SceneNumHi {
			return fmt.Errorf("writer: act %s: scene[%d] scene_num=%d out of range [%d,%d]: %w",
				spec.Act.ID, i, scene.SceneNum, spec.SceneNumLo, spec.SceneNumHi, domain.ErrValidation)
		}
		if scene.SceneNum <= prev {
			return fmt.Errorf("writer: act %s: scene[%d] scene_num=%d not strictly ascending after %d: %w",
				spec.Act.ID, i, scene.SceneNum, prev, domain.ErrValidation)
		}
		if scene.ActID != spec.Act.ID {
			return fmt.Errorf("writer: act %s: scene[%d] act_id=%q: %w",
				spec.Act.ID, i, scene.ActID, domain.ErrValidation)
		}
		if n := utf8.RuneCountInString(scene.Narration); n > runeCap {
			return fmt.Errorf("writer: act %s: scene[%d] scene_num=%d narration length=%d runes exceeds cap=%d (one-visual-beat rule, see docs/prompts/scenario/03_writing.md): %w",
				spec.Act.ID, i, scene.SceneNum, n, runeCap, domain.ErrValidation)
		}
		prev = scene.SceneNum
	}
	return nil
}

func mergeWriterActs(state *PipelineState, specs []writerActSpec, responses []writerActResponse) (domain.NarrationScript, error) {
	totalScenes := 0
	for _, spec := range specs {
		totalScenes += spec.Act.SceneBudget
	}
	scenes := make([]domain.NarrationScene, 0, totalScenes)
	for _, resp := range responses {
		scenes = append(scenes, resp.Scenes...)
	}
	sort.Slice(scenes, func(i, j int) bool {
		return scenes[i].SceneNum < scenes[j].SceneNum
	})
	for i, scene := range scenes {
		want := i + 1
		if scene.SceneNum != want {
			return domain.NarrationScript{}, fmt.Errorf("writer: merged scene[%d] scene_num=%d want=%d: %w",
				i, scene.SceneNum, want, domain.ErrValidation)
		}
	}

	title := state.Research.Title
	if title == "" {
		title = state.SCPID
	}
	return domain.NarrationScript{
		SCPID:  state.SCPID,
		Title:  title,
		Scenes: scenes,
	}, nil
}

func renderWriterActPrompt(
	state *PipelineState,
	prompts PromptAssets,
	terms *ForbiddenTerms,
	spec writerActSpec,
	priorSummary string,
	qualityFeedback string,
) (string, error) {
	visualJSON, err := json.MarshalIndent(state.Research.VisualIdentity, "", "  ")
	if err != nil {
		return "", fmt.Errorf("writer: marshal visual identity: %w", domain.ErrValidation)
	}
	forbidden := renderForbiddenTermsSection(terms.Raw)
	keyPoints := renderKeyPoints(spec.Act.KeyPoints)
	sceneRange := fmt.Sprintf("%d..%d", spec.SceneNumLo, spec.SceneNumHi)
	exemplar, ok := prompts.ExemplarsByAct[spec.Act.ID]
	if !ok || exemplar == "" {
		return "", fmt.Errorf("writer: act %s: no exemplar narration available: %w", spec.Act.ID, domain.ErrValidation)
	}
	replacer := strings.NewReplacer(
		"{scp_id}", state.SCPID,
		"{act_id}", spec.Act.ID,
		"{scene_num_range}", sceneRange,
		"{scene_budget}", strconv.Itoa(spec.Act.SceneBudget),
		"{act_synopsis}", spec.Act.Synopsis,
		"{act_key_points}", keyPoints,
		"{prior_act_summary}", priorSummary,
		"{scp_visual_reference}", string(visualJSON),
		"{format_guide}", prompts.FormatGuide,
		"{forbidden_terms_section}", forbidden,
		"{glossary_section}", "",
		"{quality_feedback}", qualityFeedback,
		"{exemplar_scenes}", exemplar,
	)
	return replacer.Replace(prompts.WriterTemplate), nil
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

// summarizePriorAct condenses an act's narration tail into a continuity
// hint for the next act. Returns "" for an empty act (defensive — also
// used as the seed value before Act 1 runs, which renders an empty
// `{prior_act_summary}` in Act 1's prompt). Each act sees its actual
// predecessor's tail in the cascade; the rune cap keeps prompt cost
// bounded even though the summary is no longer shared across siblings.
func summarizePriorAct(act writerActResponse) string {
	if len(act.Scenes) == 0 {
		return ""
	}
	last := act.Scenes[len(act.Scenes)-1]
	beat := truncatePrompt(strings.TrimSpace(last.Narration), priorActBeatRuneCap)
	parts := []string{
		fmt.Sprintf("Previous act ended on this beat: %s", beat),
	}
	if last.Mood != "" {
		parts = append(parts, fmt.Sprintf("Maintain the tone established there (mood: %s).", last.Mood))
	}
	parts = append(parts, "Do NOT re-introduce the entity from scratch and do NOT recap the hook.")
	return strings.Join(parts, " ")
}

// isTruncatedFinishReason reports whether a provider's finish_reason
// indicates the response was cut off by the output token cap (vs. the
// model genuinely finishing). Decoded JSON from a truncated response is
// almost always invalid; surface a clear error early instead of letting
// the per-act retry burn its budget on the same broken shape.
func isTruncatedFinishReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens":
		return true
	default:
		return false
	}
}

// truncatePrompt rune-aware truncates s to at most n runes.
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
	script.Metadata = domain.NarrationMetadata{
		Language:              domain.LanguageKorean,
		SceneCount:            len(script.Scenes),
		WriterModel:           resp.Model,
		WriterProvider:        resp.Provider,
		PromptTemplate:        filepath.Base(writerPromptPath),
		FormatGuideTemplate:   filepath.Base(formatGuidePath),
		ForbiddenTermsVersion: terms.Version,
	}
	script.SourceVersion = domain.NarrationSourceVersionV1
}
