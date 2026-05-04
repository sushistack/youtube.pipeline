package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// V1.2: deterministic beat extraction + one LLM "role classifier" call that
// labels each beat as hook/tension/reveal/bridge so the structurer can place
// it in the matching act. The classifier piggybacks on cfg.WriterModel /
// cfg.WriterProvider — see spec §Boundaries: no new defaults, no new env keys.

var beatToneRotation = [4]string{"mystery", "horror", "tension", "revelation"}

// roleClassifierAttempts is the total attempt budget (1 initial + 2 retries)
// per spec §Boundaries: classifier failure must hard-fail the run rather than
// silently fall back to the deleted modulo path.
const roleClassifierAttempts = 3

// roleClassifierAuditStage is the Stage label written to audit.log so the
// extra text_generation event is filterable from the writer/critic events.
const roleClassifierAuditStage = "researcher.role_classifier"

func NewResearcher(
	corpus CorpusReader,
	validator *Validator,
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	prompts PromptAssets,
) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		switch {
		case state.SCPID == "":
			return fmt.Errorf("researcher: empty scp_id: %w", domain.ErrValidation)
		case gen == nil:
			return fmt.Errorf("researcher: %w: generator is nil", domain.ErrValidation)
		case cfg.Model == "":
			return fmt.Errorf("researcher: %w: model is empty", domain.ErrValidation)
		case cfg.Provider == "":
			return fmt.Errorf("researcher: %w: provider is empty", domain.ErrValidation)
		case prompts.RoleClassifierTemplate == "":
			return fmt.Errorf("researcher: %w: role classifier template is empty", domain.ErrValidation)
		}

		doc, err := corpus.Read(ctx, state.SCPID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if errors.Is(err, ErrCorpusNotFound) || errors.Is(err, domain.ErrValidation) {
				return fmt.Errorf("researcher: read corpus for %s: %w", state.SCPID, err)
			}
			return fmt.Errorf("researcher: read corpus for %s: %v: %w", state.SCPID, err, domain.ErrStageFailed)
		}

		output := domain.ResearcherOutput{
			SCPID:                 state.SCPID,
			Title:                 doc.Facts.Title,
			ObjectClass:           doc.Facts.ObjectClass,
			PhysicalDescription:   doc.Facts.PhysicalDescription,
			AnomalousProperties:   copyStringSlice(doc.Facts.AnomalousProperties),
			ContainmentProcedures: doc.Facts.ContainmentProcedures,
			BehaviorAndNature:     doc.Facts.BehaviorAndNature,
			OriginAndDiscovery:    doc.Facts.OriginAndDiscovery,
			VisualIdentity: domain.VisualIdentity{
				Appearance:             doc.Facts.VisualElements.Appearance,
				DistinguishingFeatures: copyStringSlice(doc.Facts.VisualElements.DistinguishingFeatures),
				EnvironmentSetting:     doc.Facts.VisualElements.EnvironmentSetting,
				KeyVisualMoments:       copyStringSlice(doc.Facts.VisualElements.KeyVisualMoments),
			},
			DramaticBeats:   buildDramaticBeats(doc.Facts),
			MainTextExcerpt: truncateMainText(doc.MainText),
			Tags:            copyStringSlice(doc.Facts.Tags),
			SourceVersion:   domain.SourceVersionV1,
		}
		if len(output.Tags) == 0 {
			output.Tags = copyStringSlice(doc.Meta.Tags)
		}
		if output.Tags == nil {
			output.Tags = []string{}
		}
		if len(output.DramaticBeats) < 4 {
			return fmt.Errorf("researcher: sparse corpus: %d beats < 4: %w", len(output.DramaticBeats), domain.ErrValidation)
		}

		if err := classifyBeatRoles(ctx, gen, cfg, prompts.RoleClassifierTemplate, state, output.DramaticBeats); err != nil {
			return err
		}

		if err := validator.Validate(output); err != nil {
			return fmt.Errorf("researcher: %w", err)
		}
		state.Research = &output
		return nil
	}
}

