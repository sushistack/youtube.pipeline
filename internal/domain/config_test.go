package domain

import (
	"strings"
	"testing"
)

func TestDefaultConfig_WriterNotEqualCritic(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.WriterProvider == cfg.CriticProvider {
		t.Errorf("DefaultConfig must have different writer and critic providers, got both %q", cfg.WriterProvider)
	}
}

func TestDefaultConfig_AllFieldsPopulated(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.WriterModel == "" {
		t.Error("WriterModel is empty")
	}
	if cfg.CriticModel == "" {
		t.Error("CriticModel is empty")
	}
	if cfg.TTSModel == "" {
		t.Error("TTSModel is empty")
	}
	if cfg.ImageModel == "" {
		t.Error("ImageModel is empty")
	}
	if cfg.WriterProvider == "" {
		t.Error("WriterProvider is empty")
	}
	if cfg.CriticProvider == "" {
		t.Error("CriticProvider is empty")
	}
	if cfg.ImageProvider == "" {
		t.Error("ImageProvider is empty")
	}
	if cfg.TTSProvider == "" {
		t.Error("TTSProvider is empty")
	}
	if cfg.DashScopeRegion == "" {
		t.Error("DashScopeRegion is empty")
	}
	if cfg.DataDir == "" {
		t.Error("DataDir is empty")
	}
	if cfg.OutputDir == "" {
		t.Error("OutputDir is empty")
	}
	if cfg.DBPath == "" {
		t.Error("DBPath is empty")
	}
	if cfg.CostCapResearch <= 0 {
		t.Error("CostCapResearch must be positive")
	}
	if cfg.CostCapWrite <= 0 {
		t.Error("CostCapWrite must be positive")
	}
	if cfg.CostCapImage <= 0 {
		t.Error("CostCapImage must be positive")
	}
	if cfg.CostCapTTS <= 0 {
		t.Error("CostCapTTS must be positive")
	}
	if cfg.CostCapAssemble <= 0 {
		t.Error("CostCapAssemble must be positive")
	}
	if cfg.CostCapPerRun <= 0 {
		t.Error("CostCapPerRun must be positive")
	}
	// NFR-C2 backstop must exceed the sum of per-stage caps so per-stage caps
	// are the primary guardrail and per-run is the safety net.
	stageSum := cfg.CostCapResearch + cfg.CostCapWrite + cfg.CostCapImage + cfg.CostCapTTS + cfg.CostCapAssemble
	if cfg.CostCapPerRun <= stageSum {
		t.Errorf("CostCapPerRun (%.2f) must exceed sum of per-stage caps (%.2f)", cfg.CostCapPerRun, stageSum)
	}
}

func TestDefaultConfig_AntiProgressThreshold(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.AntiProgressThreshold != 0.92 {
		t.Errorf("AntiProgressThreshold = %v, want 0.92 (FR8 + NFR-R2)", cfg.AntiProgressThreshold)
	}
}

func TestDefaultConfig_ShadowEvalWindow(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ShadowEvalWindow != 10 {
		t.Errorf("ShadowEvalWindow = %d, want 10 (FR28 default)", cfg.ShadowEvalWindow)
	}
}

func TestDefaultConfig_AutoApprovalThreshold(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.AutoApprovalThreshold != 0.85 {
		t.Errorf("AutoApprovalThreshold = %v, want 0.85", cfg.AutoApprovalThreshold)
	}
}

func TestDefaultConfig_PathsUnderHome(t *testing.T) {
	cfg := DefaultConfig()
	if !strings.Contains(cfg.OutputDir, ".youtube-pipeline") {
		t.Errorf("OutputDir should be under .youtube-pipeline, got %s", cfg.OutputDir)
	}
	if !strings.Contains(cfg.DBPath, ".youtube-pipeline") {
		t.Errorf("DBPath should be under .youtube-pipeline, got %s", cfg.DBPath)
	}
}
