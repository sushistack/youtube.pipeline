package domain

// Stage represents a pipeline processing stage.
type Stage string

const (
	StagePending        Stage = "pending"
	StageResearch       Stage = "research"
	StageStructure      Stage = "structure"
	StageWrite          Stage = "write"
	StageVisualBreak    Stage = "visual_break"
	StageReview         Stage = "review"
	StageCritic         Stage = "critic"
	StageScenarioReview Stage = "scenario_review"
	StageCharacterPick  Stage = "character_pick"
	StageImage          Stage = "image"
	StageTTS            Stage = "tts"
	StageBatchReview    Stage = "batch_review"
	StageAssemble       Stage = "assemble"
	StageMetadataAck    Stage = "metadata_ack"
	StageComplete       Stage = "complete"
)

// allStages is the backing array for AllStages().
var allStages = [...]Stage{
	StagePending, StageResearch, StageStructure, StageWrite,
	StageVisualBreak, StageReview, StageCritic, StageScenarioReview,
	StageCharacterPick, StageImage, StageTTS, StageBatchReview,
	StageAssemble, StageMetadataAck, StageComplete,
}

// AllStages returns a copy of all Stage constants in pipeline order.
func AllStages() []Stage {
	s := allStages
	return s[:]
}

// IsValid returns true if s is one of the 15 defined stage constants.
func (s Stage) IsValid() bool {
	for _, v := range allStages {
		if s == v {
			return true
		}
	}
	return false
}

// Event represents a state machine trigger that causes stage transitions.
type Event string

const (
	EventStart    Event = "start"
	EventComplete Event = "complete"
	EventApprove  Event = "approve"
	EventRetry    Event = "retry"
)

var allEvents = [...]Event{
	EventStart, EventComplete, EventApprove, EventRetry,
}

// AllEvents returns a copy of all Event constants.
func AllEvents() []Event {
	s := allEvents
	return s[:]
}

// IsValid returns true if e is one of the defined event constants.
func (e Event) IsValid() bool {
	for _, v := range allEvents {
		if e == v {
			return true
		}
	}
	return false
}

// Status represents the operational status of a pipeline run.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusWaiting   Status = "waiting"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

var allStatuses = [...]Status{
	StatusPending, StatusRunning, StatusWaiting,
	StatusCompleted, StatusFailed, StatusCancelled,
}

// AllStatuses returns a copy of all Status constants.
func AllStatuses() []Status {
	s := allStatuses
	return s[:]
}

// RetryReasonServerRestarted is the retry_reason marker the orphan
// reconciler stamps on runs whose status='running' did not survive a
// process restart. The marker is a system-event signal, not an operator
// action — Resume treats it as authorization to bypass the FS/DB
// consistency gate so mid-flight crashes can be recovered without UI
// gymnastics.
const RetryReasonServerRestarted = "server restarted while run was in progress"