func buildDramaticBeats(facts SCPFacts) []domain.DramaticBeat {
	beats := make([]domain.DramaticBeat, 0, 16)
	appendBeat := func(source, description string) {
		if len(beats) >= 16 {
			return
		}
		index := len(beats)
		beats = append(beats, domain.DramaticBeat{
			Index:         index,
			Source:        source,
			Description:   description,
			EmotionalTone: beatToneRotation[index%len(beatToneRotation)],
		})
	}
	for _, moment := range facts.VisualElements.KeyVisualMoments {
		appendBeat("visual_moment", moment)
	}
	for _, prop := range facts.AnomalousProperties {
		appendBeat("anomalous_property", prop)
	}
	return beats
}

// classifyBeatRoles runs the role classifier against the LLM up to
// roleClassifierAttempts times. On the first response that decodes cleanly
// AND validates (right shape, right indices, all four roles represented),
// each beat's RoleSuggestion is populated in place and the function returns
// nil. After the budget is exhausted the run fails with ErrStageFailed —
// the deleted modulo fallback would silently emit a broken arc, so this
// path must propagate the failure to the operator. (See spec I/O matrix:
// "All retries fail → no degraded structurer output is emitted.")
func classifyBeatRoles(
	ctx context.Context,
	gen domain.TextGenerator,
	cfg TextAgentConfig,
	template string,
	state *PipelineState,
	beats []domain.DramaticBeat,
) error {
	prompt := renderRoleClassifierPrompt(template, state.SCPID, beats)
	req := domain.TextRequest{
		Prompt:      prompt,
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	}

	var lastErr error
	for attempt := 0; attempt < roleClassifierAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("role classifier attempt start",
				"run_id", state.RunID,
				"attempt", attempt,
				"provider", cfg.Provider,
				"model", cfg.Model,
				"beat_count", len(beats),
			)
		}
		var (
			resp     domain.TextResponse
			parsed   any
			finalErr error
		)
		callStart := time.Now()
		emitOnExit := func() {
			emitAgentTrace(ctx, cfg, roleClassifierAuditStage, prompt, resp, parsed, "", finalErr, callStart)
		}
		var err error
		resp, err = gen.Generate(ctx, req)
		if err != nil {
			finalErr = err
			emitOnExit()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			lastErr = fmt.Errorf("transport: %w", err)
			if cfg.Logger != nil {
				cfg.Logger.Info("role classifier retry",
					"run_id", state.RunID,
					"attempt", attempt,
					"reason", "transport",
					"duration_ms", time.Since(callStart).Milliseconds(),
					"error", err.Error(),
				)
			}
			continue
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("role classifier attempt complete",
				"run_id", state.RunID,
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
				Stage:     roleClassifierAuditStage,
				Provider:  resp.Provider,
				Model:     resp.Model,
				Prompt:    truncatePrompt(prompt, 2048),
				CostUSD:   resp.CostUSD,
			})
		}

		assignments, perr := parseRoleClassifications(resp.Content, len(beats))
		if perr != nil {
			finalErr = perr
			emitOnExit()
			lastErr = perr
			if cfg.Logger != nil {
				cfg.Logger.Info("role classifier retry",
					"run_id", state.RunID,
					"attempt", attempt,
					"reason", "validation",
					"error", perr.Error(),
				)
			}
			continue
		}
		parsed = assignments
		emitOnExit()
		for i, role := range assignments {
			beats[i].RoleSuggestion = role
		}
		return nil
	}
	return fmt.Errorf("researcher: role classifier exhausted %d attempts: %v: %w", roleClassifierAttempts, lastErr, domain.ErrStageFailed)
}

// roleClassifierResponse is the strict shape the classifier prompt mandates.
type roleClassifierResponse struct {
	Classifications []roleClassification `json:"classifications"`
}

