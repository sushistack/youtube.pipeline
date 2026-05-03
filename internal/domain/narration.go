package domain

const (
	NarrationSourceVersionV1 = "v1-llm-writer"
	LanguageKorean           = "ko"
)

type NarrationScript struct {
	SCPID         string            `json:"scp_id"`
	Title         string            `json:"title"`
	Scenes        []NarrationScene  `json:"scenes"`
	Metadata      NarrationMetadata `json:"metadata"`
	SourceVersion string            `json:"source_version"`
}

type NarrationScene struct {
	SceneNum int    `json:"scene_num"`
	ActID    string `json:"act_id"`
	// Narration is the full Korean narration text for this scene. It must
	// stay rune-capped per ActNarrationRuneCap (one-visual-beat rule).
	Narration string `json:"narration"`
	// NarrationBeats is the writer's per-scene split into discrete visual
	// beats. Each beat seeds exactly one downstream visual_breakdowner shot
	// (1:1 mapping). Min length 1: even single-image incident hooks carry
	// one beat. Order is rendering order. See Stage 3.5
	// (docs/prompts/scenario/03_5_visual_breakdown.md) for the consumer
	// contract.
	NarrationBeats    []string  `json:"narration_beats"`
	FactTags          []FactTag `json:"fact_tags"`
	Mood              string    `json:"mood"`
	EntityVisible     bool      `json:"entity_visible"`
	Location          string    `json:"location"`
	CharactersPresent []string  `json:"characters_present"`
	ColorPalette      string    `json:"color_palette"`
	Atmosphere        string    `json:"atmosphere"`
}

type FactTag struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}

type NarrationMetadata struct {
	Language              string `json:"language"`
	SceneCount            int    `json:"scene_count"`
	WriterModel           string `json:"writer_model"`
	WriterProvider        string `json:"writer_provider"`
	PromptTemplate        string `json:"prompt_template"`
	FormatGuideTemplate   string `json:"format_guide_template"`
	ForbiddenTermsVersion string `json:"forbidden_terms_version"`
}
