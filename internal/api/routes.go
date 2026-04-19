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
	Character *CharacterHandler
	Scene     *SceneHandler
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
	api.HandleFunc("POST /api/runs/{id}/cancel", deps.Run.Cancel)
	api.HandleFunc("POST /api/runs/{id}/resume", deps.Run.Resume)
	api.HandleFunc("GET /api/runs/{id}/characters", deps.Character.Search)
	api.HandleFunc("GET /api/runs/{id}/characters/descriptor", deps.Character.Descriptor)
	api.HandleFunc("POST /api/runs/{id}/characters/pick", deps.Character.Pick)
	api.HandleFunc("GET /api/runs/{id}/scenes", deps.Scene.List)
	api.HandleFunc("POST /api/runs/{id}/scenes/{idx}/edit", deps.Scene.Edit)

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
func NewDependencies(
	svc *service.RunService,
	hitl *service.HITLService,
	characters *service.CharacterService,
	scenes *service.SceneService,
	outputDir string,
	logger *slog.Logger,
	webFS fs.FS,
) *Dependencies {
	return &Dependencies{
		Run:       NewRunHandler(svc, hitl, outputDir, logger),
		Character: NewCharacterHandler(characters),
		Scene:     NewSceneHandler(scenes),
		Logger:    logger,
		WebFS:     webFS,
		OutputDir: outputDir,
	}
}
