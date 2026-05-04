// Package contractv2 defines the SCP-Explained-aligned schemas the spec
// at _bmad-output/planning-artifacts/next-session-enhance-prompts.md
// section 4 calls for.
//
// All types are net-new and additive. v1 (`internal/domain/...`) keeps
// working unchanged.
//
// Selection between v1 and v2 is the caller's job: compare a config
// value against the Version constant below
// (e.g. `cfg.ContractVersion == contractv2.Version`). The toggle is
// expected to live in domain.PipelineConfig (config.yaml) when wired
// in — env-only flags are avoided per
// memory/feedback_config_not_env.md.
//
// Adapter helpers in adapter.go convert between v1 and v2 best-effort.
// Fields that v1 does not carry (KoreanTerms, RelatedSCPs, NarrativeArc,
// HookAngle, TwistPoint, etc.) are zero-valued when adapting v1→v2.
package contractv2

// Version is the canonical v2 schema identifier callers compare against.
const Version = "v2"

// ResearchOutput is the spec section 4.1 schema. Compared to
// domain.ResearcherOutput the new shape adds KoreanTerms,
// RelatedSCPs, LoreConnections, KeyIncidents, and a structured
// Branch/Author/WikiURL trio that the v1 ResearcherOutput represents
// only as free-text fields.
type ResearchOutput struct {
	SCPNumber           string            `json:"scp_number"`
	ObjectClass         string            `json:"object_class"`
	Author              string            `json:"author"`
	WikiURL             string            `json:"wiki_url"`
	Branch              string            `json:"branch"`
	OriginalSummary     string            `json:"original_summary"`
	AnomalousProperties []string          `json:"anomalous_properties"`
	KeyIncidents        []Incident        `json:"key_incidents"`
	RelatedSCPs         []string          `json:"related_scps"`
	LoreConnections     []string          `json:"lore_connections"`
	KoreanTerms         map[string]string `json:"korean_terms"`
}

// Incident is one dramatized event the writer can lean into.
type Incident struct {
	IncidentID  string `json:"incident_id"`
	Date        string `json:"date,omitempty"`
	Location    string `json:"location,omitempty"`
	Summary     string `json:"summary"`
	Outcome     string `json:"outcome,omitempty"`
	Casualties  int    `json:"casualties,omitempty"`
	SourceURL   string `json:"source_url,omitempty"`
}

// StructureOutput is spec section 4.2.
type StructureOutput struct {
	NarrativeArc   string `json:"narrative_arc"`
	HookAngle      string `json:"hook_angle"`
	TwistPoint     string `json:"twist_point"`
	EndingHook     string `json:"ending_hook"`
	Tone           string `json:"tone"`
	TargetDuration int    `json:"target_duration_seconds"`
	SceneCount     int    `json:"scene_count"`
}

// ScriptOutput is spec section 4.3. The shape intentionally diverges
// from domain.NarrationScript: TitleCandidates plural, OutroHookKO is a
// distinct field, SourceAttribution is a structured value rather than
// metadata.
type ScriptOutput struct {
	TitleCandidates   []string    `json:"title_candidates"`
	Scenes            []Scene     `json:"scenes"`
	OutroHookKO       string      `json:"outro_hook_ko"`
	SourceAttribution Attribution `json:"source_attribution"`
}

// Scene mirrors spec section 4.3 — leaner than domain.BeatAnchor (no
// FactTags, ColorPalette, EntityVisible) and adds EmotionCurve / SFXHint.
// One contract Scene per source beat.
type Scene struct {
	SceneID         int    `json:"scene_id"`
	Section         string `json:"section"`
	DurationSeconds int    `json:"duration_seconds"`
	NarrationKO     string `json:"narration_ko"`
	VisualDirection string `json:"visual_direction"`
	EmotionCurve    string `json:"emotion_curve"`
	SFXHint         string `json:"sfx_hint"`
}

// Attribution carries the CC BY-SA 3.0 credit fields the style guide
// ko template renders. The spec wants attribution as a first-class
// field of ScriptOutput rather than buried in metadata.
type Attribution struct {
	SCPNumber  string `json:"scp_number"`
	Author     string `json:"author"`
	WikiURL    string `json:"wiki_url"`
	License    string `json:"license"`
	RenderedKO string `json:"rendered_ko"`
}

// ShotPlanOutput is spec section 4.4.
type ShotPlanOutput struct {
	Shots         []Shot           `json:"shots"`
	AssetReuseMap map[string][]int `json:"asset_reuse_map"`
}

// Shot is one camera setup, possibly reused across scenes.
type Shot struct {
	SceneID         int      `json:"scene_id"`
	ShotID          int      `json:"shot_id"`
	Background      string   `json:"background"`
	Foreground      []string `json:"foreground"`
	CameraMove      string   `json:"camera_move"`
	DurationSeconds float64  `json:"duration_seconds"`
	Lighting        string   `json:"lighting"`
	Notes           string   `json:"notes"`
}

// CriticReport is spec section 4.5.
type CriticReport struct {
	OverallScore     int            `json:"overall_score"`
	Passed           bool           `json:"passed"`
	RubricScores     map[string]int `json:"rubric_scores"`
	Failures         []Failure      `json:"failures"`
	RevisionPriority string         `json:"revision_priority"`
}

// Failure annotates a single rubric criterion that scored below the
// per-criterion bar (spec section 5: any score < 8 must include a
// specific failure quote).
type Failure struct {
	Criterion         string `json:"criterion"`
	Score             int    `json:"score"`
	FailureQuote      string `json:"failure_quote,omitempty"`
	Recommendation    string `json:"recommendation,omitempty"`
	RequiresLLMReview bool   `json:"requires_llm_review,omitempty"`
}

// PassingScore is the rubric pass threshold from spec section 5.
const PassingScore = 80

// MaxScorePerCriterion is the per-criterion ceiling.
const MaxScorePerCriterion = 10

// CriterionKeys is the canonical ordered list of rubric criterion
// identifiers. Stable across releases; renames break consumers, so
// new criteria must be appended.
var CriterionKeys = []string{
	"hook_under_15s",
	"information_drip",
	"concrete_incident",
	"twist_position",
	"unresolved_outro",
	"sentence_rhythm",
	"sensory_language",
	"pov_consistency",
	"scp_fidelity",
	"visual_reusability",
}
