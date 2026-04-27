package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// ErrCorruptedConfig signals that config.yaml could not be parsed. Handlers
// translate this to a recovery-capable error state so the operator can reset
// the file without hand-editing YAML.
var ErrCorruptedConfig = errors.New("config file is corrupted")

type SettingsFileManager struct {
	ConfigPath string
	EnvPath    string
}

func NewSettingsFileManager(configPath, envPath string) *SettingsFileManager {
	return &SettingsFileManager{ConfigPath: configPath, EnvPath: envPath}
}

func (m *SettingsFileManager) Load() (domain.SettingsFileSnapshot, error) {
	cfg, err := loadConfigFile(m.ConfigPath)
	if err != nil {
		return domain.SettingsFileSnapshot{}, err
	}
	env, err := loadEnvFile(m.EnvPath)
	if err != nil {
		return domain.SettingsFileSnapshot{}, err
	}
	return domain.SettingsFileSnapshot{
		Config: cfg,
		Env:    env,
	}, nil
}

// Write persists config.yaml and .env transactionally: if either write fails,
// the caller observes no partial state. The approach snapshots the current
// on-disk contents, performs both atomic writes in sequence, and restores the
// snapshots if the second write fails. A restore failure is surfaced so the
// operator can intervene rather than leaving a silent mismatch.
func (m *SettingsFileManager) Write(files domain.SettingsFileSnapshot) error {
	// Capture the existing contents so we can roll back on partial failure.
	configBackup, configMode, configExisted, err := readFileBackup(m.ConfigPath)
	if err != nil {
		return fmt.Errorf("write settings: snapshot config: %w", err)
	}
	envBackup, envMode, envExisted, err := readFileBackup(m.EnvPath)
	if err != nil {
		return fmt.Errorf("write settings: snapshot env: %w", err)
	}

	if err := writeConfigFile(m.ConfigPath, files.Config, configMode, configExisted); err != nil {
		return err
	}
	if err := writeEnvFile(m.EnvPath, files.Env); err != nil {
		// Best-effort rollback of config.yaml so the two files stay consistent.
		if rollbackErr := restoreFile(m.ConfigPath, configBackup, configMode, configExisted); rollbackErr != nil {
			return fmt.Errorf(
				"write settings: env write failed and config rollback also failed "+
					"(env=%w, config_rollback=%v)", err, rollbackErr,
			)
		}
		_ = envBackup // kept for symmetry if future code needs env rollback too
		_ = envMode
		_ = envExisted
		return err
	}
	return nil
}

func loadConfigFile(path string) (domain.PipelineConfig, error) {
	cfg := domain.DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config file: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%w: %v", ErrCorruptedConfig, err)
	}
	return cfg, nil
}

func writeConfigFile(path string, cfg domain.PipelineConfig, existingMode os.FileMode, existed bool) error {
	ordered := orderedPipelineConfig{
		WriterModel:           cfg.WriterModel,
		CriticModel:           cfg.CriticModel,
		TTSModel:              cfg.TTSModel,
		TTSVoice:              cfg.TTSVoice,
		TTSAudioFormat:        cfg.TTSAudioFormat,
		ImageModel:            cfg.ImageModel,
		ImageEditModel:        cfg.ImageEditModel,
		WriterProvider:        cfg.WriterProvider,
		CriticProvider:        cfg.CriticProvider,
		ImageProvider:         cfg.ImageProvider,
		TTSProvider:           cfg.TTSProvider,
		DashScopeRegion:       cfg.DashScopeRegion,
		DataDir:               cfg.DataDir,
		OutputDir:             cfg.OutputDir,
		DBPath:                cfg.DBPath,
		CostCapResearch:       cfg.CostCapResearch,
		CostCapWrite:          cfg.CostCapWrite,
		CostCapImage:          cfg.CostCapImage,
		CostCapTTS:            cfg.CostCapTTS,
		CostCapAssemble:       cfg.CostCapAssemble,
		CostCapPerRun:         cfg.CostCapPerRun,
		AntiProgressThreshold: cfg.AntiProgressThreshold,
		GoldenStalenessDays:   cfg.GoldenStalenessDays,
		ShadowEvalWindow:      cfg.ShadowEvalWindow,
		AutoApprovalThreshold: cfg.AutoApprovalThreshold,
		BlockedVoiceIDs:       cfg.BlockedVoiceIDs,
		ArtifactRetentionDays: cfg.ArtifactRetentionDays,
		DryRun:                cfg.DryRun,
	}
	data, err := yaml.Marshal(&ordered)
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	mode := os.FileMode(0644)
	if existed {
		mode = existingMode
	}
	return writeAtomic(path, data, mode)
}

func loadEnvFile(path string) (map[string]string, error) {
	values := map[string]string{}
	if path == "" {
		return values, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, fmt.Errorf("read env file: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		key, value, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if isSupportedSettingsSecret(key) {
			values[key] = value
		}
	}
	return values, nil
}

