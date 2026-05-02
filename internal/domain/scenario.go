package domain

// ResearcherOutput is the schema-validated summary produced by the
// deterministic V1 Researcher agent.
type ResearcherOutput struct {
	SCPID                 string         `json:"scp_id"`
	Title                 string         `json:"title"`
	ObjectClass           string         `json:"object_class"`
	PhysicalDescription   string         `json:"physical_description"`
	AnomalousProperties   []string       `json:"anomalous_properties"`
	ContainmentProcedures string         `json:"containment_procedures"`
	BehaviorAndNature     string         `json:"behavior_and_nature"`
	OriginAndDiscovery    string         `json:"origin_and_discovery"`
	VisualIdentity        VisualIdentity `json:"visual_identity"`
	DramaticBeats         []DramaticBeat `json:"dramatic_beats"`
	MainTextExcerpt       string         `json:"main_text_excerpt"`
	Tags                  []string       `json:"tags"`
	SourceVersion         string         `json:"source_version"`
}

// VisualIdentity is the deterministic visual descriptor propagated through
// Phase A.
type VisualIdentity struct {
	Appearance             string   `json:"appearance"`
	DistinguishingFeatures []string `json:"distinguishing_features"`
	EnvironmentSetting     string   `json:"environment_setting"`
	KeyVisualMoments       []string `json:"key_visual_moments"`
}

// DramaticBeat is one deterministic dramatic beat extracted from the corpus.
type DramaticBeat struct {
	Index         int    `json:"index"`
	Source        string `json:"source"`
	Description   string `json:"description"`
	EmotionalTone string `json:"emotional_tone"`
}

// StructurerOutput is the schema-validated 4-act structure produced by the
// deterministic V1 Structurer agent.
type StructurerOutput struct {
	SCPID            string `json:"scp_id"`
	Acts             []Act  `json:"acts"`
	TargetSceneCount int    `json:"target_scene_count"`
	SourceVersion    string `json:"source_version"`
}

// Act is one act in the deterministic 4-act scenario structure.
type Act struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Synopsis        string   `json:"synopsis"`
	SceneBudget     int      `json:"scene_budget"`
	DurationRatio   float64  `json:"duration_ratio"`
	DramaticBeatIDs []int    `json:"dramatic_beat_ids"`
	KeyPoints       []string `json:"key_points"`
}

const (
	ActIncident   = "incident"
	ActMystery    = "mystery"
	ActRevelation = "revelation"
	ActUnresolved = "unresolved"

	// SourceVersionV1 identifies the deterministic-V1 contract for the
	// researcher and structurer. Bump this when EITHER agent's output
	// shape or values change in a way that makes prior cached output
	// no longer compatible. The cache loader at phase_a.go invalidates
	// entries whose source_version differs from this constant, so a
	// bump is the mechanism for force-rerunning deterministic stages
	// after a logic change.
	//
	// History:
	//   v1-deterministic   — initial deterministic researcher + structurer
	//   v1.1-deterministic — structurer fans each beat out to scenesPerBeat
	//                        scenes; old caches had 1 scene per beat
	SourceVersionV1 = "v1.1-deterministic"
)

var ActOrder = [4]string{
	ActIncident,
	ActMystery,
	ActRevelation,
	ActUnresolved,
}

var ActDurationRatio = map[string]float64{
	ActIncident:   0.15,
	ActMystery:    0.30,
	ActRevelation: 0.40,
	ActUnresolved: 0.15,
}
