package service

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// ScpImageRunStore is the minimal run access surface ScpImageService needs.
// *db.RunStore satisfies it structurally.
type ScpImageRunStore interface {
	Get(ctx context.Context, id string) (*domain.Run, error)
}

// ScpImageStore is the persistence surface for the canonical image library.
// *db.ScpImageLibraryStore satisfies it structurally.
type ScpImageStore interface {
	Get(ctx context.Context, scpID string) (*domain.ScpImageRecord, error)
	Upsert(ctx context.Context, rec *domain.ScpImageRecord) (*domain.ScpImageRecord, error)
	Delete(ctx context.Context, scpID string) error
}

// ScpImageCharacterCache is the read surface used to resolve the operator's
// selected DDG candidate. *db.CharacterCacheStore satisfies it.
type ScpImageCharacterCache interface {
	Get(ctx context.Context, queryKey string) (*domain.CharacterGroup, error)
}

// ReferenceFetcher rewrites a remote URL into a value the image-edit
// provider can ingest. Production wires pipeline.FetchReferenceImageAsDataURL.
// nil disables rewriting (URL passes through verbatim — used by tests).
type ReferenceFetcher func(ctx context.Context, url string) (string, error)

// ScpImageService owns the lifecycle of the per-SCP canonical cartoon image:
// generate (image-edit on the operator's DDG pick), persist (file + DB row),
// retrieve, and atomic regenerate (version bump).
type ScpImageService struct {
	runs           ScpImageRunStore
	cache          ScpImageCharacterCache
	library        ScpImageStore
	images         domain.ImageGenerator
	refFetcher     ReferenceFetcher
	editModel      string
	stylePrompt    string
	scpImageDir    string
	canonicalWidth int
	canonicalHt    int
}

// ScpImageServiceConfig bundles the construction-time dependencies.
type ScpImageServiceConfig struct {
	Runs           ScpImageRunStore
	Cache          ScpImageCharacterCache
	Library        ScpImageStore
	Images         domain.ImageGenerator
	RefFetcher     ReferenceFetcher
	EditModel      string
	StylePrompt    string
	ScpImageDir    string
	CanonicalWidth int
	CanonicalHt    int
}

// NewScpImageService constructs a service. EditModel/StylePrompt/ScpImageDir
// must be non-empty; CanonicalWidth/Ht must be positive. Validation is
// fail-fast at construction so a misconfigured server does not surface as a
// runtime error during the operator's first canonical generation.
func NewScpImageService(cfg ScpImageServiceConfig) (*ScpImageService, error) {
	if cfg.Runs == nil || cfg.Cache == nil || cfg.Library == nil || cfg.Images == nil {
		return nil, fmt.Errorf("scp image service: %w: required dep is nil", domain.ErrValidation)
	}
	if strings.TrimSpace(cfg.EditModel) == "" {
		return nil, fmt.Errorf("scp image service: %w: edit model is empty", domain.ErrValidation)
	}
	if strings.TrimSpace(cfg.StylePrompt) == "" {
		return nil, fmt.Errorf("scp image service: %w: style prompt is empty", domain.ErrValidation)
	}
	if strings.TrimSpace(cfg.ScpImageDir) == "" {
		return nil, fmt.Errorf("scp image service: %w: scp image dir is empty", domain.ErrValidation)
	}
	if cfg.CanonicalWidth <= 0 || cfg.CanonicalHt <= 0 {
		return nil, fmt.Errorf("scp image service: %w: invalid canonical dimensions %dx%d",
			domain.ErrValidation, cfg.CanonicalWidth, cfg.CanonicalHt)
	}
	return &ScpImageService{
		runs:           cfg.Runs,
		cache:          cfg.Cache,
		library:        cfg.Library,
		images:         cfg.Images,
		refFetcher:     cfg.RefFetcher,
		editModel:      cfg.EditModel,
		stylePrompt:    cfg.StylePrompt,
		scpImageDir:    cfg.ScpImageDir,
		canonicalWidth: cfg.CanonicalWidth,
		canonicalHt:    cfg.CanonicalHt,
	}, nil
}

// GetByRun returns the canonical record keyed on the run's SCP_ID.
// Returns ErrNotFound when the SCP has no canonical yet.
func (s *ScpImageService) GetByRun(ctx context.Context, runID string) (*domain.ScpImageRecord, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("scp image get by run: load run: %w", err)
	}
	return s.GetBySCPID(ctx, run.SCPID)
}

// GetBySCPID returns the canonical record for scpID. Returns ErrNotFound when
// the library has no row.
func (s *ScpImageService) GetBySCPID(ctx context.Context, scpID string) (*domain.ScpImageRecord, error) {
	if !IsValidSCPID(scpID) {
		return nil, fmt.Errorf("scp image get: %w: invalid scp_id %q", domain.ErrValidation, scpID)
	}
	return s.library.Get(ctx, scpID)
}