// writeEnvFile rewrites .env preserving unsupported lines verbatim. Supported
// secret lines are replaced with the values passed in; keys missing from
// `values` are treated as "delete" so explicit null-clears propagate to disk.
// The file is always written with 0600 regardless of existing mode — secret
// files must not be widened out-of-band.
func writeEnvFile(path string, values map[string]string) error {
	var existing []string
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			existing = strings.Split(string(data), "\n")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("read env file for write: %w", err)
		}
	}

	if values == nil {
		values = map[string]string{}
	}
	remaining := map[string]string{}
	for key, value := range values {
		if isSupportedSettingsSecret(key) {
			remaining[key] = value
		}
	}

	rewritten := make([]string, 0, len(existing)+len(remaining))
	for _, line := range existing {
		key, _, ok := parseEnvLine(line)
		if !ok || !isSupportedSettingsSecret(key) {
			// Non-supported / comment / blank — keep verbatim.
			rewritten = append(rewritten, line)
			continue
		}
		value, exists := remaining[key]
		if !exists {
			// Operator explicitly cleared this secret (or schema doesn't
			// recognize it any more); drop the line from the rewrite.
			continue
		}
		rewritten = append(rewritten, fmt.Sprintf("%s=%s", key, quoteEnvValue(value)))
		delete(remaining, key)
	}

	keys := make([]string, 0, len(remaining))
	for key := range remaining {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rewritten = append(rewritten, fmt.Sprintf("%s=%s", key, quoteEnvValue(remaining[key])))
	}

	body := strings.Join(rewritten, "\n")
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	// Always 0600: secrets never widen, even if the file previously had a
	// more permissive mode set externally.
	return writeAtomic(path, []byte(body), 0600)
}

// writeAtomic writes data to path via a temp file in the same directory,
// fsyncs the temp file, renames it into place, and then fsyncs the parent
// directory so the rename survives a crash. The temp file's mode is set
// explicitly — not inherited from umask — so secret files do not widen.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if path == "" {
		return fmt.Errorf("write file: empty path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("write file: mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("write file: temp file: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("write file: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write file: write: %w", err)
	}
	// Flush file contents to disk BEFORE the rename so a crash after rename
	// cannot observe a zero-length file.
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("write file: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write file: close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write file: rename: %w", err)
	}
	// fsync the parent dir so the rename entry hits disk.
	if err := fsyncDir(dir); err != nil {
		return fmt.Errorf("write file: fsync dir: %w", err)
	}
	return nil
}

// fsyncDir flushes a directory's metadata so a rename survives a crash. On
// platforms where opening a directory for writing is not supported this is a
// best-effort operation — we log by returning nil because the rename itself
// has already succeeded.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	_ = f.Sync()
	return nil
}

func readFileBackup(path string) ([]byte, os.FileMode, bool, error) {
	if path == "" {
		return nil, 0, false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, false, nil
		}
		return nil, 0, false, fmt.Errorf("stat backup: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("read backup: %w", err)
	}
	return data, info.Mode().Perm(), true, nil
}

func restoreFile(path string, data []byte, mode os.FileMode, existed bool) error {
	if path == "" {
		return nil
	}
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restore: remove new file: %w", err)
		}
		return nil
	}
	return writeAtomic(path, data, mode)
}

// parseEnvLine parses a single .env line into (key, value, ok). Quote handling
// strips exactly one matched pair of leading/trailing quotes — never mixed,
// never asymmetric — so values that legitimately contain quote characters
// round-trip through load → write without silent corruption.
func parseEnvLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	parts := strings.SplitN(trimmed, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false
	}
	value := strings.TrimSpace(parts[1])
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}

func quoteEnvValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t#") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func isSupportedSettingsSecret(key string) bool {
	for _, candidate := range supportedSecretKeys() {
		if key == candidate {
			return true
		}
	}
	return false
}

func supportedSecretKeys() []string {
	return []string{
		domain.SettingsSecretDashScope,
		domain.SettingsSecretDeepSeek,
		domain.SettingsSecretGemini,
	}
}

type orderedPipelineConfig struct {
	WriterModel           string   `yaml:"writer_model"`
	CriticModel           string   `yaml:"critic_model"`
	TTSModel              string   `yaml:"tts_model"`
	TTSVoice              string   `yaml:"tts_voice"`
	TTSAudioFormat        string   `yaml:"tts_audio_format"`
	ImageModel            string   `yaml:"image_model"`
	ImageEditModel        string   `yaml:"image_edit_model"`
	WriterProvider        string   `yaml:"writer_provider"`
	CriticProvider        string   `yaml:"critic_provider"`
	ImageProvider         string   `yaml:"image_provider"`
	TTSProvider           string   `yaml:"tts_provider"`
	DashScopeRegion       string   `yaml:"dashscope_region"`
	DataDir               string   `yaml:"data_dir"`
	OutputDir             string   `yaml:"output_dir"`
	DBPath                string   `yaml:"db_path"`
	CostCapResearch       float64  `yaml:"cost_cap_research"`
	CostCapWrite          float64  `yaml:"cost_cap_write"`
	CostCapImage          float64  `yaml:"cost_cap_image"`
	CostCapTTS            float64  `yaml:"cost_cap_tts"`
	CostCapAssemble       float64  `yaml:"cost_cap_assemble"`
	CostCapPerRun         float64  `yaml:"cost_cap_per_run"`
	AntiProgressThreshold float64  `yaml:"anti_progress_threshold"`
	GoldenStalenessDays   int      `yaml:"golden_staleness_days"`
	ShadowEvalWindow      int      `yaml:"shadow_eval_window"`
	AutoApprovalThreshold float64  `yaml:"auto_approval_threshold"`
	BlockedVoiceIDs       []string `yaml:"blocked_voice_ids,omitempty"`
	ArtifactRetentionDays int      `yaml:"artifact_retention_days"`
	DryRun                bool     `yaml:"dry_run,omitempty"`
}
