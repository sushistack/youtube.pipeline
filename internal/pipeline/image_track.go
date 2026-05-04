package pipeline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

// referenceImageMaxBytes caps the reference image download. DashScope rejects
// payloads above ~10 MiB; we stop well below that to keep request bodies sane
// after base64 expansion (≈4/3× the raw size).
const referenceImageMaxBytes = 8 << 20

// CharacterResolver resolves the operator-selected character candidate for a
// run via the Story 5.3 handoff contract. Image-track callers depend on this
// port instead of the concrete service to keep Phase B orchestration free of
// external search/cache plumbing.
type CharacterResolver interface {
	GetSelectedCandidate(ctx context.Context, runID string) (*domain.CharacterCandidate, error)
}

// ImageShotStore is the minimal persistence surface the image track needs to
// refresh `segments.shots` rows after a Phase B regeneration. *db.SegmentStore
// satisfies it structurally.
type ImageShotStore interface {
	UpsertImageShots(ctx context.Context, runID string, sceneIndex int, shots []domain.Shot) error
}

// Limiter gates provider calls through the shared rate-limit + concurrency
// tokens. *llmclient.CallLimiter satisfies this interface. Tests can supply a
// trivial passthrough implementation.
type Limiter interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

// ReferenceImageFetcher resolves a candidate reference URL into a value the
// downstream image-edit provider can ingest. Production wires this to a
// fetcher that downloads and re-encodes as a base64 data URL (DashScope
// cannot fetch DDG/Bing-CDN URLs). When nil, the URL is passed through
// unchanged — used by tests with mock providers that do not actually fetch.
type ReferenceImageFetcher func(ctx context.Context, url string) (string, error)

// ImageTrackConfig bundles the dependencies required to build an ImageTrack
// function. Provider/model identifiers are config-driven per AC-4 so business
// logic does not hardcode DashScope model names.
type ImageTrackConfig struct {
	OutputDir         string
	Provider          string
	GenerateModel     string
	EditModel         string
	Width             int
	Height            int
	Images            domain.ImageGenerator
	CharacterResolver CharacterResolver
	Shots             ImageShotStore
	Limiter           Limiter
	Clock             clock.Clock
	Logger            *slog.Logger
	// AuditLogger is the optional audit logger. When non-nil, image_generation
	// audit entries are written after each successful Generate/Edit call.
	// Nil is allowed (no-op guard).
	AuditLogger domain.AuditLogger
	// RefImageFetcher rewrites the selected character's image URL before it is
	// handed to the image-edit provider. Nil disables rewriting.
	RefImageFetcher ReferenceImageFetcher
}

// NewImageTrack constructs the Phase B image track from cfg. The returned
// function is safe to pass as pipeline.ImageTrack to NewPhaseBRunner.
func NewImageTrack(cfg ImageTrackConfig) (ImageTrack, error) {
	if cfg.OutputDir == "" {
		return nil, fmt.Errorf("image track: %w: output dir is empty", domain.ErrValidation)
	}
	if cfg.GenerateModel == "" {
		return nil, fmt.Errorf("image track: %w: generate model is empty", domain.ErrValidation)
	}
	if cfg.EditModel == "" {
		return nil, fmt.Errorf("image track: %w: edit model is empty", domain.ErrValidation)
	}
	if cfg.Images == nil {
		return nil, fmt.Errorf("image track: %w: image generator is nil", domain.ErrValidation)
	}
	if cfg.Shots == nil {
		return nil, fmt.Errorf("image track: %w: shot store is nil", domain.ErrValidation)
	}
	if cfg.CharacterResolver == nil {
		return nil, fmt.Errorf("image track: %w: character resolver is nil", domain.ErrValidation)
	}
	if cfg.Limiter == nil {
		return nil, fmt.Errorf("image track: %w: limiter is nil", domain.ErrValidation)
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, req PhaseBRequest) (ImageTrackResult, error) {
		return runImageTrack(ctx, cfg, clk, logger, req)
	}, nil
}