// GenerateCanonicalInput carries optional UI-stage overrides for the
// pre-pick "preview before commit" flow. When CandidateID or FrozenDescriptor
// are non-empty they win over whatever is currently persisted on the run —
// this lets CharacterPick generate a canonical preview before /pick has
// advanced the run's stage and persisted those values. Empty overrides fall
// back to run state, preserving the post-pick "regenerate" path.
type GenerateCanonicalInput struct {
	Regenerate       bool
	CandidateID      string
	FrozenDescriptor string
}

// Generate produces (or regenerates) the canonical cartoon image for the run's
// SCP_ID. When in.Regenerate is false and the library already has a hit, the
// stored record is returned as-is (no provider call, idempotent).
//
// The flow on a generate path:
//  1. Compose prompt = StylePrompt + "; " + frozen_descriptor
//  2. Fetch the operator-selected DDG candidate's image as a data URL
//  3. Call ImageGenerator.Edit with target = scpImageDir/{SCP_ID}/canonical.png
//  4. Upsert the library row (version increments on conflict)
//
// If Edit fails after a partial write, the underlying ComfyUI client uses
// temp+rename so no half-written file is left behind. If the DB upsert fails
// after a successful Edit, the file is updated but the row is stale — the
// next regenerate corrects this. Acceptable for a single-operator pipeline.
func (s *ScpImageService) Generate(ctx context.Context, runID string, in GenerateCanonicalInput) (*domain.ScpImageRecord, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("scp image generate: load run: %w", err)
	}
	if !IsValidSCPID(run.SCPID) {
		return nil, fmt.Errorf("scp image generate: %w: run has invalid scp_id %q", domain.ErrValidation, run.SCPID)
	}

	// Idempotent hit path. Note: hit short-circuits regardless of the
	// overrides — the operator chose an existing canonical and explicitly
	// did not request regenerate, so we honor it.
	if !in.Regenerate {
		if existing, err := s.library.Get(ctx, run.SCPID); err == nil {
			return existing, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("scp image generate: lookup library: %w", err)
		}
	}

	candidateID := in.CandidateID
	if candidateID == "" && run.SelectedCharacterID != nil {
		candidateID = *run.SelectedCharacterID
	}
	if candidateID == "" {
		return nil, fmt.Errorf("scp image generate: %w: missing candidate id (run has no selected character and no override)", domain.ErrValidation)
	}
	queryKey := ""
	if run.CharacterQueryKey != nil {
		queryKey = *run.CharacterQueryKey
	}
	if queryKey == "" {
		queryKey = queryKeyFromCandidateID(candidateID)
	}
	if queryKey == "" {
		return nil, fmt.Errorf("scp image generate: %w: missing character query key", domain.ErrValidation)
	}
	group, err := s.cache.Get(ctx, queryKey)
	if err != nil {
		return nil, fmt.Errorf("scp image generate: load cached candidates: %w", err)
	}
	var selected *domain.CharacterCandidate
	for i := range group.Candidates {
		if group.Candidates[i].ID == candidateID {
			selected = &group.Candidates[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("scp image generate: %w: candidate %q missing from cache", domain.ErrValidation, candidateID)
	}
	if selected.ImageURL == "" {
		return nil, fmt.Errorf("scp image generate: %w: candidate has no image url", domain.ErrValidation)
	}

	frozen := strings.TrimSpace(in.FrozenDescriptor)
	if frozen == "" && run.FrozenDescriptor != nil {
		frozen = strings.TrimSpace(*run.FrozenDescriptor)
	}
	if frozen == "" {
		return nil, fmt.Errorf("scp image generate: %w: missing frozen descriptor (run has none and no override)", domain.ErrValidation)
	}
	prompt := composeCanonicalPrompt(s.stylePrompt, frozen)

	refURL := selected.ImageURL
	if s.refFetcher != nil {
		rewritten, fetchErr := s.refFetcher(ctx, refURL)
		if fetchErr != nil {
			return nil, fmt.Errorf("scp image generate: prepare reference: %w", fetchErr)
		}
		refURL = rewritten
	}

	relPath := filepath.Join(run.SCPID, "canonical.png")
	absPath := filepath.Join(s.scpImageDir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("scp image generate: prepare dir: %w", err)
	}

	seed, err := newRandSeed()
	if err != nil {
		return nil, fmt.Errorf("scp image generate: seed: %w", err)
	}

	resp, err := s.images.Edit(ctx, domain.ImageEditRequest{
		Prompt:             prompt,
		Model:              s.editModel,
		ReferenceImageURL: refURL,
		Width:              s.canonicalWidth,
		Height:             s.canonicalHt,
		OutputPath:         absPath,
		Seed:               seed,
	})
	if err != nil {
		return nil, fmt.Errorf("scp image generate: edit: %w", err)
	}
	// Sanity check: the file must exist after a successful Edit. Providers
	// that bypass OutputPath would leave the library row pointing at thin air.
	if _, statErr := os.Stat(absPath); statErr != nil {
		return nil, fmt.Errorf("scp image generate: provider did not write canonical at %s: %w", absPath, statErr)
	}

	persistedSeed := resp.Seed
	if persistedSeed == 0 {
		// Provider did not surface a seed (e.g. DashScope). Persist the seed
		// we generated so the row carries a real value.
		persistedSeed = seed
	}

	rec, err := s.library.Upsert(ctx, &domain.ScpImageRecord{
		ScpID:             run.SCPID,
		FilePath:          relPath,
		SourceRefURL:      selected.ImageURL,
		SourceQueryKey:    queryKey,
		SourceCandidateID: candidateID,
		FrozenDescriptor:  frozen,
		PromptUsed:        prompt,
		Seed:              persistedSeed,
	})
	if err != nil {
		// Roll back the on-disk write so the file/row pair stays atomic
		// (Always rule). Best-effort: a failed remove is logged via the
		// outer error chain but does not mask the original Upsert error.
		// On a true concurrent regenerate this might delete a peer's
		// fresh write; that race is bounded by the operator-driven UI
		// guard plus the rate limiter and is acceptable for this pipeline.
		_ = os.Remove(absPath)
		return nil, fmt.Errorf("scp image generate: persist library row: %w", err)
	}
	return rec, nil
}

// composeCanonicalPrompt prepends the cartoon-style prompt to the
// character-only segments of the operator's frozen descriptor. Phase A's
// frozen_descriptor mixes character description (`Appearance`,
// `Distinguishing features`) with scene cues (`Environment`); the latter
// caused FLUX.2 to render multi-panel scene comics instead of a single
// character reference. We strip those cues here so the canonical image is a
// clean character portrait. Phase B's per-shot prompt re-introduces scene
// context via the per-shot visual_descriptor.
//
// Legacy 4-field descriptors (pre-2026-05-05) also carried `Key visual
// moments`; the strip path below still recognizes that label so legacy runs
// regenerated through canonical do not regress.
//
// The stored library row keeps the FULL `frozen_descriptor` — only the
// prompt fed to image-edit is filtered.
func composeCanonicalPrompt(stylePrompt, frozen string) string {
	sep := "; "
	if strings.HasSuffix(stylePrompt, "; ") || strings.HasSuffix(stylePrompt, ";") {
		sep = " "
	}
	return stylePrompt + sep + extractCharacterDescriptor(frozen)
}

// extractCharacterDescriptor drops scene-level segments from a "; "-joined
// frozen descriptor. Recognized scene labels: "Environment" (current shape)
// and "Key visual moments" (legacy 4-field shape, retained for
// defense-in-depth on regenerated legacy runs). When filtering would empty
// the descriptor, falls back to the original — defensive for descriptors
// that don't follow the expected label scheme, so the image-edit call
// always receives some character context.
func extractCharacterDescriptor(frozen string) string {
	segments := strings.Split(frozen, "; ")
	kept := make([]string, 0, len(segments))
	for _, s := range segments {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "environment:") ||
			strings.HasPrefix(lower, "key visual moments:") {
			continue
		}
		kept = append(kept, trimmed)
	}
	if len(kept) == 0 {
		return frozen
	}
	return strings.Join(kept, "; ")
}

// IsValidSCPID enforces the path-safe shape used by both the static-serve
// handler and the canonical resolver. Allowed: alphanumerics, dash, underscore,
// dot, between 1 and 64 characters, AND must contain at least one
// alphanumeric character. Rejects any sequence containing `..`, `/`, `\`,
// or whitespace so a malicious value cannot escape the configured
// scp_image_dir; the alphanumeric requirement also rejects all-dot strings
// like `.` which would resolve to the dir root and collide across runs.
func IsValidSCPID(scpID string) bool {
	if scpID == "" || len(scpID) > 64 {
		return false
	}
	if strings.Contains(scpID, "..") {
		return false
	}
	hasAlnum := false
	for _, r := range scpID {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9':
			hasAlnum = true
		case r == '-', r == '_', r == '.':
			// allowed but not alphanumeric
		default:
			return false
		}
	}
	return hasAlnum
}

// newRandSeed returns a non-negative seed masked to 53 bits so the value
// round-trips through JSON without precision loss (JavaScript Number is
// float64; integers above 2^53-1 are silently rounded). Mirrors the
// constraint enforced by the FE Zod contract on scpCanonicalImageSchema.seed.
func newRandSeed() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:]) & ((1 << 53) - 1)), nil
}
