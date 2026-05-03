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
// RoleSuggestion is the narrative role assigned by the role classifier
// (hook/tension/reveal/bridge); empty only on transitional fixtures/tests
// — the prod researcher always populates it.
type DramaticBeat struct {
	Index          int    `json:"index"`
	Source         string `json:"source"`
	Description    string `json:"description"`
	EmotionalTone  string `json:"emotional_tone"`
	RoleSuggestion string `json:"role_suggestion,omitempty"`
}

// StructurerOutput is the schema-validated 4-act structure produced by the
// deterministic V1 Structurer agent.
type StructurerOutput struct {
	SCPID            string `json:"scp_id"`
	Acts             []Act  `json:"acts"`
	TargetSceneCount int    `json:"target_scene_count"`
	SourceVersion    string `json:"source_version"`
}

// Act is one act in the deterministic 4-act scenario structure. Role mirrors
// the act-id-to-role mapping from RoleForAct (incident→hook, mystery→tension,
// revelation→reveal, unresolved→bridge); included in the contract so writer
// and downstream stages can read the role without re-deriving it.
type Act struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Synopsis        string   `json:"synopsis"`
	SceneBudget     int      `json:"scene_budget"`
	DurationRatio   float64  `json:"duration_ratio"`
	DramaticBeatIDs []int    `json:"dramatic_beat_ids"`
	KeyPoints       []string `json:"key_points"`
	Role            string   `json:"role,omitempty"`
}

const (
	ActIncident   = "incident"
	ActMystery    = "mystery"
	ActRevelation = "revelation"
	ActUnresolved = "unresolved"

	// RoleHook/RoleTension/RoleReveal/RoleBridge are the four narrative
	// roles classified per dramatic beat. Each role maps 1:1 to an act
	// (hook→incident, tension→mystery, reveal→revelation, bridge→unresolved)
	// via RoleForAct/ActForRole. The classifier prompt mandates every
	// role appears at least once across the beats; the structurer rejects
	// any beat whose role is unset or unrecognized.
	RoleHook    = "hook"
	RoleTension = "tension"
	RoleReveal  = "reveal"
	RoleBridge  = "bridge"

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
	//   v1.1-deterministic — structurer fans each beat out to a constant
	//                        per-beat scene budget; old caches had 1 scene
	//                        per beat
	//   v1.2-roles         — researcher gains a role-classifier LLM call;
	//                        structurer assigns beats to acts by role and
	//                        per-act scene multipliers replace the legacy
	//                        global constant; writer narration cap is per-act
	SourceVersionV1 = "v1.2-roles"
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

// RoleOrder lists narrative roles in the same order as ActOrder so downstream
// code can iterate roles and acts together.
var RoleOrder = [4]string{
	RoleHook,
	RoleTension,
	RoleReveal,
	RoleBridge,
}

// RoleForAct maps each act ID to the narrative role whose beats fill it.
var RoleForAct = map[string]string{
	ActIncident:   RoleHook,
	ActMystery:    RoleTension,
	ActRevelation: RoleReveal,
	ActUnresolved: RoleBridge,
}

// ActForRole is the inverse of RoleForAct: given a beat's role, where it
// belongs in the 4-act structure.
var ActForRole = map[string]string{
	RoleHook:    ActIncident,
	RoleTension: ActMystery,
	RoleReveal:  ActRevelation,
	RoleBridge:  ActUnresolved,
}

// RoleKoreanLabel renders each role as the short Korean phrase the writer
// and structurer prompts use to describe it. Stored here so the synopsis
// prefix emitted by the structurer and any future writer-facing role copy
// pull from one source.
var RoleKoreanLabel = map[string]string{
	RoleHook:    "흥미로운 상황",
	RoleTension: "급박한 상황",
	RoleReveal:  "SCP 설명",
	RoleBridge:  "부연 / 다른 SCP와의 관계",
}

// ActScenesPerBeat is the per-act scene multiplier applied to each beat
// assigned to that act. Replaces the legacy global per-beat scene constant.
//
//	incident   = 1  (cold open is one striking moment, not a montage)
//	mystery    = 2  (information drip needs spread)
//	revelation = 3  (multi-step reveal is the climax — needs room)
//	unresolved = 1  (clean cliffhanger)
var ActScenesPerBeat = map[string]int{
	ActIncident:   1,
	ActMystery:    2,
	ActRevelation: 3,
	ActUnresolved: 1,
}

// ActMonologueRuneCap is the per-act inclusive cap on the act's continuous
// monologue length, in runes. v2 unit is the act monologue (not per-scene),
// so the v1 per-scene caps are rescaled ~4× to align with hada-golden density
// (`docs/exemplars/scp-049-hada.txt`, ~5500 KR chars per ~10-min video).
//
// Sum at the cap floors: 480+1600+2080+1120 = 5280 runes — fits the ≥4500
// Lever P parity floor with headroom, and stays under hada's ~5500 ceiling
// so the writer doesn't bloat past golden density. Per-act ratios mirror v1:
// incident is the tightest (cold-open hook still wants compression), mystery
// and revelation carry the bulk of the explanation arc, unresolved leaves room
// for the closer with the relaxed CTA + 의문문 rule.
//
//	incident   =  480 (~4× v1's 120 — opening discovery section)
//	mystery    = 1600 (~4× v1's 400 — abilities/protocol setup)
//	revelation = 2080 (~4× v1's 520 — climax with sensory + numeric anchors)
//	unresolved = 1120 (~4× v1's 280 — closer + reflective questions + CTA)
//
// Stage-2 beat segmentation (8–10 beats/act) operates within these monologue
// boundaries; it neither rescales nor enforces this cap.
var ActMonologueRuneCap = map[string]int{
	ActIncident:   480,
	ActMystery:    1600,
	ActRevelation: 2080,
	ActUnresolved: 1120,
}
