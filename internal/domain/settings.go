package domain

const (
	SettingsSecretDashScope = "DASHSCOPE_API_KEY"
	SettingsSecretDeepSeek  = "DEEPSEEK_API_KEY"
	SettingsSecretGemini    = "GEMINI_API_KEY"
)

type SettingsFileSnapshot struct {
	Config PipelineConfig
	Env    map[string]string
}

type SettingsBudgetRun struct {
	ID      string
	Status  string
	CostUSD float64
}
