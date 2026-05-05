package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/sushistack/youtube.pipeline/internal/llmclient/comfyui"
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
	canonicalResolver pipeline.CanonicalImageResolver,
	logger *slog.Logger,
) (*pipeline.PhaseBRunner, error) {
	if limiterFactory == nil {
		return nil, fmt.Errorf("phase b runner: nil limiter factory")
	}
	if characterResolver == nil {
		return nil, fmt.Errorf("phase b runner: nil character resolver")
	}
	// Whitelist image providers up front so a typo like "comfy_ui" or "ComfyUI"
	// fails fast at server start instead of silently routing to the dashscope
	// branch. Empty value is rejected by validateSettingsConfig before this
	// point, but defense-in-depth.
	switch cfg.ImageProvider {
	case "dashscope", "comfyui":
	default:
		return nil, fmt.Errorf("phase b runner: %w: unknown image_provider %q", domain.ErrValidation, cfg.ImageProvider)
	}
	// DashScope API key is only required when at least one of image/tts routes
	// through DashScope. TTSProvider defaults to "dashscope" when empty, so the
	// empty-string clause keeps existing behavior. ComfyUI image + DashScope
	// TTS is the expected production combo and still needs the key.
	usingDashScope := cfg.ImageProvider == "dashscope" || cfg.TTSProvider == "dashscope" || cfg.TTSProvider == ""
	if !cfg.DryRun && usingDashScope && dashScopeAPIKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY not set")
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}

	// imageLimiter is selected to match the image client below. ComfyUI runs
	// locally with its own queue so it must not consume the DashScope token
	// budget; DashScope and dry-run continue to share the DashScope limiter
	// pointer so existing budget invariants hold.
	imageLimiter := limiterFactory.DashScopeImage()

	var (
		ttsClient   domain.TTSSynthesizer
		imageClient domain.ImageGenerator
	)
	if cfg.DryRun {
		logger.Info("phase b dry-run mode active: image + tts calls swapped for placeholder fakes")
		ttsClient = dryrun.NewTTSClient()
		imageClient = dryrun.NewImageClient()
	} else if cfg.ImageProvider == "comfyui" {
		// Local ComfyUI image path. TTS still routes through DashScope —
		// this branch only swaps the image client (and its limiter).
		realTTS, err := dashscope.NewTTSClient(httpClient, dashscope.TTSClientConfig{
			APIKey:       dashScopeAPIKey,
			Endpoint:     dashscope.DefaultTTSEndpointIntl,
			LanguageType: "Korean",
		})
		if err != nil {
			return nil, fmt.Errorf("build tts client: %w", err)
		}
		comfyClient, err := comfyui.NewImageClient(httpClient, comfyui.ImageClientConfig{
			Endpoint:          cfg.ComfyUIEndpoint,
			Clock:             clock.RealClock{},
			LoRAName:          cfg.ComfyUILoRAName,
			LoRAStrengthModel: cfg.ComfyUILoRAStrengthModel,
			LoRAStrengthClip:  cfg.ComfyUILoRAStrengthClip,
		})
		if err != nil {
			return nil, fmt.Errorf("build comfyui image client: %w", err)
		}
		logger.Info("phase b image provider: comfyui",
			"endpoint", cfg.ComfyUIEndpoint,
			"generate_model", cfg.ImageModel,
			"edit_model", cfg.ImageEditModel,
			"lora_name", cfg.ComfyUILoRAName,
			"lora_strength_model", cfg.ComfyUILoRAStrengthModel,
			"lora_strength_clip", cfg.ComfyUILoRAStrengthClip,
		)
		ttsClient = realTTS
		imageClient = comfyClient
		imageLimiter = limiterFactory.ComfyUIImage()
	} else {
		realTTS, err := dashscope.NewTTSClient(httpClient, dashscope.TTSClientConfig{
			APIKey:       dashScopeAPIKey,
			Endpoint:     dashscope.DefaultTTSEndpointIntl,
			LanguageType: "Korean",
		})
		if err != nil {
			return nil, fmt.Errorf("build tts client: %w", err)
		}
		realImage, err := dashscope.NewImageClient(httpClient, dashscope.ImageClientConfig{
			APIKey:   dashScopeAPIKey,
			Endpoint: dashscope.DefaultImageEndpointIntl,
			Clock:    clock.RealClock{},
		})
		if err != nil {
			return nil, fmt.Errorf("build image client: %w", err)
		}
		logger.Info("phase b image provider: dashscope",
			"endpoint", dashscope.DefaultImageEndpointIntl,
			"generate_model", cfg.ImageModel,
			"edit_model", cfg.ImageEditModel,
		)
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
		CanonicalResolver: canonicalResolver,
		ScpImageDir:       cfg.ScpImageDir,
		Shots:             segStore,
		Limiter:           imageLimiter,
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
		MaxInputBytes:   cfg.TTSMaxInputBytes,
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
			APIKey:   apiKey,
			Endpoint: dashscope.DefaultChatEndpointIntl,
			Limiter:  limiterFactory.DashScopeText(),
			Logger:   logger,
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
// Now that the project-root layout puts the canonical .env at
// <projectRoot>/.env this is largely redundant in normal use, but it
// stays as a safety net for legacy operators who pointed --config at a
// custom directory whose .env is sparse.
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

// sha256Hex returns the lowercase hex sha256 of s. Used to build
// FingerprintInputs.PromptTemplateSHA without hitting the filesystem again
// after LoadPromptAssets has already read the files into memory.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
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
	segmenterKey := apiKeyForProvider(cfg.SegmenterProvider, env)

	writerGen, err := makeTextGenerator(cfg.WriterProvider, writerKey, limiterFactory, httpClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build phase a runner: writer generator (%s): %w", cfg.WriterProvider, err)
	}
	// Segmenter routed to its own provider (qwen-plus on dashscope vs
	// deepseek on writer). Same-provider reuses the limiter via factory.
	segmenterGen, err := makeTextGenerator(cfg.SegmenterProvider, segmenterKey, limiterFactory, httpClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build phase a runner: segmenter generator (%s): %w", cfg.SegmenterProvider, err)
	}
	criticGen, err := makeTextGenerator(cfg.CriticProvider, criticKey, limiterFactory, httpClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build phase a runner: critic generator (%s): %w", cfg.CriticProvider, err)
	}

	prompts, err := agents.LoadPromptAssets(projectRoot, cfg.UseTemplatePrompts)
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

	// TraceWriter: active only when observability.debug_traces=true.
	// NoopTraceWriter makes the hot path zero-syscall.
	var traceWriter domain.TraceWriter = pipeline.NoopTraceWriter{}
	if cfg.Observability.DebugTraces {
		traceWriter = pipeline.NewFileTraceWriter(cfg.OutputDir)
	}

	writerCfg := agents.TextAgentConfig{
		Model:    cfg.WriterModel,
		Provider: cfg.WriterProvider,
		// v2 stage-1: per-act monologue output budget. Reuses the cycle-C
		// 12288-token headroom (originally tuned for v1 per-act envelopes
		// at temp 0.7); v2 monologues are larger units (~480–2080 runes
		// per act) and the JSON envelope adds metadata overhead, so the
		// retry-on-truncation policy stays the safety net.
		MaxTokens:   12288,
		Temperature: 0.7,
		AuditLogger: auditLogger,
		TraceWriter: traceWriter,
		Logger:      logger,
	}
	// v2 stage-2: beat segmenter (qwen-plus per spec D1). Offset arithmetic
	// over a fixed monologue is a deterministic-shaped task — temperature 0
	// reduces re-roll variance on the structured output. MaxTokens 4096 is
	// generous for a 10-beat JSON array (each beat ~150 chars metadata).
	segmenterCfg := agents.TextAgentConfig{
		Model:       cfg.SegmenterModel,
		Provider:    cfg.SegmenterProvider,
		MaxTokens:   4096,
		Temperature: 0.0,
		AuditLogger: auditLogger,
		TraceWriter: traceWriter,
		Logger:      logger,
	}
	criticCfg := agents.TextAgentConfig{
		Model:       cfg.CriticModel,
		Provider:    cfg.CriticProvider,
		MaxTokens:   2048,
		Temperature: 0.0,
		AuditLogger: auditLogger,
		TraceWriter: traceWriter,
		Logger:      logger,
	}

	roleClassifierCfg := agents.TextAgentConfig{
		Model:       cfg.WriterModel,
		Provider:    cfg.WriterProvider,
		MaxTokens:   1024,
		Temperature: 0.0,
		Concurrency: 1,
		AuditLogger: auditLogger,
		TraceWriter: traceWriter,
		Logger:      logger,
	}
	polisherCfg := agents.TextAgentConfig{
		Model:    cfg.WriterModel,
		Provider: cfg.WriterProvider,
		// Full script in one shot. 12288 was hit (finish_reason=length) on
		// SCP-049 dogfood — JSON-wrapped polished script ran ~2× the input
		// token count (~6.4K in → 12.3K+ out). 16384 leaves headroom; at
		// observed ~85 tok/s on deepseek-v4-flash that's ~3min, well under
		// the 5min HTTP timeout.
		MaxTokens:   16384,
		Temperature: 0.5,
		AuditLogger: auditLogger,
		TraceWriter: traceWriter,
		Logger:      logger,
	}

	// FingerprintInputs for deterministic stages. Researcher fingerprint covers
	// the role-classifier prompt (the only LLM dependency in that stage) plus
	// model/provider/schema. Structurer has no LLM dependency — its fingerprint
	// only tracks schema versioning so stale caches are invalidated on struct
	// layout changes.
	fps := map[agents.PipelineStage]pipeline.FingerprintInputs{
		agents.StageResearcher: {
			SourceVersion:     domain.SourceVersionV1,
			PromptTemplateSHA: sha256Hex(prompts.RoleClassifierTemplate),
			FewshotSHA:        "",
			Model:             cfg.WriterModel,
			Provider:          cfg.WriterProvider,
			SchemaVersion:     "v1",
		},
		agents.StageStructurer: {
			SourceVersion:     domain.SourceVersionV1,
			PromptTemplateSHA: "",
			FewshotSHA:        "",
			Model:             "",
			Provider:          "",
			SchemaVersion:     "v1",
		},
	}

	researcher := agents.NewResearcher(corpus, researchV, writerGen, roleClassifierCfg, prompts)
	structurer := agents.NewStructurer(structureV)
	writer := agents.NewWriter(writerGen, segmenterGen, writerCfg, segmenterCfg, prompts, writerV, terms)
	polisher := agents.NewPolisher(writerGen, polisherCfg, prompts, writerV, terms)
	postWriterCritic := agents.NewPostWriterCritic(criticGen, criticCfg, prompts, writerV, criticPostWriterV, terms, cfg.WriterProvider)
	visualBreakdowner := agents.NewVisualBreakdowner(writerGen, writerCfg, prompts, visualV, agents.NewHeuristicDurationEstimator())
	reviewer := agents.NewReviewer(criticGen, criticCfg, prompts, visualV, reviewV)
	postReviewerCritic := agents.NewPostReviewerCritic(criticGen, criticCfg, prompts, writerV, visualV, reviewV, criticPostReviewerV, terms, cfg.WriterProvider)

	runner, err := pipeline.NewPhaseARunner(
		researcher, structurer, writer, polisher, postWriterCritic,
		visualBreakdowner, reviewer, postReviewerCritic,
		cfg.WriterProvider, cfg.CriticProvider,
		cfg.OutputDir, clock.RealClock{}, logger,
	)
	if err != nil {
		return nil, err
	}
	return runner.WithCacheFingerprints(fps), nil
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
	// loaded .env. With the project-root layout the loaded .env IS the
	// project-root .env, so this is a no-op in normal use; it still fires
	// when --config points at a custom directory whose .env lacks keys.
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

// buildCanonicalImageGenerator constructs the ImageGenerator used for SCP
// canonical cartoon image generation. Pinned at server start using bootstrap
// config — separate from Phase B's per-run image client (which reads runtime
// settings via dynamicPhaseBExecutor) because canonical generation is a
// rare HITL-triggered operation where mid-flight provider swap has no
// real-world value. Operator restarts the server to pick up provider changes.
func buildCanonicalImageGenerator(
	cfg domain.PipelineConfig,
	dashScopeKey string,
	limiterFactory *llmclient.ProviderLimiterFactory,
	logger *slog.Logger,
) (domain.ImageGenerator, error) {
	httpClient := &http.Client{Timeout: 10 * time.Minute}
	switch cfg.ImageProvider {
	case "comfyui":
		client, err := comfyui.NewImageClient(httpClient, comfyui.ImageClientConfig{
			Endpoint:          cfg.ComfyUIEndpoint,
			Clock:             clock.RealClock{},
			LoRAName:          cfg.ComfyUILoRAName,
			LoRAStrengthModel: cfg.ComfyUILoRAStrengthModel,
			LoRAStrengthClip:  cfg.ComfyUILoRAStrengthClip,
		})
		if err != nil {
			return nil, fmt.Errorf("comfyui client: %w", err)
		}
		logger.Info("canonical image provider: comfyui",
			"endpoint", cfg.ComfyUIEndpoint, "edit_model", cfg.ImageEditModel)
		return client, nil
	case "dashscope":
		if dashScopeKey == "" {
			return nil, fmt.Errorf("DASHSCOPE_API_KEY required when image_provider=dashscope")
		}
		client, err := dashscope.NewImageClient(httpClient, dashscope.ImageClientConfig{
			APIKey:   dashScopeKey,
			Endpoint: dashscope.DefaultImageEndpointIntl,
			Clock:    clock.RealClock{},
		})
		if err != nil {
			return nil, fmt.Errorf("dashscope client: %w", err)
		}
		logger.Info("canonical image provider: dashscope",
			"endpoint", dashscope.DefaultImageEndpointIntl, "edit_model", cfg.ImageEditModel)
		return client, nil
	default:
		return nil, fmt.Errorf("unknown image_provider %q", cfg.ImageProvider)
	}
}

// dashScopeAPIKey reads the DashScope API key from process env. The .env file
// has already been loaded into the process at config.Load time. Empty when
// unset — only fatal if the active image/TTS provider is dashscope.
func dashScopeAPIKey() string {
	return os.Getenv("DASHSCOPE_API_KEY")
}

type dynamicPhaseBExecutor struct {
	settings          *service.SettingsService
	runStore          *db.RunStore
	segStore          *db.SegmentStore
	characterResolver pipeline.CharacterResolver
	canonicalResolver pipeline.CanonicalImageResolver
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
		e.canonicalResolver,
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
	// Rewind wiring: cancel registry tracks in-flight stage execution so a
	// rewind can interrupt and wait for clean unwinding before destructive
	// cleanup. RunStore exposes the rewind-only DB primitives.
	engine.SetCancelRegistry(pipeline.NewCancelRegistry())
	engine.SetRewindStore(store)
	svc := service.NewRunService(store, engine)
	svc.SetAdvancer(engine)
	svc.SetRewinder(engine)
	svc.SetCanceller(engine)
	svc.SetHITLSessionStore(newHITLSessionStoreAdapter(decisionStore), clock.RealClock{})

	limiterFactory, err := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 10, MaxConcurrent: 2, AcquireTimeout: 30 * time.Second},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 5 * time.Minute},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 30 * time.Second},
		// RPM=600 is a high-bound; ComfyUI's queue is the real throttle.
		// MaxConcurrent=1 prevents GPU OOM on the local FLUX.2 Klein 4B model.
		// AcquireTimeout=10m must exceed the 300s polling cap + cold-start (~180s).
		ComfyUI: llmclient.LimitConfig{RequestsPerMinute: 600, MaxConcurrent: 1, AcquireTimeout: 10 * time.Minute},
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

	// SCP canonical image library — Cast-stage cartoon canonical generation
	// + Phase B reference fallback. Pinned at server start using bootstrap
	// config; Settings changes to image provider/endpoint require a restart
	// to take effect for canonical generation. Phase B's image_track still
	// reads runtime settings via dynamicPhaseBExecutor — it just consults
	// this resolver before falling back to the DDG candidate URL.
	scpImageStore := db.NewScpImageLibraryStore(database)
	canonicalImageClient, err := buildCanonicalImageGenerator(cfg, dashScopeAPIKey(), limiterFactory, logger)
	if err != nil {
		return fmt.Errorf("build canonical image generator: %w", err)
	}
	scpImageSvc, err := service.NewScpImageService(service.ScpImageServiceConfig{
		Runs:           store,
		Cache:          characterCache,
		Library:        scpImageStore,
		Images:         canonicalImageClient,
		RefFetcher:     pipeline.FetchReferenceImageAsDataURL,
		EditModel:      cfg.ImageEditModel,
		StylePrompt:    cfg.CartoonStylePrompt,
		ScpImageDir:    cfg.ScpImageDir,
		CanonicalWidth: cfg.ScpCanonicalWidth,
		CanonicalHt:    cfg.ScpCanonicalHeight,
	})
	if err != nil {
		return fmt.Errorf("build scp image service: %w", err)
	}

	engine.SetPhaseBExecutor(&dynamicPhaseBExecutor{
		settings:          settingsSvc,
		runStore:          store,
		segStore:          segStore,
		characterResolver: characterSvc,
		canonicalResolver: scpImageSvc,
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
		// Polisher generates the full polished script in one shot (MaxTokens=12288).
		// At ~50 tok/s that's ~4min, so 120s timed out the body-read and forced
		// the silent-fallback regime. 5min gives headroom; writer/critic finish
		// well under it, so the ceiling is only hit by polisher worst-case.
		httpClient: &http.Client{Timeout: 5 * time.Minute},
		logger:     logger,
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

	deps := api.NewDependencies(svc, settingsSvc, hitlSvc, characterSvc, scpImageSvc, sceneSvc, segStore, tuningSvc, cfg.OutputDir, cfg.ScpImageDir, logger, web.FS)
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
