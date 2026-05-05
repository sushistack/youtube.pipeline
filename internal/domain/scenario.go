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
// Sum: 720+1600+2080+1400 = 5800 runes — fits the ≥4500 Lever P parity floor
// with headroom; modestly above hada's ~5500 ceiling (+5.5%) after both
// dogfood-driven widenings below. Per-act ratios mirror v1 EXCEPT for
// incident and unresolved, both raised after the dogfood-observed overshoot
// pattern (LLM cannot count Korean runes precisely; the writer chronically
// retried into rejection). Mystery and revelation already sit at +73–129%
// over their hada-act baselines and have not exhibited the pattern.
//
//	incident   =  720 (raised from 480; hada Act 1 ~330, +118% headroom)
//	mystery    = 1600 (~4× v1's 400 — abilities/protocol setup)
//	revelation = 2080 (~4× v1's 520 — climax with sensory + numeric anchors)
//	unresolved = 1400 (raised from 1120; SCP-049 dogfood 2026-05-05 emitted
//	             1286 runes — content-rich closers naturally exceed the
//	             hada-baseline-derived cap. +9% safety buffer over the
//	             observed overshoot keeps retries productive without an
//	             unbounded raise.)
//
// Stage-2 beat segmentation (8–10 beats/act) operates within these monologue
// boundaries; it neither rescales nor enforces this cap.
var ActMonologueRuneCap = map[string]int{
	ActIncident:   720,
	ActMystery:    1600,
	ActRevelation: 2080,
	ActUnresolved: 1400,
}

// ActMonologueRuneFloor is the per-act inclusive lower bound on the act's
// continuous monologue length, in runes. Without a floor the writer LLM
// chronically under-utilizes the cap (observed dogfood: 37% of cap on
// average, revelation as low as 21%), producing monologues 38% shorter than
// hada-golden density. Floors are set near the per-act hada baseline so the
// writer cannot drift back to under-utilization, while leaving pacing
// latitude. For incident the floor stays at 288 (just below hada Act 1's
// ~330 runes) even though the cap was widened to 720 — the wider band gives
// retry headroom without forcing length above golden density. Validation
// rejects below floor and burns the writer retry budget if the LLM does not
// expand.
//
// Revelation lowered from the mechanical 60%-of-cap (1248) to 900 after
// SCP-049 dogfood (2026-05-05) showed chronic under-flow: the LLM emitted
// 932–936 runes consistently across retries, exhausting the budget. The
// 60%-of-cap value was a shortcut that diverged from the stated "near hada
// baseline" philosophy; 900 still sits well above the historical floor-less
// low of ~437 runes (21% of cap), so under-utilization regression risk is
// bounded.
var ActMonologueRuneFloor = map[string]int{
	ActIncident:   288, // hada-realistic; cap=720 gives wide band for retries
	ActMystery:    960, // 1600 × 0.6
	ActRevelation: 900, // lowered from 1248; SCP-049 dogfood under-flow
	ActUnresolved: 672, // unchanged after the cap raise to 1400 — same
	// philosophy as the incident widening: the wider band gives retry
	// headroom without forcing length above golden density. Floor still
	// well above hada Act 4's ~280-rune baseline.
}