type roleClassification struct {
	Index int    `json:"index"`
	Role  string `json:"role"`
}

// parseRoleClassifications decodes the classifier response and validates:
//   - JSON shape (well-formed, no extras at top level)
//   - exact count (== beat count)
//   - indices are unique and cover [0, beatCount)
//   - role values are in the four-role enum
//   - every role appears at least once (balanced classification)
//
// Returns a slice of role strings indexed by beat index on success.
func parseRoleClassifications(raw string, beatCount int) ([]string, error) {
	body := stripJSONFence(raw)
	var decoded roleClassifierResponse
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode classifier json: %w", err)
	}
	// json.Decoder.Decode reads one JSON value and stops; trailing prose (the
	// LLM appending a "note:" comment after the object) would otherwise pass
	// silently. Drain whatever the decoder buffered — anything non-whitespace
	// fails the response so the prompt's "STRICT JSON only" rule has teeth.
	if rem, err := io.ReadAll(dec.Buffered()); err == nil {
		if extra := strings.TrimSpace(string(rem)); extra != "" {
			return nil, fmt.Errorf("decode classifier json: trailing content after object: %q", truncatePrompt(extra, 80))
		}
	}
	if len(decoded.Classifications) != beatCount {
		return nil, fmt.Errorf("classifier returned %d classifications, want %d", len(decoded.Classifications), beatCount)
	}

	seen := make(map[int]bool, beatCount)
	roles := make([]string, beatCount)
	roleCounts := make(map[string]int, 4)
	for _, c := range decoded.Classifications {
		if c.Index < 0 || c.Index >= beatCount {
			return nil, fmt.Errorf("classifier index %d out of range [0,%d)", c.Index, beatCount)
		}
		if seen[c.Index] {
			return nil, fmt.Errorf("classifier duplicate index %d", c.Index)
		}
		seen[c.Index] = true
		if !isValidRole(c.Role) {
			return nil, fmt.Errorf("classifier index %d has unknown role %q", c.Index, c.Role)
		}
		roles[c.Index] = c.Role
		roleCounts[c.Role]++
	}
	for _, role := range domain.RoleOrder {
		if roleCounts[role] == 0 {
			return nil, fmt.Errorf("classifier missing role %q (every role must appear ≥1 time)", role)
		}
	}
	return roles, nil
}

func isValidRole(role string) bool {
	switch role {
	case domain.RoleHook, domain.RoleTension, domain.RoleReveal, domain.RoleBridge:
		return true
	}
	return false
}

// stripJSONFence trims a leading UTF-8 BOM and ```json / ``` markdown fence
// (and trailing ``` if present). Mirrors decodeJSONResponse's tolerance for
// fenced LLM output. The BOM strip handles providers/proxies that prepend
// U+FEFF to multilingual responses — without it, the JSON decoder would fail
// on the leading byte and burn an attempt every time.
func stripJSONFence(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimPrefix(s, "json")
	s = strings.TrimPrefix(s, "JSON")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func renderRoleClassifierPrompt(template, scpID string, beats []domain.DramaticBeat) string {
	beatLines := make([]string, len(beats))
	for i, b := range beats {
		beatLines[i] = fmt.Sprintf("- index=%d source=%s tone=%s | %s", b.Index, b.Source, b.EmotionalTone, b.Description)
	}
	replacer := strings.NewReplacer(
		"{scp_id}", scpID,
		"{beat_count}", strconv.Itoa(len(beats)),
		"{beat_count_minus_one}", strconv.Itoa(len(beats)-1),
		"{beat_table}", strings.Join(beatLines, "\n"),
	)
	return replacer.Replace(template)
}

func copyStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func truncateMainText(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= 4000 {
		return s
	}
	var idx int
	for i := range s {
		if utf8.RuneCountInString(s[:i]) == 4000 {
			idx = i
			break
		}
	}
	if idx == 0 {
		return s
	}
	return strings.TrimSpace(s[:idx])
}