// ComposeImagePrompt returns the deterministic image prompt for one shot.
// It prefixes the Frozen Descriptor verbatim when the shot-level descriptor
// does not already begin with it, and never rewrites, trims, or paraphrases
// the Frozen Descriptor segment. Callers must have non-empty frozen and
// visual inputs; empty inputs are a programmer error surfaced via the image
// track's validation path before this helper is called.
func ComposeImagePrompt(frozen, visual string) string {
	// Treat the Frozen Descriptor as immutable bytes: do not TrimSpace here.
	// AC-2 requires the Frozen Descriptor segment to remain byte-stable
	// across every shot in a run, so normalization is deliberately absent.
	if visual == "" {
		return frozen
	}
	if frozen == "" {
		return visual
	}
	if strings.HasPrefix(visual, frozen) {
		return visual
	}
	// Guard against double separator when frozen already ends with "; ".
	sep := "; "
	if strings.HasSuffix(frozen, "; ") {
		sep = ""
	}
	return frozen + sep + visual
}

func runImageTrack(
	ctx context.Context,
	cfg ImageTrackConfig,
	clk clock.Clock,
	logger *slog.Logger,
	req PhaseBRequest,
) (ImageTrackResult, error) {
	if req.Stage != domain.StageImage {
		return ImageTrackResult{}, fmt.Errorf("image track: %w: stage %q is not image", domain.ErrValidation, req.Stage)
	}
	if req.RunID == "" {
		return ImageTrackResult{}, fmt.Errorf("image track: %w: run_id is empty", domain.ErrValidation)
	}
	if strings.ContainsAny(req.RunID, "/\\") || strings.Contains(req.RunID, "..") {
		return ImageTrackResult{}, fmt.Errorf("image track: %w: run_id contains invalid characters", domain.ErrValidation)
	}

	state, err := loadScenarioState(req)
	if err != nil {
		return ImageTrackResult{}, err
	}
	if state.VisualScript == nil {
		return ImageTrackResult{}, fmt.Errorf("image track: %w: scenario.json missing visual_script", domain.ErrValidation)
	}
	// D2 migration: derive the legacy per-scene shape via the in-memory
	// bridge so the v1 image_track loop continues to operate on
	// VisualBreakdownScene[] without v2 awareness. Bridge is read-only;
	// scenarioPath state is unaffected. Replaced by direct VisualAct
	// consumption when the image-prompt redesign lands (post-D-vis-final).
	legacyScenes := state.VisualScript.LegacyScenes(state.Narration)
	frozen := state.VisualScript.FrozenDescriptor
	if req.FrozenDescriptorOverride != nil && strings.TrimSpace(*req.FrozenDescriptorOverride) != "" {
		// Operator-edited descriptor (runs.frozen_descriptor) takes precedence
		// over the artifact value. The artifact itself is read-only — we only
		// swap the bytes used by ComposeImagePrompt.
		frozen = *req.FrozenDescriptorOverride
	}
	if strings.TrimSpace(frozen) == "" {
		return ImageTrackResult{}, fmt.Errorf("image track: %w: frozen descriptor is empty", domain.ErrValidation)
	}

	sceneCharacterMap, err := buildCharacterMap(state)
	if err != nil {
		return ImageTrackResult{}, err
	}

	// Validate no duplicate SceneNum in visual breakdown.
	seenScenes := make(map[int]bool, len(legacyScenes))
	for _, scene := range legacyScenes {
		if scene.SceneNum <= 0 {
			return ImageTrackResult{}, fmt.Errorf("image track: %w: scene_num %d must be >= 1", domain.ErrValidation, scene.SceneNum)
		}
		if scene.SceneNum > 99 {
			return ImageTrackResult{}, fmt.Errorf("image track: %w: scene_num %d exceeds 2-digit canonical format", domain.ErrValidation, scene.SceneNum)
		}
		if seenScenes[scene.SceneNum] {
			return ImageTrackResult{}, fmt.Errorf("image track: %w: duplicate scene_num %d in visual_breakdown", domain.ErrValidation, scene.SceneNum)
		}
		seenScenes[scene.SceneNum] = true
		for _, shot := range scene.Shots {
			if shot.ShotIndex <= 0 {
				return ImageTrackResult{}, fmt.Errorf("image track: %w: scene %d shot_index %d must be >= 1", domain.ErrValidation, scene.SceneNum, shot.ShotIndex)
			}
			if shot.ShotIndex > 99 {
				return ImageTrackResult{}, fmt.Errorf("image track: %w: scene %d shot_index %d exceeds 2-digit canonical format", domain.ErrValidation, scene.SceneNum, shot.ShotIndex)
			}
		}
	}

	runDir := filepath.Join(cfg.OutputDir, req.RunID)
	imagesRoot := filepath.Join(runDir, "images")
	if err := os.MkdirAll(imagesRoot, 0o755); err != nil {
		return ImageTrackResult{}, fmt.Errorf("image track: prepare images dir: %w", err)
	}

	var selected *domain.CharacterCandidate
	if anyCharacterScene(state, sceneCharacterMap) {
		selected, err = cfg.CharacterResolver.GetSelectedCandidate(ctx, req.RunID)
		if err != nil {
			return ImageTrackResult{}, fmt.Errorf("image track: resolve selected character: %w", err)
		}
		if selected == nil || selected.ImageURL == "" {
			return ImageTrackResult{}, fmt.Errorf("image track: %w: selected character has no image reference", domain.ErrValidation)
		}
		if cfg.RefImageFetcher != nil {
			// DashScope cannot fetch DDG/Bing-CDN URLs directly (redirects, UA
			// gating). Production wires a fetcher that downloads the reference
			// once and rewrites to a base64 data URL the qwen-image-edit
			// endpoint accepts inline.
			rewritten, fetchErr := cfg.RefImageFetcher(ctx, selected.ImageURL)
			if fetchErr != nil {
				return ImageTrackResult{}, fmt.Errorf("image track: prepare reference image: %w", fetchErr)
			}
			// Operate on a local copy so the cache row stays untouched.
			copy := *selected
			copy.ImageURL = rewritten
			selected = &copy
		}
	}

	result := ImageTrackResult{
		Observation: domain.NewStageObservation(domain.StageImage),
	}

	start := clk.Now()
	for _, scene := range legacyScenes {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("image track: %w", err)
		}
		if len(scene.Shots) == 0 {
			continue
		}

		sceneDir := filepath.Join(imagesRoot, fmt.Sprintf("scene_%02d", scene.SceneNum))
		if err := os.MkdirAll(sceneDir, 0o755); err != nil {
			return result, fmt.Errorf("image track: prepare scene dir %s: %w", sceneDir, err)
		}

		containsCharacter := sceneCharacterMap[scene.SceneNum]
		sceneIndex := scene.SceneNum - 1

		// Validate no duplicate ShotIndex within a scene.
		seenShots := make(map[int]bool, len(scene.Shots))
		for _, shot := range scene.Shots {
			if seenShots[shot.ShotIndex] {
				return result, fmt.Errorf("image track: %w: scene %d has duplicate shot_index %d", domain.ErrValidation, scene.SceneNum, shot.ShotIndex)
			}
			seenShots[shot.ShotIndex] = true
		}

		persisted := make([]domain.Shot, 0, len(scene.Shots))
		for _, shot := range scene.Shots {
			prompt := ComposeImagePrompt(frozen, shot.VisualDescriptor)
			relPath := filepath.Join("images",
				fmt.Sprintf("scene_%02d", scene.SceneNum),
				fmt.Sprintf("shot_%02d.png", shot.ShotIndex),
			)
			absPath := filepath.Join(runDir, relPath)

			logger.Info("image track: generating shot",
				"provider", cfg.Provider,
				"run_id", req.RunID,
				"scene", scene.SceneNum,
				"shot", shot.ShotIndex,
				"character_shot", containsCharacter,
				"output", absPath,
			)
			resp, err := invokeImageProvider(ctx, cfg, prompt, absPath, containsCharacter, selected)
			if err != nil {
				return result, fmt.Errorf("image track: scene %d shot %d: %w", scene.SceneNum, shot.ShotIndex, err)
			}
			if _, statErr := os.Stat(absPath); statErr != nil {
				return result, fmt.Errorf("image track: provider did not write scene %d shot %d at %s: %w", scene.SceneNum, shot.ShotIndex, absPath, statErr)
			}
			result.Observation.CostUSD += resp.CostUSD
			result.Artifacts = append(result.Artifacts, absPath)

			// Non-fatal audit write after each successful image generation.
			if cfg.AuditLogger != nil {
				if logErr := cfg.AuditLogger.Log(ctx, domain.AuditEntry{
					Timestamp: clk.Now(),
					EventType: domain.AuditEventImageGeneration,
					RunID:     req.RunID,
					Stage:     string(domain.StageImage),
					Provider:  resp.Provider,
					Model:     resp.Model,
					Prompt:    truncatePrompt(prompt, 2048),
					CostUSD:   resp.CostUSD,
				}); logErr != nil {
					logger.Warn("audit log write failed", "run_id", req.RunID, "error", logErr)
				}
			}

			persisted = append(persisted, domain.Shot{
				ImagePath:        relPath,
				DurationSeconds:  shot.EstimatedDurationS,
				Transition:       shot.Transition,
				VisualDescriptor: shot.VisualDescriptor,
			})

			// Persist immediately after each shot so partial progress is recoverable.
			if err := cfg.Shots.UpsertImageShots(ctx, req.RunID, sceneIndex, persisted); err != nil {
				return result, fmt.Errorf("image track: persist scene %d shot %d: %w", scene.SceneNum, shot.ShotIndex, err)
			}

			logger.Info("image track shot",
				"run_id", req.RunID,
				"scene", scene.SceneNum,
				"shot", shot.ShotIndex,
				"image_path", relPath,
				"character_shot", containsCharacter,
				"cost_usd", resp.CostUSD,
			)
		}
	}

	result.Observation.DurationMs = clk.Now().Sub(start).Milliseconds()
	if result.Observation.DurationMs < 0 {
		result.Observation.DurationMs = 0
	}
	return result, nil
}

