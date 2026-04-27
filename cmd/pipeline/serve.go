package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dashscope"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/deepseek"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dryrun"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/gemini"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/web"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const viteDevServerURL = "http://localhost:5173"

// ttsEndpointForRegion maps a DashScopeRegion config value to the qwen3-tts
// MultiModalConversation endpoint. API keys are region-bound: a Singapore key
// rejected by the Beijing endpoint surfaces as 401 InvalidApiKey, so callers
// must pass the region that matches their issued key. Unknown values fall back
// to the international endpoint to match the most common deployment outside
// mainland China.
func ttsEndpointForRegion(region string) string {
	switch region {
	case "cn-beijing":
		return dashscope.DefaultTTSEndpointCN
	default:
		return dashscope.DefaultTTSEndpointIntl
	}
}

// imageEndpointForRegion mirrors ttsEndpointForRegion for the qwen-image /
// qwen-image-edit text2image surface. Same key-region binding applies.
func imageEndpointForRegion(region string) string {
	switch region {
	case "cn-beijing":
		return dashscope.DefaultImageEndpointCN
	default:
		return dashscope.DefaultImageEndpointIntl
	}
}

// buildPhaseBRunner constructs a PhaseBRunner with real DashScope-backed
// image and TTS tracks. Returns an error if the API key is missing or any
// construction step fails; the caller decides how to handle the absence
// (warn + skip vs. fatal).
//
// When cfg.DryRun is true, the DashScope clients are swapped for in-process
// fakes from the dryrun package — the API key is not required and no
// outbound HTTP traffic is generated. This is the single switch that makes
// Phase B free during prompt iteration. Phase C / StageAssemble is gated
// separately in pipeline.Engine.Advance using runs.dry_run, so a placeholder
// asset cannot compose into a final video.
//
// The limiter factory is injected rather than created per-call so TTS and
// image tracks continue to share a single DashScope limiter budget across
// stages and retries — otherwise each rebuild would hand each track a fresh
// budget and the rate-limit guarantees the old code established silently
// disappear.
func buildPhaseBRunner(
	cfg domain.PipelineConfig,
	dashScopeAPIKey string,
	limiterFactory *llmclient.ProviderLimiterFactory,
	runStore *db.RunStore,
	segStore *db.SegmentStore,
	characterResolver pipeline.CharacterResolver,
	logger *slog.Logger,
) (*pipeline.PhaseBRunner, error) {
	if !cfg.DryRun && dashScopeAPIKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY not set")
	}
	if limiterFactory == nil {
		return nil, fmt.Errorf("phase b runner: nil limiter factory")
	}
	if characterResolver == nil {
		return nil, fmt.Errorf("phase b runner: nil character resolver")
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}

	var (
		ttsClient   domain.TTSSynthesizer
		imageClient domain.ImageGenerator
	)
	if cfg.DryRun {
		logger.Info("phase b dry-run mode active: image + tts calls swapped for placeholder fakes")
		ttsClient = dryrun.NewTTSClient()
		imageClient = dryrun.NewImageClient()
	} else {
		realTTS, err := dashscope.NewTTSClient(httpClient, dashscope.TTSClientConfig{
			APIKey:       dashScopeAPIKey,
			Endpoint:     ttsEndpointForRegion(cfg.DashScopeRegion),
			LanguageType: "Korean",
		})
		if err != nil {
			return nil, fmt.Errorf("build tts client: %w", err)
		}
		realImage, err := dashscope.NewImageClient(httpClient, dashscope.ImageClientConfig{
			APIKey:   dashScopeAPIKey,
			Endpoint: imageEndpointForRegion(cfg.DashScopeRegion),
			Clock:    clock.RealClock{},
		})
		if err != nil {
			return nil, fmt.Errorf("build image client: %w", err)
		}
		ttsClient = realTTS
		imageClient = realImage
	}

	// Compliance audit logging — creates {outputDir}/{runID}/audit.log.
	auditLogger := pipeline.NewFileAuditLogger(cfg.OutputDir)

	imageTrack, err := pipeline.NewImageTrack(pipeline.ImageTrackConfig{
		OutputDir:     cfg.OutputDir,
		Provider:      cfg.ImageProvider,
		GenerateModel: cfg.ImageModel,
		EditModel:     cfg.ImageEditModel,
		// 16:9 at qwen-image-2.0's recommended resolution (2688×1536 =
		// 4,128,768 px, just under the 2048² total-pixel cap). YouTube's native
		// frame is 16:9; keeping image generation aligned avoids letterboxing
		// when Phase C composites them with ken_burns.
		Width:             2688,
		Height:            1536,
		Images:            imageClient,
		CharacterResolver: characterResolver,
		Shots:             segStore,
		Limiter:           limiterFactory.DashScopeImage(),
		Clock:             clock.RealClock{},
		Logger:            logger,
		AuditLogger:       auditLogger,
		RefImageFetcher:   pipeline.FetchReferenceImageAsDataURL,
	})
	if err != nil {
		return nil, fmt.Errorf("build image track: %w", err)
	}

	ttsTrack, err := pipeline.NewTTSTrack(pipeline.TTSTrackConfig{
		OutputDir:       cfg.OutputDir,
		TTSModel:        cfg.TTSModel,
		TTSVoice:        cfg.TTSVoice,
		AudioFormat:     cfg.TTSAudioFormat,
		MaxRetries:      3,
		BlockedVoiceIDs: cfg.BlockedVoiceIDs,
		AuditLogger:     auditLogger,
		TTS:             ttsClient,
		Store:           segStore,
		Limiter:         limiterFactory.DashScopeTTS(),
		Clock:           clock.RealClock{},
		Logger:          logger,
	})
	if err != nil {
		return nil, fmt.Errorf("build tts track: %w", err)
	}

	// runStore is passed as the PhaseBRunLoader: whenever image_track or the
	// tts_track is invoked, PhaseBRunner.prepareRequest resolves
	// runs.frozen_descriptor from the DB and populates
	// PhaseBRequest.FrozenDescriptorOverride. This makes AC-6 propagation
	// load-bearing at the Phase B entry point — no future wiring can forget
	// to thread the operator's edited descriptor.
	return pipeline.NewPhaseBRunner(imageTrack, ttsTrack, nil, clock.RealClock{}, logger, nil, runStore), nil
}

