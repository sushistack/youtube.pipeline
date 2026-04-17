package testutil

import "os"

func init() {
	if os.Getenv("CI") != "true" {
		return
	}
	keys := []string{"DASHSCOPE_API_KEY", "DEEPSEEK_API_KEY", "GEMINI_API_KEY"}
	for _, k := range keys {
		if os.Getenv(k) != "" {
			panic("API keys must not be set in CI environment")
		}
	}
}