func invokeImageProvider(
	ctx context.Context,
	cfg ImageTrackConfig,
	prompt string,
	outputPath string,
	containsCharacter bool,
	selected *domain.CharacterCandidate,
) (domain.ImageResponse, error) {
	var resp domain.ImageResponse
	call := func(inner context.Context) error {
		if containsCharacter {
			if selected == nil {
				return fmt.Errorf("%w: character shot without resolved candidate", domain.ErrValidation)
			}
			out, err := cfg.Images.Edit(inner, domain.ImageEditRequest{
				Prompt:             prompt,
				Model:              cfg.EditModel,
				ReferenceImageURL: selected.ImageURL,
				Width:              cfg.Width,
				Height:             cfg.Height,
				OutputPath:         outputPath,
			})
			if err != nil {
				return err
			}
			resp = out
			return nil
		}
		out, err := cfg.Images.Generate(inner, domain.ImageRequest{
			Prompt:     prompt,
			Model:      cfg.GenerateModel,
			Width:      cfg.Width,
			Height:     cfg.Height,
			OutputPath: outputPath,
		})
		if err != nil {
			return err
		}
		resp = out
		return nil
	}
	if err := cfg.Limiter.Do(ctx, call); err != nil {
		return domain.ImageResponse{}, err
	}
	return resp, nil
}