// makeTextGenerator routes a provider name to a concrete domain.TextGenerator.
// Supported: dashscope, deepseek, gemini. Returns an error for unknown providers
// or when the required API key is empty.
func makeTextGenerator(
	provider, apiKey string,
	limiterFactory *llmclient.ProviderLimiterFactory,
	httpClient *http.Client,
	logger *slog.Logger,
) (domain.TextGenerator, error) {
	switch provider {
	case "dashscope":
		return dashscope.NewTextClient(httpClient, dashscope.TextClientConfig{
			APIKey:  apiKey,
			Limiter: limiterFactory.DashScopeText(),
			Logger:  logger,
		})
	case "deepseek":
		return deepseek.NewTextClient(httpClient, deepseek.TextClientConfig{
			APIKey:  apiKey,
			Limiter: limiterFactory.DeepSeekText(),
		})
	case "gemini":
		return gemini.NewTextClient(httpClient, gemini.TextClientConfig{
			APIKey:  apiKey,
			Limiter: limiterFactory.GeminiText(),
		})
	default:
		return nil, fmt.Errorf("unsupported text provider %q", provider)
	}
}

// apiKeyForProvider returns the API key for a provider from the loaded env map.
func apiKeyForProvider(provider string, env map[string]string) string {
	switch provider {
	case "dashscope":
		return env[domain.SettingsSecretDashScope]
	case "deepseek":
		return env[domain.SettingsSecretDeepSeek]
	case "gemini":
		return env[domain.SettingsSecretGemini]
	default:
		return ""
	}
}

// mergeProjectEnv supplements the loaded env map with values from
// <projectRoot>/.env for any key that is empty in the loaded map.
// This allows developers to keep secrets in the repo .env without
// copying them to ~/.youtube-pipeline/.env.
func mergeProjectEnv(loaded map[string]string, projectRoot string) map[string]string {
	projectEnvPath := filepath.Join(projectRoot, ".env")
	raw, err := os.ReadFile(projectEnvPath)
	if err != nil {
		return loaded
	}
	merged := make(map[string]string, len(loaded))
	for k, v := range loaded {
		merged[k] = v
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		k, v := line[:idx], line[idx+1:]
		if merged[k] == "" && v != "" {
			merged[k] = v
		}
	}
	return merged
}

