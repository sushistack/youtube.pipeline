package domain

// MetadataBundle is the YT-ready AI-content disclosure written to metadata.json.
type MetadataBundle struct {
	Version     int                    `json:"version"`
	GeneratedAt string                 `json:"generated_at"` // RFC3339
	RunID       string                 `json:"run_id"`
	SCPID       string                 `json:"scp_id"`
	Title       string                 `json:"title"`
	AIGenerated AIGeneratedFlags       `json:"ai_generated"`
	ModelsUsed  map[string]ModelRecord `json:"models_used"`
}

// AIGeneratedFlags declares which content components were AI-generated.
type AIGeneratedFlags struct {
	Narration bool `json:"narration"`
	Imagery   bool `json:"imagery"`
	TTS       bool `json:"tts"`
}

// ModelRecord records a single provider+model pair used during generation.
type ModelRecord struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Voice    string `json:"voice,omitempty"` // TTS only
}

// SourceManifest is the license-audit record written to manifest.json.
type SourceManifest struct {
	Version     int            `json:"version"`
	GeneratedAt string         `json:"generated_at"` // RFC3339
	RunID       string         `json:"run_id"`
	SCPID       string         `json:"scp_id"`
	SourceURL   string         `json:"source_url"`
	AuthorName  string         `json:"author_name"`
	License     string         `json:"license"`     // "CC BY-SA 3.0"
	LicenseURL  string         `json:"license_url"` // canonical CC URL
	LicenseChain []LicenseEntry `json:"license_chain"`
}

// LicenseEntry is one node in the license attribution chain.
type LicenseEntry struct {
	Component  string `json:"component"`  // e.g. "SCP article text"
	SourceURL  string `json:"source_url"`
	AuthorName string `json:"author_name"`
	License    string `json:"license"`
}

const (
	// LicenseCCBYSA30 is the SCP Wiki license identifier.
	LicenseCCBYSA30 = "CC BY-SA 3.0"
	// LicenseURLCCBYSA30 is the canonical CC BY-SA 3.0 URL.
	LicenseURLCCBYSA30 = "https://creativecommons.org/licenses/by-sa/3.0/"
)