func loadScenarioState(req PhaseBRequest) (*agents.PipelineState, error) {
	if req.Scenario != nil {
		return req.Scenario, nil
	}
	if req.ScenarioPath == "" {
		return nil, fmt.Errorf("image track: %w: scenario path is empty", domain.ErrValidation)
	}
	raw, err := os.ReadFile(req.ScenarioPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("image track: %w: scenario.json missing at %s", domain.ErrValidation, req.ScenarioPath)
		}
		return nil, fmt.Errorf("image track: read scenario: %w", err)
	}
	var state agents.PipelineState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("image track: %w: decode scenario.json: %v", domain.ErrValidation, err)
	}
	return &state, nil
}

// FetchReferenceImageAsDataURL downloads imageURL and returns a base64 data
// URL (`data:<mime>;base64,...`) suitable for DashScope's ref_imgs field.
// DashScope's text2image endpoint cannot fetch arbitrary external URLs (DDG,
// Bing CDN) due to redirect chains and User-Agent gating, but it accepts
// inlined data URLs. The size cap protects request bodies from blowing past
// DashScope's payload limits after base64 expansion.
func FetchReferenceImageAsDataURL(ctx context.Context, imageURL string) (string, error) {
	if strings.HasPrefix(imageURL, "data:") {
		// Already inlined — pass through.
		return imageURL, nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", fmt.Errorf("create reference fetch request: %w", err)
	}
	// Some image hosts (Bing CDN among them) refuse default Go UA.
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 youtube.pipeline reference fetch")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("fetch reference: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch reference: %w: status %d", domain.ErrStageFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, referenceImageMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read reference body: %w: %v", domain.ErrStageFailed, err)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("%w: reference image is empty", domain.ErrValidation)
	}
	if len(body) > referenceImageMaxBytes {
		return "", fmt.Errorf("%w: reference image exceeds %d byte cap", domain.ErrValidation, referenceImageMaxBytes)
	}

	mime := resp.Header.Get("Content-Type")
	// Strip "; charset=..." or other parameters; some hosts return text/html for
	// blocked content, which would corrupt the data URL — sniff to verify.
	if idx := strings.IndexByte(mime, ';'); idx >= 0 {
		mime = strings.TrimSpace(mime[:idx])
	}
	if !strings.HasPrefix(mime, "image/") {
		mime = http.DetectContentType(body)
		if idx := strings.IndexByte(mime, ';'); idx >= 0 {
			mime = strings.TrimSpace(mime[:idx])
		}
	}
	if !strings.HasPrefix(mime, "image/") {
		return "", fmt.Errorf("%w: reference url returned non-image content type %q", domain.ErrValidation, mime)
	}

	encoded := base64.StdEncoding.EncodeToString(body)
	return "data:" + mime + ";base64," + encoded, nil
}