// buildPhaseARunner constructs a PhaseARunner wired with real agent
// implementations. projectRoot must be the repo root so schema files and
// prompt assets can be resolved. Returns an error if any required API key is
// missing or a schema/prompt file cannot be read.
func buildPhaseARunner(
	cfg domain.PipelineConfig,
	env map[string]string,
	projectRoot string,
	limiterFactory *llmclient.ProviderLimiterFactory,
	httpClient *http.Client,
	logger *slog.Logger,
) (*pipeline.PhaseARunner, error) {
	writerKey := apiKeyForProvider(cfg.WriterProvider, env)
	criticKey := apiKeyForProvider(cfg.CriticProvider, env)

	writerGen, err := makeTextGenerator(cfg.WriterProvider, writerKey, limiterFactory, httpClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build phase a runner: writer generator (%s): %w", cfg.WriterProvider, err)
	}
	criticGen, err := makeTextGenerator(cfg.CriticProvider, criticKey, limiterFactory, httpClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build phase a runner: critic generator (%s): %w", cfg.CriticProvider, err)
	}

	prompts, err := agents.LoadPromptAssets(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("build phase a runner: load prompts: %w", err)
	}
	terms, err := agents.LoadForbiddenTerms(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("build phase a runner: load forbidden terms: %w", err)
	}

	newV := func(schema string) (*agents.Validator, error) {
		v, verr := agents.NewValidator(projectRoot, schema)
		if verr != nil {
			return nil, fmt.Errorf("build phase a runner: validator %s: %w", schema, verr)
		}
		return v, nil
	}

	researchV, err := newV("researcher_output.schema.json")
	if err != nil {
		return nil, err
	}
	structureV, err := newV("structurer_output.schema.json")
	if err != nil {
		return nil, err
	}
	writerV, err := newV("writer_output.schema.json")
	if err != nil {
		return nil, err
	}
	visualV, err := newV("visual_breakdown.schema.json")
	if err != nil {
		return nil, err
	}
	reviewV, err := newV("reviewer_report.schema.json")
	if err != nil {
		return nil, err
	}
	criticPostWriterV, err := newV("critic_post_writer.schema.json")
	if err != nil {
		return nil, err
	}
	criticPostReviewerV, err := newV("critic_post_reviewer.schema.json")
	if err != nil {
		return nil, err
	}

	corpus := agents.NewFilesystemCorpus(cfg.DataDir)
	auditLogger := pipeline.NewFileAuditLogger(cfg.OutputDir)

	writerCfg := agents.TextAgentConfig{
		Model:       cfg.WriterModel,
		Provider:    cfg.WriterProvider,
		MaxTokens:   8192,
		Temperature: 0.7,
		AuditLogger: auditLogger,
		Logger:      logger,
	}
	criticCfg := agents.TextAgentConfig{
		Model:       cfg.CriticModel,
		Provider:    cfg.CriticProvider,
		MaxTokens:   2048,
		Temperature: 0.0,
		AuditLogger: auditLogger,
		Logger:      logger,
	}

	researcher := agents.NewResearcher(corpus, researchV)
	structurer := agents.NewStructurer(structureV)
	writer := agents.NewWriter(writerGen, writerCfg, prompts, writerV, terms)
	postWriterCritic := agents.NewPostWriterCritic(criticGen, criticCfg, prompts, writerV, criticPostWriterV, terms, cfg.WriterProvider)
	visualBreakdowner := agents.NewVisualBreakdowner(writerGen, writerCfg, prompts, visualV, agents.NewHeuristicDurationEstimator())
	reviewer := agents.NewReviewer(criticGen, criticCfg, prompts, visualV, reviewV)
	postReviewerCritic := agents.NewPostReviewerCritic(criticGen, criticCfg, prompts, writerV, visualV, reviewV, criticPostReviewerV, terms, cfg.WriterProvider)

	return pipeline.NewPhaseARunner(
		researcher, structurer, writer, postWriterCritic,
		visualBreakdowner, reviewer, postReviewerCritic,
		cfg.WriterProvider, cfg.CriticProvider,
		cfg.OutputDir, clock.RealClock{}, logger,
	)
}

type dynamicPhaseAExecutor struct {
	settings       *service.SettingsService
	projectRoot    string
	outputDir      string
	limiterFactory *llmclient.ProviderLimiterFactory
	httpClient     *http.Client
	logger         *slog.Logger
}

