package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/deepseek"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

const (
	criticPostWriterSchemaFile = "critic_post_writer.schema.json"
)

type RuntimeFilesLoader interface {
	LoadEffectiveRuntimeFiles(ctx context.Context) (domain.SettingsFileSnapshot, error)
}

type RuntimeEvaluatorOptions struct {
	ProjectRoot string
	Runtime     RuntimeFilesLoader
	HTTPClient  *http.Client
	Limiter     *llmclient.CallLimiter
	Endpoint    string
}

type RuntimeEvaluator struct {
	projectRoot     string
	runtime         RuntimeFilesLoader
	httpClient      *http.Client
	limiter         *llmclient.CallLimiter
	endpoint        string
	writerValidator *agents.Validator
	criticValidator *agents.Validator
	forbiddenTerms  *agents.ForbiddenTerms
}

func NewRuntimeEvaluator(opts RuntimeEvaluatorOptions) (*RuntimeEvaluator, error) {
	if opts.ProjectRoot == "" {
		return nil, fmt.Errorf("runtime evaluator: %w: project root is empty", domain.ErrValidation)
	}
	if opts.Runtime == nil {
		return nil, fmt.Errorf("runtime evaluator: %w: runtime loader is nil", domain.ErrValidation)
	}
	if opts.HTTPClient == nil {
		return nil, fmt.Errorf("runtime evaluator: %w: http client is nil", domain.ErrValidation)
	}
	if opts.Limiter == nil {
		return nil, fmt.Errorf("runtime evaluator: %w: limiter is nil", domain.ErrValidation)
	}

	writerValidator, err := agents.NewValidator(opts.ProjectRoot, inputSchemaFile)
	if err != nil {
		return nil, fmt.Errorf("runtime evaluator: %w", err)
	}
	criticValidator, err := agents.NewValidator(opts.ProjectRoot, criticPostWriterSchemaFile)
	if err != nil {
		return nil, fmt.Errorf("runtime evaluator: %w", err)
	}
	forbiddenTerms, err := agents.LoadForbiddenTerms(opts.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("runtime evaluator: %w", err)
	}

	return &RuntimeEvaluator{
		projectRoot:     opts.ProjectRoot,
		runtime:         opts.Runtime,
		httpClient:      opts.HTTPClient,
		limiter:         opts.Limiter,
		endpoint:        opts.Endpoint,
		writerValidator: writerValidator,
		criticValidator: criticValidator,
		forbiddenTerms:  forbiddenTerms,
	}, nil
}

func (e *RuntimeEvaluator) Evaluate(ctx context.Context, fixture Fixture) (VerdictResult, error) {
	files, err := e.runtime.LoadEffectiveRuntimeFiles(ctx)
	if err != nil {
		return VerdictResult{}, fmt.Errorf("runtime evaluator: load runtime files: %w", err)
	}
	if files.Config.CriticProvider != "deepseek" {
		return VerdictResult{}, fmt.Errorf(
			"runtime evaluator: critic provider %q is not supported for tuning; expected deepseek: %w",
			files.Config.CriticProvider,
			domain.ErrValidation,
		)
	}
	apiKey := files.Env[domain.SettingsSecretDeepSeek]
	if apiKey == "" {
		return VerdictResult{}, fmt.Errorf("runtime evaluator: missing DEEPSEEK_API_KEY: %w", domain.ErrValidation)
	}

	gen, err := deepseek.NewTextClient(e.httpClient, deepseek.TextClientConfig{
		APIKey:   apiKey,
		Endpoint: e.endpoint,
		Limiter:  e.limiter,
	})
	if err != nil {
		return VerdictResult{}, fmt.Errorf("runtime evaluator: build deepseek client: %w", err)
	}
	prompts, err := agents.LoadPromptAssets(e.projectRoot, files.Config.UseTemplatePrompts)
	if err != nil {
		return VerdictResult{}, fmt.Errorf("runtime evaluator: load prompt assets: %w", err)
	}

	var script domain.NarrationScript
	if err := json.Unmarshal(fixture.Input, &script); err != nil {
		return VerdictResult{}, fmt.Errorf("runtime evaluator: decode fixture input: %w: %v", domain.ErrValidation, err)
	}

	state := &agents.PipelineState{
		RunID:     fixture.FixtureID,
		SCPID:     script.SCPID,
		Narration: &script,
	}
	// Fail loudly when fixture metadata lacks writer_provider rather than
	// forging a "dashscope" default: silently mislabeling the baseline writer
	// would lie in the Critic audit trail and corrupt Shadow per-case
	// provider attribution. NewPostWriterCritic also rejects empty strings,
	// so this surfaces a clear ErrValidation instead of a generic
	// "writer provider is empty" buried under runtime evaluator framing.
	if script.Metadata.WriterProvider == "" {
		return VerdictResult{}, fmt.Errorf(
			"runtime evaluator: fixture %s missing metadata.writer_provider: %w",
			fixture.FixtureID, domain.ErrValidation,
		)
	}
	agent := agents.NewPostWriterCritic(
		gen,
		agents.TextAgentConfig{
			Model:       files.Config.CriticModel,
			Provider:    files.Config.CriticProvider,
			MaxTokens:   1600,
			Temperature: 0.1,
		},
		prompts,
		e.writerValidator,
		e.criticValidator,
		e.forbiddenTerms,
		script.Metadata.WriterProvider,
	)
	if err := agent(ctx, state); err != nil {
		return VerdictResult{}, fmt.Errorf("runtime evaluator: evaluate fixture %s: %w", fixture.FixtureID, err)
	}
	if state.Critic == nil || state.Critic.PostWriter == nil {
		return VerdictResult{}, fmt.Errorf("runtime evaluator: missing post_writer report: %w", domain.ErrStageFailed)
	}

	report := state.Critic.PostWriter
	return VerdictResult{
		Verdict:      report.Verdict,
		RetryReason:  report.RetryReason,
		OverallScore: report.OverallScore,
		Model:        report.CriticModel,
		Provider:     report.CriticProvider,
	}, nil
}
