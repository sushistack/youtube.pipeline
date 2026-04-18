package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// V1: This agent is deterministic - no LLM call. docs/prompts/scenario/
// 01_research.md is the reference prompt template that a V1.5 upgrade
// will wire through a domain.TextGenerator. The deterministic derivation
// here is the scaffolding and schema contract that the LLM path must honor.

var beatToneRotation = [4]string{"mystery", "horror", "tension", "revelation"}

func NewResearcher(corpus CorpusReader, validator *Validator) AgentFunc {
	return func(ctx context.Context, state *PipelineState) error {
		if state.SCPID == "" {
			return fmt.Errorf("researcher: empty scp_id: %w", domain.ErrValidation)
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
		if len(output.DramaticBeats) < 3 {
			return fmt.Errorf("researcher: sparse corpus: %d beats < 3: %w", len(output.DramaticBeats), domain.ErrValidation)
		}
		if err := validator.Validate(output); err != nil {
			return fmt.Errorf("researcher: %w", err)
		}
		state.Research = &output
		return nil
	}
}

func buildDramaticBeats(facts SCPFacts) []domain.DramaticBeat {
	beats := make([]domain.DramaticBeat, 0, 10)
	appendBeat := func(source, description string) {
		if len(beats) >= 10 {
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
