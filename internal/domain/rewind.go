package domain

// RewindResetParams describes the database-side effects of a stepper rewind.
// Defined in domain so both the pipeline (orchestrator) and db (executor)
// layers can reference one shape without either package importing the
// other.
//
// The struct is computed by pipeline.PlanRewind from a StageNodeKey; the
// db RunStore consumes it as the input to ApplyRewindReset.
type RewindResetParams struct {
	// FinalStage is what runs.stage is set to after cleanup completes.
	// May differ from the rewind target — e.g. rewind-to-scenario lands
	// at pending so the operator clicks "Start run" rather than
	// auto-restarting Phase A.
	FinalStage Stage
	// FinalStatus is what runs.status is set to.
	FinalStatus Status

	// Segment-row effects.
	DeleteSegments      bool
	ClearImageArtifacts bool
	ClearTTSArtifacts   bool
	ClearClipPaths      bool

	// runs-row column resets. ClearCharacterPick clears
	// selected_character_id, frozen_descriptor, and character_query_key
	// together (one logical "operator pick").
	ClearScenarioPath  bool
	ClearCharacterPick bool
	ClearOutputPath    bool
	ClearCriticScore   bool

	// DecisionTypesToDelete enumerates the decision_type strings whose
	// rows should be removed for this run. Empty/nil → no deletion.
	DecisionTypesToDelete []string
}