// buildCharacterMap returns a scene_num → containsCharacter lookup using the
// narration's EntityVisible signal. Narration is the canonical source for
// whether the named character/entity appears in a scene; image-track shot
// routing must not infer this from prompt strings at call time.
// Returns a validation error if VisualScript contains character scenes but
// narration is absent or missing the required scene entry.
func buildCharacterMap(state *agents.PipelineState) (map[int]bool, error) {
	out := map[int]bool{}
	if state.Narration == nil {
		// If narration is absent, verify no scene needs character routing.
		// (Bridged visual scenes derive EntityVisible from narration anyway,
		// so an absent narration produces an empty char map — consistent.)
		return out, nil
	}
	legacyScenes := state.Narration.LegacyScenes()
	narrationByScene := make(map[int]bool, len(legacyScenes))
	for _, scene := range legacyScenes {
		narrationByScene[scene.SceneNum] = scene.EntityVisible
	}
	// Validate every visual scene has a narration counterpart.
	if state.VisualScript != nil {
		visualLegacy := state.VisualScript.LegacyScenes(state.Narration)
		for _, scene := range visualLegacy {
			if _, ok := narrationByScene[scene.SceneNum]; !ok {
				return nil, fmt.Errorf("image track: %w: visual scene %d has no matching narration scene", domain.ErrValidation, scene.SceneNum)
			}
		}
	}
	for sceneNum, entityVisible := range narrationByScene {
		out[sceneNum] = entityVisible
	}
	return out, nil
}

func anyCharacterScene(state *agents.PipelineState, sceneMap map[int]bool) bool {
	if state.VisualScript == nil {
		return false
	}
	for _, scene := range state.VisualScript.LegacyScenes(state.Narration) {
		if sceneMap[scene.SceneNum] {
			return true
		}
	}
	return false
}

