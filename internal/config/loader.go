package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// Load reads configuration from the Viper hierarchy:
//   1. Defaults from domain.DefaultConfig()
//   2. .env file (secrets) — populates environment variables
//   3. config.yaml (non-secret settings)
//   4. Environment variables (override yaml)
//   5. CLI flags (highest priority, bound via BindFlags)
func Load(cfgPath, envPath string) (domain.PipelineConfig, error) {
	cfg := domain.DefaultConfig()

	// Load .env into process environment (secrets only).
	if envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			if err := godotenv.Load(envPath); err != nil {
				return cfg, fmt.Errorf("load .env: %w", err)
			}
		}
	}

	v := viper.New()

	// Set defaults from DefaultConfig.
	v.SetDefault("writer_model", cfg.WriterModel)
	v.SetDefault("critic_model", cfg.CriticModel)
	v.SetDefault("tts_model", cfg.TTSModel)
	v.SetDefault("image_model", cfg.ImageModel)
	v.SetDefault("writer_provider", cfg.WriterProvider)
	v.SetDefault("critic_provider", cfg.CriticProvider)
	v.SetDefault("dashscope_region", cfg.DashScopeRegion)
	v.SetDefault("data_dir", cfg.DataDir)
	v.SetDefault("output_dir", cfg.OutputDir)
	v.SetDefault("db_path", cfg.DBPath)
	v.SetDefault("cost_cap_research", cfg.CostCapResearch)
	v.SetDefault("cost_cap_write", cfg.CostCapWrite)
	v.SetDefault("cost_cap_image", cfg.CostCapImage)
	v.SetDefault("cost_cap_tts", cfg.CostCapTTS)
	v.SetDefault("cost_cap_assemble", cfg.CostCapAssemble)
	v.SetDefault("cost_cap_per_run", cfg.CostCapPerRun)
	v.SetDefault("anti_progress_threshold", cfg.AntiProgressThreshold)

	// Read config.yaml if it exists.
	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			if !os.IsNotExist(err) {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					return cfg, fmt.Errorf("read config: %w", err)
				}
			}
		}
	}

	// Environment variables override config file values.
	v.AutomaticEnv()

	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal config: %w", err)
	}

	return cfg, nil
}

// BindFlags binds CLI persistent flags to Viper keys so that flag values
// take highest priority in the config hierarchy.
func BindFlags(v *viper.Viper, flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		v.BindPFlag(f.Name, f)
	})
}

// DefaultConfigDir returns the default configuration directory path.
func DefaultConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".youtube-pipeline")
}

// DefaultConfigPath returns the default config.yaml path.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.yaml")
}

// DefaultEnvPath returns the default .env path.
func DefaultEnvPath() string {
	return filepath.Join(DefaultConfigDir(), ".env")
}