// IsValid returns true if s is one of the defined status constants.
func (s Status) IsValid() bool {
	for _, v := range allStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// Run maps to the runs database table.
type Run struct {
	ID                  string   `json:"id"`
	SCPID               string   `json:"scp_id"`
	Stage               Stage    `json:"stage"`
	Status              Status   `json:"status"`
	RetryCount          int      `json:"retry_count"`
	RetryReason         *string  `json:"retry_reason,omitempty"`
	CriticScore         *float64 `json:"critic_score,omitempty"`
	CostUSD             float64  `json:"cost_usd"`
	TokenIn             int      `json:"token_in"`
	TokenOut            int      `json:"token_out"`
	DurationMs          int64    `json:"duration_ms"`
	HumanOverride       bool     `json:"human_override"`
	ScenarioPath        *string  `json:"scenario_path,omitempty"`
	CharacterQueryKey   *string  `json:"character_query_key,omitempty"`
	SelectedCharacterID *string  `json:"selected_character_id,omitempty"`
	FrozenDescriptor    *string  `json:"frozen_descriptor,omitempty"`
	CriticPromptVersion *string  `json:"critic_prompt_version,omitempty"`
	CriticPromptHash    *string  `json:"critic_prompt_hash,omitempty"`
	DryRun              bool     `json:"dry_run"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
}

// PhaseAAdvanceResult is the atomic persistence surface used when the engine
// completes or retries the Phase A chain.
type PhaseAAdvanceResult struct {
	Stage        Stage    `json:"stage"`
	Status       Status   `json:"status"`
	RetryReason  *string  `json:"retry_reason,omitempty"`
	CriticScore  *float64 `json:"critic_score,omitempty"`
	ScenarioPath *string  `json:"scenario_path,omitempty"`
}

// CharacterCandidate is the normalized operator-facing image candidate.
type CharacterCandidate struct {
	ID          string  `json:"id"`
	PageURL     string  `json:"page_url"`
	ImageURL    string  `json:"image_url"`
	PreviewURL  *string `json:"preview_url,omitempty"`
	Title       *string `json:"title,omitempty"`
	SourceLabel *string `json:"source_label,omitempty"`
}

// CharacterGroup is the stable API/domain schema for character search results.
type CharacterGroup struct {
	Query      string               `json:"query"`
	QueryKey   string               `json:"query_key"`
	Candidates []CharacterCandidate `json:"candidates"`
}

// ScpImageRecord is the stable schema for the scp_image_library table.
// FilePath is stored relative to PipelineConfig.ScpImageDir so the row
// remains valid across machines/checkouts as long as the directory is
// regenerated. Version increments on each regenerate (starts at 1).
//
// SourceCandidateID is the DDG candidate ID (form `{queryKey}#{n}`) the
// operator selected to seed canonical generation. It is preserved so a
// later "reuse" flow on a fresh run can re-issue /characters/pick with a
// candidate ID that survives in the shared character_search_cache row,
// without forcing the operator to redo the search.
type ScpImageRecord struct {
	ScpID             string `json:"scp_id"`
	FilePath          string `json:"file_path"`
	SourceRefURL      string `json:"source_ref_url"`
	SourceQueryKey    string `json:"source_query_key"`
	SourceCandidateID string `json:"source_candidate_id"`
	// FrozenDescriptor is the raw operator-edited descriptor used to
	// generate this canonical (without the cartoon-style prefix). Stored
	// separately from PromptUsed so the FE reuse flow does not need to
	// parse the prompt — that string is operator-tunable and any "; "
	// inside the style prefix would corrupt naive client-side splits.
	FrozenDescriptor string `json:"frozen_descriptor"`
	PromptUsed       string `json:"prompt_used"`
	Seed             int64  `json:"seed"`
	Version          int    `json:"version"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// Episode represents a scene/segment. Maps to the segments database table.
type Episode struct {
	ID             int64        `json:"id"`
	RunID          string       `json:"run_id"`
	SceneIndex     int          `json:"scene_index"`
	Narration      *string      `json:"narration,omitempty"`
	ShotCount      int          `json:"shot_count"`
	Shots          []Shot       `json:"shots"`
	TTSPath        *string      `json:"tts_path,omitempty"`
	TTSDurationMs  *int         `json:"tts_duration_ms,omitempty"`
	ClipPath       *string      `json:"clip_path,omitempty"`
	CriticScore    *float64     `json:"critic_score,omitempty"`
	CriticSub      *string      `json:"critic_sub,omitempty"`
	Status         string       `json:"status"`
	ReviewStatus   ReviewStatus `json:"review_status"`
	SafeguardFlags []string     `json:"safeguard_flags,omitempty"`
	CreatedAt      string       `json:"created_at"`
}

// Shot represents a single visual shot within an Episode.
type Shot struct {
	ImagePath        string  `json:"image_path"`
	DurationSeconds  float64 `json:"duration_s"`
	Transition       string  `json:"transition"`
	VisualDescriptor string  `json:"visual_descriptor"`
}

// Decision maps to the decisions database table.
type Decision struct {
	ID              int64   `json:"id"`
	RunID           string  `json:"run_id"`
	SceneID         *string `json:"scene_id,omitempty"`
	DecisionType    string  `json:"decision_type"`
	ContextSnapshot *string `json:"context_snapshot,omitempty"`
	OutcomeLink     *string `json:"outcome_link,omitempty"`
	Tags            *string `json:"tags,omitempty"`
	FeedbackSource  *string `json:"feedback_source,omitempty"`
	ExternalRef     *string `json:"external_ref,omitempty"`
	FeedbackAt      *string `json:"feedback_at,omitempty"`
	SupersededBy    *int64  `json:"superseded_by,omitempty"`
	Note            *string `json:"note,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

// NormalizedResponse is the common response envelope for all LLM providers.
type NormalizedResponse struct {
	Content      string  `json:"content"`
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	CostUSD      float64 `json:"cost_usd"`
	DurationMs   int64   `json:"duration_ms"`
	FinishReason string  `json:"finish_reason,omitempty"`
}
