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
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dashscope"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/web"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const viteDevServerURL = "http://localhost:5173"

// buildPhaseBRunner constructs a PhaseBRunner with a real TTS track backed by
// the DashScope qwen3-tts-flash client. Returns an error if the API key is
// missing or any construction step fails; the caller decides how to handle the
// absence (warn + skip vs. fatal).
func buildPhaseBRunner(
	cfg domain.PipelineConfig,
	runStore *db.RunStore,
	segStore *db.SegmentStore,
	logger *slog.Logger,
) (*pipeline.PhaseBRunner, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY not set")
	}

	ttsClient, err := dashscope.NewTTSClient(&http.Client{Timeout: 120 * time.Second}, dashscope.TTSClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("build tts client: %w", err)
	}

	limiterFactory, err := llmclient.NewProviderLimiterFactory(llmclient.ProviderLimiterConfig{
		DashScope: llmclient.LimitConfig{RequestsPerMinute: 10, MaxConcurrent: 2, AcquireTimeout: 30 * time.Second},
		DeepSeek:  llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 30 * time.Second},
		Gemini:    llmclient.LimitConfig{RequestsPerMinute: 60, MaxConcurrent: 5, AcquireTimeout: 30 * time.Second},
	}, clock.RealClock{})
	if err != nil {
		return nil, fmt.Errorf("build limiter factory: %w", err)
	}

	// Compliance audit logging — creates {outputDir}/{runID}/audit.log.
	auditLogger := pipeline.NewFileAuditLogger(cfg.OutputDir)

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

	// Image track requires further wiring (Story 5.4); use a no-op stub here
	// so the runner can be instantiated without the image plumbing.
	imageTrackStub := pipeline.ImageTrack(func(_ context.Context, req pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
		return pipeline.ImageTrackResult{Observation: domain.NewStageObservation(domain.StageImage)}, nil
	})

	// runStore is passed as the PhaseBRunLoader: whenever image_track or the
	// tts_track is invoked, PhaseBRunner.prepareRequest resolves
	// runs.frozen_descriptor from the DB and populates
	// PhaseBRequest.FrozenDescriptorOverride. This makes AC-6 propagation
	// load-bearing at the Phase B entry point — no future wiring can forget
	// to thread the operator's edited descriptor.
	return pipeline.NewPhaseBRunner(imageTrackStub, ttsTrack, nil, clock.RealClock{}, logger, nil, runStore), nil
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Build Phase B TTS track. Requires DASHSCOPE_API_KEY in environment.
	// The TTS track and image track share the same DashScope limiter budget
	// via ProviderLimiterFactory.DashScopeTTS() == DashScopeImage().
	phaseBRunner, err := buildPhaseBRunner(cfg, store, segStore, logger)
	if err != nil {
		logger.Warn("phase b runner unavailable (TTS disabled)", "error", err.Error())
		phaseBRunner = nil
	}
	_ = phaseBRunner // wired into engine.SetPhaseAExecutor when Phase A lands

	engine := pipeline.NewEngine(store, segStore, decisionStore, clock.RealClock{}, cfg.OutputDir, logger)
	engine.SetPhaseBExecutor(phaseBRunner)
	svc := service.NewRunService(store, engine)
	hitlSvc := service.NewHITLService(store, decisionStore, logger)
	characterCache := db.NewCharacterCacheStore(database)
	characterClient := service.NewDuckDuckGoClient(nil)
	characterSvc := service.NewCharacterService(store, characterCache, characterClient)
	characterSvc.SetDescriptorRecorder(decisionStore)
	sceneSvc := service.NewSceneService(store, segStore, decisionStore, clock.RealClock{})
	sceneSvc.SetSceneRegenerator(service.NewNoOpSceneRegenerator(segStore))

	deps := api.NewDependencies(svc, hitlSvc, characterSvc, sceneSvc, cfg.OutputDir, logger, web.FS)
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
