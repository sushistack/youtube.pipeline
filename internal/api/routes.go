package api

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/sushistack/youtube.pipeline/internal/service"
)

// Dependencies holds all handler dependencies injected at startup.
type Dependencies struct {
	Run       *RunHandler
	Settings  *SettingsHandler
	Artifacts *ArtifactsHandler // NEW
	Character *CharacterHandler
	ScpImage  *ScpImageHandler
	Scene     *SceneHandler
	Media     *MediaHandler
	Tuning    *TuningHandler
	HITL      *service.HITLService
	Logger    *slog.Logger
	WebFS     fs.FS
	OutputDir string
}

// RegisterRoutes registers all API routes on mux.
// All /api/ routes are wrapped with the standard middleware chain.
// The SPA catch-all is registered only when WebFS is provided.
func RegisterRoutes(mux *http.ServeMux, deps *Dependencies) {
	api := http.NewServeMux()

	// Pipeline lifecycle — 6 endpoints.
	api.HandleFunc("POST /api/runs", deps.Run.Create)
	api.HandleFunc("GET /api/runs", deps.Run.List)
	api.HandleFunc("GET /api/runs/{id}", deps.Run.Get)
	api.HandleFunc("GET /api/runs/{id}/status", deps.Run.Status)
	api.HandleFunc("GET /api/runs/{id}/status/stream", deps.Run.StatusStream)
	api.HandleFunc("POST /api/runs/{id}/cancel", deps.Run.Cancel)
	api.HandleFunc("POST /api/runs/{id}/resume", deps.Run.Resume)
	api.HandleFunc("POST /api/runs/{id}/advance", deps.Run.Advance)
	api.HandleFunc("POST /api/runs/{id}/rewind", deps.Run.Rewind)
	api.HandleFunc("GET /api/decisions", deps.Scene.ListDecisions)
	api.HandleFunc("GET /api/runs/{id}/characters", deps.Character.Search)
	api.HandleFunc("GET /api/runs/{id}/characters/descriptor", deps.Character.Descriptor)
	api.HandleFunc("POST /api/runs/{id}/characters/pick", deps.Character.Pick)
	if deps.ScpImage != nil {
		api.HandleFunc("GET /api/runs/{id}/characters/canonical", deps.ScpImage.Get)
		api.HandleFunc("POST /api/runs/{id}/characters/canonical", deps.ScpImage.Generate)
		api.HandleFunc("GET /api/scp_images/{scp_id}", deps.ScpImage.Static)
	}
	api.HandleFunc("GET /api/runs/{id}/scenes", deps.Scene.List)
	api.HandleFunc("GET /api/runs/{id}/cache", deps.Run.Cache)
	api.HandleFunc("GET /api/runs/{id}/review-items", deps.Scene.ListReviewItems)
	api.HandleFunc("POST /api/runs/{id}/decisions", deps.Scene.RecordDecision)
	api.HandleFunc("POST /api/runs/{id}/approve-all-remaining", deps.Scene.ApproveAllRemaining)
	api.HandleFunc("POST /api/runs/{id}/undo", deps.Scene.Undo)
	api.HandleFunc("POST /api/runs/{id}/scenes/{idx}/edit", deps.Scene.Edit)
	api.HandleFunc("POST /api/runs/{id}/scenes/{idx}/regen", deps.Scene.Regenerate)
	if deps.Media != nil {
		api.HandleFunc("GET /api/runs/{id}/scenes/{idx}/audio", deps.Media.Audio)
		api.HandleFunc("GET /api/runs/{id}/scenes/{idx}/shots/{shot}/image", deps.Media.Image)
	}
	api.HandleFunc("POST /api/runs/{id}/scenario/approve", deps.Run.ApproveScenarioReview)
	api.HandleFunc("POST /api/runs/{id}/batch-review/approve", deps.Run.FinalizeBatchReview)

	// Compliance gate + artifact serving (Story 9.4).
	api.HandleFunc("POST /api/runs/{id}/metadata/ack", deps.Run.AcknowledgeMetadata)
	api.HandleFunc("GET /api/runs/{id}/video", deps.Artifacts.Video)
	api.HandleFunc("GET /api/runs/{id}/metadata", deps.Artifacts.Metadata)
	api.HandleFunc("GET /api/runs/{id}/manifest", deps.Artifacts.Manifest)
	api.HandleFunc("GET /api/settings", deps.Settings.Get)
	api.HandleFunc("PUT /api/settings", deps.Settings.Put)
	api.HandleFunc("POST /api/settings/reset", deps.Settings.ResetToDefaults)

	// Tuning surface — Story 10.2.
	if deps.Tuning != nil {
		api.HandleFunc("GET /api/tuning/critic-prompt", deps.Tuning.GetPrompt)
		api.HandleFunc("PUT /api/tuning/critic-prompt", deps.Tuning.PutPrompt)
		api.HandleFunc("GET /api/tuning/golden", deps.Tuning.GetGolden)
		api.HandleFunc("POST /api/tuning/golden/run", deps.Tuning.RunGolden)
		api.HandleFunc("POST /api/tuning/golden/pairs", deps.Tuning.AddGoldenPair)
		api.HandleFunc("POST /api/tuning/shadow/run", deps.Tuning.RunShadow)
		api.HandleFunc("GET /api/tuning/calibration", deps.Tuning.GetCalibration)
		api.HandleFunc("POST /api/tuning/fast-feedback", deps.Tuning.FastFeedback)
	}

	apiChain := Chain(api,
		WithRequestID,
		WithRecover,
		WithHostAllowlist,
		WithCORS,
		WithRequestLog(deps.Logger),
	)
	mux.Handle("/api/", apiChain)

	if deps.WebFS != nil {
		mux.Handle("/", spaHandler(deps.WebFS))
	}
}

// NewDependencies constructs a Dependencies value wiring the standard objects.
// outputDir is the server-configured run output base (never client-controlled).
// tuning may be nil in deployments that haven't wired the Tuning surface; in
// that case /api/tuning/* routes are simply not registered.
// segments may be nil; the per-scene media route is registered only when a
// lookup is provided.
// scpImage may be nil; the canonical image library routes are registered only
// when both the service and its on-disk root directory are configured.
func NewDependencies(
	svc *service.RunService,
	settings *service.SettingsService,
	hitl *service.HITLService,
	characters *service.CharacterService,
	scpImage *service.ScpImageService,
	scenes *service.SceneService,
	segments SegmentLookup,
	tuning TuningService,
	outputDir string,
	scpImageDir string,
	logger *slog.Logger,
	webFS fs.FS,
) *Dependencies {
	deps := &Dependencies{
		Run:       NewRunHandler(svc, hitl, outputDir, logger),
		Settings:  NewSettingsHandler(settings),
		Artifacts: NewArtifactsHandler(svc, outputDir),
		Character: NewCharacterHandler(characters),
		Scene:     NewSceneHandler(scenes),
		Logger:    logger,
		WebFS:     webFS,
		OutputDir: outputDir,
	}
	if segments != nil {
		deps.Media = NewMediaHandler(svc, segments, settings, outputDir)
	}
	if tuning != nil {
		deps.Tuning = NewTuningHandler(tuning)
	}
	if scpImage != nil && scpImageDir != "" {
		deps.ScpImage = NewScpImageHandler(scpImage, scpImageDir)
	}
	return deps
}