func (e *dynamicPhaseAExecutor) Run(ctx context.Context, state *agents.PipelineState) error {
	files, err := e.settings.LoadEffectiveRuntimeFiles(ctx)
	if err != nil {
		return fmt.Errorf("load settings for phase a run %s: %w", state.RunID, err)
	}
	// The settings DB stores config.yaml values which may not include the
	// OUTPUT_DIR env-var override applied at server startup. Pin the runner's
	// output dir to the engine's authoritative outputDir so scenario.json is
	// written to the same path that advancePhaseA checks via os.Stat.
	if e.outputDir != "" {
		files.Config.OutputDir = e.outputDir
	}
	// Merge project-root .env as a fallback for any key missing from the
	// user-config .env (~/.youtube-pipeline/.env). This lets developers keep
	// secrets in <repo>/.env without having to copy them manually.
	env := mergeProjectEnv(files.Env, e.projectRoot)
	runner, err := buildPhaseARunner(
		files.Config,
		env,
		e.projectRoot,
		e.limiterFactory,
		e.httpClient,
		e.logger,
	)
	if err != nil {
		return fmt.Errorf("build phase a runner for run %s: %w", state.RunID, err)
	}
	return runner.Run(ctx, state)
}

// buildPhaseCRuntime constructs the Phase C assembly runner and metadata
// builder used at StageAssemble. Phase C produces the final video and
// compliance bundle; it has no LLM dependency, so the runtime is wired
// directly from config without needing the settings service.
//
// Both Engine.Advance (fresh assembly entry from batch_review) and
// Engine.Resume (retry of failed assembly) call the same runner — wiring
// it once here keeps cmd/serve.go and cmd/resume.go behavior identical.
func buildPhaseCRuntime(
	cfg domain.PipelineConfig,
	runStore *db.RunStore,
	segStore *db.SegmentStore,
	logger *slog.Logger,
) (*pipeline.PhaseCRunner, pipeline.MetadataBuilder, error) {
	phaseC := pipeline.NewPhaseCRunner(segStore, runStore, nil, clock.RealClock{}, logger)

	// Metadata builder writes metadata.json + manifest.json under
	// {OutputDir}/{runID}/ as the entry action for StageMetadataAck.
	corpus := agents.NewFilesystemCorpus(cfg.DataDir)
	metaBuilder, err := pipeline.NewMetadataBuilder(pipeline.MetadataBuilderConfig{
		OutputDir:      cfg.OutputDir,
		WriterModel:    cfg.WriterModel,
		WriterProvider: cfg.WriterProvider,
		CriticModel:    cfg.CriticModel,
		CriticProvider: cfg.CriticProvider,
		ImageModel:     cfg.ImageModel,
		ImageProvider:  cfg.ImageProvider,
		TTSModel:       cfg.TTSModel,
		TTSProvider:    cfg.TTSProvider,
		TTSVoice:       cfg.TTSVoice,
		Corpus:         corpus,
		Clock:          clock.RealClock{},
		Logger:         logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build metadata builder: %w", err)
	}
	return phaseC, metaBuilder, nil
}

type dynamicPhaseBExecutor struct {
	settings          *service.SettingsService
	runStore          *db.RunStore
	segStore          *db.SegmentStore
	characterResolver pipeline.CharacterResolver
	logger            *slog.Logger
	limiterFactory    *llmclient.ProviderLimiterFactory
}

func (e *dynamicPhaseBExecutor) Run(ctx context.Context, req pipeline.PhaseBRequest) (pipeline.PhaseBResult, error) {
	// Phase B reads the current effective settings for everything except
	// dry-run mode — there is no per-run pinning of model/voice/etc., and
	// mid-execution settings changes affect only the next executor invocation,
	// never the in-flight one.
	//
	// Dry-run is the exception: the runs.dry_run column was snapshotted at
	// row creation, and that snapshot governs which clients (real DashScope
	// vs. dryrun fakes) Phase B uses for THIS run. Without the override
	// below, a Settings flip between Create and Phase B execution would
	// silently flip the run's mode mid-flight, defeating the snapshot
	// promise that "the per-run state is durable".
	files, err := e.settings.LoadEffectiveRuntimeFiles(ctx)
	if err != nil {
		return pipeline.PhaseBResult{}, fmt.Errorf("load settings for phase b run %s: %w", req.RunID, err)
	}
	run, err := e.runStore.Get(ctx, req.RunID)
	if err != nil {
		return pipeline.PhaseBResult{}, fmt.Errorf("load run for phase b %s: %w", req.RunID, err)
	}
	cfg := files.Config
	cfg.DryRun = run.DryRun
	runner, err := buildPhaseBRunner(
		cfg,
		files.Env[domain.SettingsSecretDashScope],
		e.limiterFactory,
		e.runStore,
		e.segStore,
		e.characterResolver,
		e.logger,
	)
	if err != nil {
		return pipeline.PhaseBResult{}, err
	}
	return runner.Run(ctx, req)
}

func newServeCmd() *cobra.Command {
	var (
		port    int
		devMode bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API server (localhost only)",
		Long: `Start the youtube.pipeline HTTP server bound to 127.0.0.1.

Use --dev to proxy non-/api/ requests to the Vite dev server (default: localhost:5173).
Without --dev, the embedded SPA is served directly.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, port, devMode)
		},
	}

	cmd.Flags().IntVar(&port, "port", 8080, "port to listen on (bound to 127.0.0.1 only)")
	cmd.Flags().BoolVar(&devMode, "dev", false, "proxy frontend requests to Vite dev server")
	return cmd
}

func runServe(cmd *cobra.Command, port int, devMode bool) error {
	cfg, err := config.Load(cfgPath, config.DefaultEnvPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	store := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	decisionStore := db.NewDecisionStore(database)
	settingsStore := db.NewSettingsStore(database)
	criticReportStore := db.NewCriticReportStore(database)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// In-process workers do not survive a restart. Any run still in 'running'
	// at startup is orphaned: flip it to 'failed' so the operator gets a
	// FailureBanner + Resume button instead of a permanently-stuck
	// "STAGE IN PROGRESS" screen.
	if n, err := store.ReconcileOrphanedRuns(cmd.Context()); err != nil {
		return fmt.Errorf("reconcile orphaned runs: %w", err)
	} else if n > 0 {
		logger.Info("reconciled orphaned runs at startup", "count", n)
	}
	settingsFiles := config.NewSettingsFileManager(cfgPath, config.DefaultEnvPath())
	settingsSvc := service.NewSettingsService(settingsStore, settingsFiles, clock.RealClock{})

	if err := settingsSvc.Bootstrap(cmd.Context()); err != nil {
		return fmt.Errorf("bootstrap settings: %w", err)
	}

	engine := pipeline.NewEngine(store, segStore, decisionStore, clock.RealClock{}, cfg.OutputDir, logger)
	engine.SetHITLSessionStore(newHITLSessionStoreAdapter(decisionStore))
	engine.SetCriticReportStore(criticReportStore)
	engine.SetNarrationSeeder(segStore)
	svc := service.NewRunService(store, engine)
	svc.SetAdvancer(engine)
	svc.SetHITLSessionStore(newHITLSessionStoreAdapter(decisionStore), clock.RealClock{})

	limiterFactory, err := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 10, MaxConcurrent: 2, AcquireTimeout: 30 * time.Second},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 5 * time.Minute},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 30 * time.Second},
	}, clock.RealClock{})
	if err != nil {
		return fmt.Errorf("build limiter factory: %w", err)
	}

	// Character service must be constructed before the Phase B executor so
	// it can be threaded into image_track as the CharacterResolver port.
	// Without this, character-shot Edit calls have no resolved reference URL
	// and image_track surfaces the "selected character has no image
	// reference" validation error before any provider call.
	characterCache := db.NewCharacterCacheStore(database)
	characterClient := service.NewDuckDuckGoClient(nil)
	characterSvc := service.NewCharacterService(store, characterCache, characterClient)
	characterSvc.SetOutputDir(cfg.OutputDir)
	characterSvc.SetDescriptorRecorder(decisionStore)

	engine.SetPhaseBExecutor(&dynamicPhaseBExecutor{
		settings:          settingsSvc,
		runStore:          store,
		segStore:          segStore,
		characterResolver: characterSvc,
		logger:            logger,
		limiterFactory:    limiterFactory,
	})

	// Phase C runs assembly + metadata entry. Wiring it here makes
	// Engine.Advance(StageAssemble) and Engine.Resume(StageAssemble) both
	// use the same runner — without this, the engine would reject Phase C
	// dispatch with a validation error.
	phaseCRunner, metaBuilder, err := buildPhaseCRuntime(cfg, store, segStore, logger)
	if err != nil {
		return fmt.Errorf("build phase c runtime: %w", err)
	}
	engine.SetPhaseCRunner(phaseCRunner)
	engine.SetPhaseCMetadataBuilder(metaBuilder)
	hitlSvc := service.NewHITLService(store, decisionStore, logger)
	// Read-time backfill: silently create the missing hitl_sessions row when
	// BuildStatus observes a run already in a HITL wait state without a
	// session anchor (the UI keeps polling and the WARN spam is replaced by
	// a single INFO line). DecisionStore satisfies HITLSessionWriter.
	hitlSvc.SetSessionWriter(decisionStore)
	sceneSvc := service.NewSceneService(store, segStore, decisionStore, clock.RealClock{})
	sceneSvc.SetSceneRegenerator(service.NewNoOpSceneRegenerator(segStore))
	// outputDir lets ListScenes / ListReviewItems resolve the scenario.json
	// path stored relative to {outputDir}/{runID}/. Without this, both
	// surfaces 404 on any run whose segments rows lack narration text.
	sceneSvc.SetNarrationSeeder(segStore, cfg.OutputDir)

	// projectRoot for Tuning is the process working directory. Prompts,
	// Golden fixtures, and manifest.json are all repo-relative artifacts,
	// so cwd must be the repo root at server start.
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}

	// Phase A runs synchronously on the /advance path. Settings are resolved
	// per-run at invocation time so provider/model changes take effect without
	// restarting the server. projectRoot must be resolved first (above).
	engine.SetPhaseAExecutor(&dynamicPhaseAExecutor{
		settings:       settingsSvc,
		projectRoot:    projectRoot,
		outputDir:      cfg.OutputDir,
		limiterFactory: limiterFactory,
		httpClient:     &http.Client{Timeout: 120 * time.Second},
		logger:         logger,
	})
	// Build the real Critic-backed evaluator up front and fail-loud on
	// construction error: the schema files and forbidden-terms loader are
	// repo-local artifacts that must always be present, so a failure here is
	// a real misconfiguration, not a "running without API key" dev shortcut.
	// Falling back to NotConfiguredEvaluator{} would resurrect the silent-
	// degrade-to-placeholder mode this story exists to remove.
	tuningEvaluator, err := eval.NewRuntimeEvaluator(eval.RuntimeEvaluatorOptions{
		ProjectRoot: projectRoot,
		Runtime:     settingsSvc,
		HTTPClient:  &http.Client{Timeout: 120 * time.Second},
		Limiter:     limiterFactory.DeepSeekText(),
	})
	if err != nil {
		return fmt.Errorf("build tuning runtime evaluator: %w", err)
	}
	tuningSvc := service.NewTuningService(service.TuningServiceOptions{
		ProjectRoot:  projectRoot,
		OutputDir:    cfg.OutputDir,
		Evaluator:    tuningEvaluator,
		ShadowSource: eval.NewSQLiteShadowSource(database),
		Calibration:  db.NewCalibrationStore(database),
		Clock:        clock.RealClock{},
	})
	// RunService stamps newly-created runs with the active Critic prompt
	// version so later metrics can group runs by prompt version (AC-3).
	svc.SetPromptVersionProvider(tuningSvc)
	// And with the effective Phase B dry-run flag so the run row carries
	// its own snapshot — Settings toggles after creation don't retroactively
	// change in-flight or completed runs.
	svc.SetDryRunProvider(settingsSvc)

	deps := api.NewDependencies(svc, settingsSvc, hitlSvc, characterSvc, sceneSvc, segStore, tuningSvc, cfg.OutputDir, logger, web.FS)
	mux := http.NewServeMux()
	if err := configureServeMux(mux, deps, devMode, mustParseURL(viteDevServerURL), cmd.OutOrStdout()); err != nil {
		return err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Listening on http://%s\n", addr)

	// Graceful shutdown on SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Separate channels for server-start failure vs. clean exit so that a
	// graceful ErrServerClosed never surfaces as a bogus "server error: <nil>".
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			return nil
		}
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		fmt.Fprintln(cmd.OutOrStdout(), "\nShutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}

func configureServeMux(
	mux *http.ServeMux,
	deps *api.Dependencies,
	devMode bool,
	devProxyURL *url.URL,
	out io.Writer,
) error {
	if mux == nil {
		return fmt.Errorf("configure serve mux: nil mux")
	}
	if deps == nil {
		return fmt.Errorf("configure serve mux: nil dependencies")
	}

	if devMode {
		deps.WebFS = nil
		api.RegisterRoutes(mux, deps)
		mux.Handle("/", newDevFrontendProxy(devProxyURL))
		if out != nil {
			fmt.Fprintf(out, "Dev mode: Go serves /api/*, proxying frontend to %s\n", devProxyURL.String())
		}
		return nil
	}

	api.RegisterRoutes(mux, deps)
	return nil
}

func newDevFrontendProxy(target *url.URL) *httputil.ReverseProxy {
	return httputil.NewSingleHostReverseProxy(target)
}

func mustParseURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}
