# Story 3.2: Researcher & Structurer Agents

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want the Researcher and Structurer agents implemented as pure `AgentFunc` closures that turn an SCP ID into a schema-validated 4-act scenario structure from the local `/mnt/data/raw` corpus,
so that Phase A has a deterministic, file-only foundation (no external API) that promotes Story 3.1's opaque `json.RawMessage` PipelineState slots into typed domain outputs without reshaping the data model for Stories 3.3–3.5.

## Prerequisites

**Story 3.1 must be implemented before Story 3.2 can compile.** Story 3.1 owns the `internal/pipeline/agents` package scaffold that this story extends:

- `internal/pipeline/agents/agent.go` — `AgentFunc` type, `PipelineState` struct (with `Research json.RawMessage` + `Structure json.RawMessage` placeholders that THIS story promotes to typed pointers), `PipelineStage` enum, `PipelineStage.DomainStage()` map.
- `internal/pipeline/agents/doc.go` — package documentation including the Agent Purity Rule verbatim.
- `internal/pipeline/agents/noop.go` — `NoopAgent()` helper used in this story's tests to fill the chain-runner slots other agents don't occupy.
- `internal/pipeline/phase_a.go` — `PhaseARunner` + `NewPhaseARunner(...)` constructor.
- `scripts/lintlayers/main.go` — `internal/pipeline/agents` entry in `allowedImports` with the stricter rule `{internal/domain, internal/clock}`.

This story does NOT re-create any of those files. It **extends** them per AC-PROMOTE-PIPELINESTATE-FIELDS (modifies `agent.go`) and **adds** five new files inside `internal/pipeline/agents/` plus four new fixtures in `testdata/`.

If Story 3.1 is not yet merged when dev-story runs for 3.2, the dev agent MUST pause, ensure 3.1 is on the branch (either by merging or cherry-picking), then proceed. Do NOT inline Story 3.1's scaffold into this story — that path creates a merge-conflict disaster.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

**V1 scope boundary (read before implementing):** This story is the **deterministic, file-only** V1 of FR9/FR10/FR11. Agents do **NOT** call an LLM, do **NOT** read the `docs/prompts/scenario/*.md` templates, and do **NOT** touch `domain.TextGenerator`. The architecture's "LLM calls injected via `domain.TextGenerator`" wiring is reserved for Story 3.3 (Writer) and the LLM-driven upgrade path documented in AC-DEFERRED. Do not invent LLM integration here — it will conflict with the scope boundary and waste Story 3.3's work.

1. **AC-DOMAIN-SCENARIO-TYPES:** `internal/domain/scenario.go` (NEW file — keep `types.go` untouched to respect the 300-line cap, per architecture §Structural Rules) declares the Phase-A scenario domain model consumed by every agent downstream. Field names are snake_case JSON; optional fields use pointer types.

    ```go
    // ResearcherOutput is the schema-validated summary produced by the Researcher
    // agent (FR9). Stored in agents.PipelineState.Research and validated against
    // testdata/contracts/researcher_output.schema.json at the agent boundary
    // (FR11). Every field is sourced verbatim or structurally from the local
    // /mnt/data/raw/{scp_id}/ corpus — no LLM-generated prose in V1.
    type ResearcherOutput struct {
        SCPID                 string         `json:"scp_id"`
        Title                 string         `json:"title"`                  // facts.json.title
        ObjectClass           string         `json:"object_class"`           // facts.json.object_class
        PhysicalDescription   string         `json:"physical_description"`   // facts.json.physical_description
        AnomalousProperties   []string       `json:"anomalous_properties"`   // facts.json.anomalous_properties
        ContainmentProcedures string         `json:"containment_procedures"` // facts.json.containment_procedures
        BehaviorAndNature     string         `json:"behavior_and_nature"`    // facts.json.behavior_and_nature
        OriginAndDiscovery    string         `json:"origin_and_discovery"`   // facts.json.origin_and_discovery
        VisualIdentity        VisualIdentity `json:"visual_identity"`        // derived from facts.json.visual_elements
        DramaticBeats         []DramaticBeat `json:"dramatic_beats"`         // derived from key_visual_moments + anomalous_properties
        MainTextExcerpt       string         `json:"main_text_excerpt"`      // truncated main.txt (≤ 4000 runes)
        Tags                  []string       `json:"tags"`                   // meta.json.tags (or facts.json.tags fallback)
        SourceVersion         string         `json:"source_version"`         // "v1-deterministic" — pinned marker so LLM upgrade can gate on it
    }

    // VisualIdentity is the "Frozen Descriptor" input — every Phase B shot
    // will eventually prefix this verbatim (FR16; not this story's concern).
    // V1 populates it deterministically from facts.json.visual_elements.
    type VisualIdentity struct {
        Appearance             string   `json:"appearance"`
        DistinguishingFeatures []string `json:"distinguishing_features"`
        EnvironmentSetting     string   `json:"environment_setting"`
        KeyVisualMoments       []string `json:"key_visual_moments"`
    }

    // DramaticBeat is one pre-scene dramatic beat derived from corpus data.
    // V1 generates 3–10 beats by concatenating key_visual_moments and
    // anomalous_properties; Structurer distributes them across the 4 acts.
    type DramaticBeat struct {
        Index         int    `json:"index"`          // 0-based, stable insertion order
        Source        string `json:"source"`         // "visual_moment" | "anomalous_property"
        Description   string `json:"description"`    // verbatim string from the source field
        EmotionalTone string `json:"emotional_tone"` // "mystery" | "horror" | "tension" | "revelation" (AC-BEAT-TONE)
    }

    // StructurerOutput is the 4-act narrative structure produced by the
    // Structurer agent (FR10). Validated against
    // testdata/contracts/structurer_output.schema.json (FR11).
    type StructurerOutput struct {
        SCPID            string `json:"scp_id"`
        Acts             []Act  `json:"acts"`               // exactly 4, in order: incident, mystery, revelation, unresolved
        TargetSceneCount int    `json:"target_scene_count"` // sum of Acts[*].SceneBudget; 8..12 per PRD structure rules
        SourceVersion    string `json:"source_version"`     // "v1-deterministic" — matches ResearcherOutput.SourceVersion
    }

    // Act is one of the 4 INCIDENT-FIRST acts (see docs/prompts/scenario/02_structure.md).
    // V1 assigns dramatic beats to acts deterministically — no LLM generated prose.
    type Act struct {
        ID              string   `json:"id"`                // "incident" | "mystery" | "revelation" | "unresolved"
        Name            string   `json:"name"`              // human label (e.g. "Act 1 — Incident")
        Synopsis        string   `json:"synopsis"`          // one-line summary built from assigned beats
        SceneBudget     int      `json:"scene_budget"`      // 1..5, sums to TargetSceneCount
        DurationRatio   float64  `json:"duration_ratio"`    // 0.15 | 0.30 | 0.40 | 0.15 (fixed per spec)
        DramaticBeatIDs []int    `json:"dramatic_beat_ids"` // indices into ResearcherOutput.DramaticBeats
        KeyPoints       []string `json:"key_points"`        // Description strings of assigned beats (beat-index order)
    }
    ```

    Required constants — declare beside the types:

    ```go
    const (
        ActIncident   = "incident"
        ActMystery    = "mystery"
        ActRevelation = "revelation"
        ActUnresolved = "unresolved"

        SourceVersionV1 = "v1-deterministic"
    )

    // ActOrder pins the canonical act order — tests assert StructurerOutput.Acts
    // is returned in this exact sequence (JSON Schema minItems/maxItems does not
    // enforce ordering; the Go test does).
    var ActOrder = [4]string{ActIncident, ActMystery, ActRevelation, ActUnresolved}

    // ActDurationRatio is the fixed per-act duration ratio from the prompt spec
    // (docs/prompts/scenario/02_structure.md). Keep in lockstep with schema.
    var ActDurationRatio = map[string]float64{
        ActIncident:   0.15,
        ActMystery:    0.30,
        ActRevelation: 0.40,
        ActUnresolved: 0.15,
    }
    ```

    Do NOT add validation methods on the structs — validation flows through the JSON-schema path (AC-SCHEMA-VALIDATOR). Struct-level guards duplicate the schema and drift.

    Add `internal/domain/scenario_test.go`:
    - `TestResearcherOutput_JSONRoundTrip` — marshal a populated value, assert snake_case keys, assert re-unmarshaled value equals original via `reflect.DeepEqual`.
    - `TestStructurerOutput_JSONRoundTrip` — same pattern.
    - `TestStructurerOutput_ActOrderConstant` — `len(ActOrder) == 4`, matches the string constants, `ActDurationRatio` sums to `1.00` within `1e-9`.
    - `TestResearcherOutput_NoOmitemptyOnRequired` — scan the struct via reflection; assert none of the 12 fields carry `,omitempty`. JSON Schema requires every key to be present even when zero. Without this guard an accidental `,omitempty` silently breaks the contract.

2. **AC-PROMOTE-PIPELINESTATE-FIELDS:** Modify `internal/pipeline/agents/agent.go` (owned by Story 3.1) to promote the `Research` and `Structure` slots from `json.RawMessage` to typed domain pointers:

    ```go
    // BEFORE (Story 3.1 scaffold):
    Research  json.RawMessage `json:"research,omitempty"`
    Structure json.RawMessage `json:"structure,omitempty"`

    // AFTER (Story 3.2):
    Research  *domain.ResearcherOutput `json:"research,omitempty"`
    Structure *domain.StructurerOutput `json:"structure,omitempty"`
    ```

    Concurrently update Story 3.1's `TestPipelineState_JSONShape` in `internal/pipeline/agents/agent_test.go`:
    - Zero-valued state still marshals to `{"run_id":"","scp_id":"","started_at":"","finished_at":""}` (pointer nils + `omitempty` drop the slots — identical to the `json.RawMessage` nil behaviour).
    - A populated state (Research + Structure set to non-nil pointers) round-trips: `Marshal → Unmarshal → Marshal` is byte-identical.
    - The new assertion: `state.Research != nil && state.Research.SCPID == "SCP-TEST"` after unmarshal — proves the typed promotion works.

    Keep the other four agent output fields as `json.RawMessage` — Stories 3.3/3.4/3.5 promote those. Do NOT touch `PipelineStage`, `PipelineStage.DomainStage()`, `NoopAgent()`, or `PhaseARunner` (out of scope).

    The `agents` package must `import "github.com/sushistack/youtube.pipeline/internal/domain"` — this is already allowed per `scripts/lintlayers/main.go:233` (`{"internal/domain", "internal/clock"}`). Verify the import stays within the existing allowlist; NO changes to `scripts/lintlayers/main.go` in this story.

    Add `TestPipelineState_ResearchStructureTyped` to `agent_test.go`: construct a `PipelineState{Research: &domain.ResearcherOutput{SCPID: "SCP-TEST"}, Structure: &domain.StructurerOutput{SCPID: "SCP-TEST"}}`, marshal it, unmarshal into a fresh `PipelineState`, assert both pointers are non-nil and the nested `SCPID` fields round-trip.

3. **AC-CORPUS-READER:** `internal/pipeline/agents/corpus.go` (NEW) declares the corpus-access contract used by the Researcher agent. Filesystem I/O is permitted in the agents package via an injected reader (architecture §Agent Purity Rule line 1731: "File-system reads are permitted via an injected Reader interface, never via the global filesystem"). The reader interface lives here — not in `domain/` — because corpus layout is a pipeline-layer concern.

    ```go
    // CorpusReader loads the per-SCP source files used by the Researcher agent.
    // Implementations:
    //   - filesystemCorpus: reads {dataDir}/{scp_id}/ on the real disk.
    //   - Test fakes: inject via the CorpusReader parameter on NewResearcher
    //     to avoid real I/O.
    //
    // Paths are conceptual — the interface does not expose them. Callers see
    // three parsed blobs (facts.json parsed, meta.json parsed, main.txt raw)
    // and a sentinel (ErrCorpusNotFound wrapping domain.ErrNotFound) when the
    // SCP directory is absent.
    type CorpusReader interface {
        Read(ctx context.Context, scpID string) (CorpusDocument, error)
    }

    // CorpusDocument is the parsed corpus payload for a single SCP ID.
    // facts.json and meta.json are pre-parsed into structs so the Researcher
    // does not duplicate parsing; main.txt is raw bytes (already UTF-8).
    type CorpusDocument struct {
        SCPID    string
        Facts    SCPFacts
        Meta     SCPMeta
        MainText string // raw main.txt (UTF-8)
    }

    // SCPFacts mirrors the /mnt/data/raw/{scp_id}/facts.json schema documented
    // by the corpus (see /mnt/data/raw/SCP-173/facts.json as reference shape).
    // Missing optional fields decode to zero values. Opaque fields
    // (incidents, related_documents, cross_references) use `[]any` because
    // V1 ignores them — a richer schema is V1.5 concern.
    type SCPFacts struct {
        SCPID                 string            `json:"scp_id"`
        Title                 string            `json:"title"`
        ObjectClass           string            `json:"object_class"`
        Rating                int               `json:"rating"`
        PhysicalDescription   string            `json:"physical_description"`
        AnomalousProperties   []string          `json:"anomalous_properties"`
        ContainmentProcedures string            `json:"containment_procedures"`
        BehaviorAndNature     string            `json:"behavior_and_nature"`
        OriginAndDiscovery    string            `json:"origin_and_discovery"`
        Incidents             []any             `json:"incidents"`
        RelatedDocuments      []any             `json:"related_documents"`
        VisualElements        SCPVisualElements `json:"visual_elements"`
        CrossReferences       []any             `json:"cross_references"`
        Tags                  []string          `json:"tags"`
    }

    type SCPVisualElements struct {
        Appearance             string   `json:"appearance"`
        DistinguishingFeatures []string `json:"distinguishing_features"`
        EnvironmentSetting     string   `json:"environment_setting"`
        KeyVisualMoments       []string `json:"key_visual_moments"`
    }

    type SCPMeta struct {
        SCPID       string   `json:"scp_id"`
        Tags        []string `json:"tags"`
        RelatedDocs []string `json:"related_docs"`
    }

    // ErrCorpusNotFound wraps domain.ErrNotFound — the SCP directory is absent.
    // Retryable: false. HTTP 404 via domain.Classify(). Declared as a package
    // variable so callers can errors.Is-match against both this sentinel and
    // domain.ErrNotFound (wrap chain).
    var ErrCorpusNotFound = fmt.Errorf("corpus not found: %w", domain.ErrNotFound)
    ```

    Filesystem implementation — returned by `NewFilesystemCorpus`:

    ```go
    // NewFilesystemCorpus returns a CorpusReader rooted at dataDir
    // (domain.PipelineConfig.DataDir — defaults to "/mnt/data/raw").
    // The returned reader does NOT verify dataDir exists at construction time;
    // missing directories surface as ErrCorpusNotFound from Read().
    func NewFilesystemCorpus(dataDir string) CorpusReader

    // Read loads {dataDir}/{scp_id}/{facts.json,meta.json,main.txt}.
    // Errors:
    //   - SCP directory missing               → ErrCorpusNotFound
    //   - facts.json missing or invalid JSON  → domain.ErrValidation wrapping the parse error
    //   - meta.json missing or invalid JSON   → domain.ErrValidation
    //   - main.txt missing                    → domain.ErrValidation (required)
    //   - main.txt not valid UTF-8            → domain.ErrValidation
    //   - Any other I/O error                 → domain.ErrStageFailed
    ```

    The `scp_id` parameter is passed through verbatim (no case normalization). If the corpus on disk is `SCP-173` and the caller passes `scp-173`, the lookup fails — surface the mismatch rather than silently guessing (avoids "worked on my machine" bugs when operators rename corpus directories).

    Tests in `internal/pipeline/agents/corpus_test.go` (new, use `t.TempDir()` — **never** touch `/mnt/data/raw` from tests):
    - `TestFilesystemCorpus_Read_Happy` — seed a TempDir with a valid SCP subtree, assert all three files parsed.
    - `TestFilesystemCorpus_Read_MissingSCP` — nonexistent SCP ID → `errors.Is(err, domain.ErrNotFound)` true AND `errors.Is(err, ErrCorpusNotFound)` true.
    - `TestFilesystemCorpus_Read_MissingMainText` — facts/meta present, main.txt absent → `errors.Is(err, domain.ErrValidation)` true.
    - `TestFilesystemCorpus_Read_MalformedFacts` — facts.json contains `{` only → `errors.Is(err, domain.ErrValidation)` true.
    - `TestFilesystemCorpus_Read_InvalidUTF8Main` — main.txt with raw `0xFF 0xFE` bytes → `errors.Is(err, domain.ErrValidation)` true.
    - `TestFilesystemCorpus_Read_CaseSensitive` — directory `SCP-173` exists, caller passes `scp-173` → `ErrCorpusNotFound`.

4. **AC-SCHEMA-VALIDATOR:** `internal/pipeline/agents/validator.go` (NEW) exposes a narrow validator that loads schemas from `testdata/contracts/` and validates a single value. Do NOT write a generic schema registry — V1 has exactly two schemas. Keep the surface minimal.

    ```go
    // Validator wraps a compiled JSON Schema. Constructed from a schema file in
    // testdata/contracts/ resolved via a projectRoot anchor passed by the
    // caller (runner / test). Validation errors are wrapped as
    // domain.ErrValidation — fail-fast at every agent boundary (FR11 / NFR-M3).
    type Validator struct {
        schema *jsonschema.Schema
        name   string // relative path, e.g. "researcher_output.schema.json"
    }

    // NewValidator loads and compiles the schema at
    // filepath.Join(projectRoot, "testdata", "contracts", schemaFile).
    // Returns an error wrapping domain.ErrValidation on compile failure (the
    // schema file is a contract — a malformed one is a dev bug and MUST fail
    // loudly).
    func NewValidator(projectRoot, schemaFile string) (*Validator, error)

    // Validate marshals value to JSON and validates it against the schema.
    // On success returns nil. On schema failure returns
    //   fmt.Errorf("schema %s: %s: %w", v.name, describe(errs), domain.ErrValidation)
    // where describe collects a stable, short summary of the jsonschema errors
    // (first ≤3 violations, each "/path: message"). Stability matters because
    // this error message reaches operator-facing logs.
    func (v *Validator) Validate(value any) error
    ```

    **Library choice:** add `github.com/santhosh-tekuri/jsonschema/v5` to `go.mod`. Pure Go, no CGO, supports JSON Schema Draft 7 (the draft used by this story's schema files). Pinned reason: architecture §Test Infrastructure line 228 explicitly calls for a JSON Schema library to preserve Go↔Zod parity (Epic 6 adds the Zod side). Do NOT reinvent with struct-tag validation (would drift from the Zod side).

    **Schema draft version:** every schema file in `testdata/contracts/*.schema.json` declares `"$schema": "http://json-schema.org/draft-07/schema#"`. Compiler defaults in `santhosh-tekuri/jsonschema/v5` pick Draft-07 from the `$schema` keyword. Do not mix 2020-12 — keeping one draft simplifies Zod generation downstream.

    **projectRoot resolution:** in tests, expose `testutil.ProjectRoot(t testing.TB) string` as a thin public wrapper over the existing private `findProjectRoot` in `internal/testutil/fixture.go` (~3 lines; no behavior change). Use it in every V1 Validator call path.

    **Production wiring deferred:** Story 3.1's engine.Advance is a stub through Story 3.5; Phase A only executes inside tests in V1. That means Validator is constructed only from test paths in V1. Story 3.5 (or a later story) picks the production schema-source strategy: `go:embed` a local copy at `internal/pipeline/agents/schemas/` with a drift-check test against `testdata/contracts/`, OR read from a config-specified path. This story explicitly does NOT choose — picking prematurely wastes work if the runner wiring ends up using a different convention. Add a deferred-work entry (see AC-DEFERRED) noting the choice is open.

    Tests in `internal/pipeline/agents/validator_test.go`:
    - `TestValidator_Validate_Happy` — load `researcher_output.schema.json`, validate a known-good struct, assert `err == nil`.
    - `TestValidator_Validate_MissingRequired` — remove `scp_id` from the value (use `map[string]any`), assert `errors.Is(err, domain.ErrValidation)` and error message contains `scp_id`.
    - `TestValidator_Validate_WrongType` — set `dramatic_beats` to a string instead of array, assert `errors.Is(err, domain.ErrValidation)`.
    - `TestValidator_NewValidator_SchemaNotFound` — pass a nonexistent filename, assert returned error is `errors.Is(err, domain.ErrValidation)`.
    - `TestValidator_NewValidator_MalformedSchema` — write `{` to TempDir, point the validator there, assert `errors.Is(err, domain.ErrValidation)`.
    - `TestContract_ResearcherOutput_SampleValidates` — validate `testdata/contracts/researcher_output.sample.json` against its schema, assert no error.
    - `TestContract_StructurerOutput_SampleValidates` — same for structurer.

5. **AC-CONTRACT-FIXTURES:** add four files under `testdata/contracts/` and pin them as single-source-of-truth per architecture §Structural Rules rule #4 ("contracts/ never auto-updated"). Do NOT add a `-update` flag or any auto-refresh path — every change must be manually reviewed in the PR diff.

    **Required files:**

    - `testdata/contracts/researcher_output.schema.json` — Draft-07 JSON Schema covering all 12 fields of `domain.ResearcherOutput` with `additionalProperties: false` at the top level. Nested `visual_identity` and each `dramatic_beats[]` element also `additionalProperties: false`. Required fields: all 12 top-level keys are required. `anomalous_properties`, `dramatic_beats`, `tags` are arrays (not nullable). `main_text_excerpt` is `maxLength: 4000`. `source_version` is `const: "v1-deterministic"`. `dramatic_beats[].emotional_tone` is `enum: ["mystery", "horror", "tension", "revelation"]`. Add `"description"` at top level noting "Owned by Story 3.2 — NEVER auto-update. Manual PR review required (architecture §Structural Rules #4)."

    - `testdata/contracts/researcher_output.sample.json` — canonical valid fixture for the SCP-TEST fixture corpus (AC-FIXTURE-CORPUS). Must validate against the schema — `TestContract_ResearcherOutput_SampleValidates` runs the validator against this file.

    - `testdata/contracts/structurer_output.schema.json` — Draft-07 JSON Schema. Top-level required: `scp_id`, `acts`, `target_scene_count`, `source_version`. `acts` is an array with `minItems: 4, maxItems: 4`. Each `acts[]` element has required `id`, `name`, `synopsis`, `scene_budget`, `duration_ratio`, `dramatic_beat_ids`, `key_points`. `id` is `enum: ["incident", "mystery", "revelation", "unresolved"]`. `scene_budget` is `integer, minimum: 1, maximum: 5`. `duration_ratio` is `number, minimum: 0.0, maximum: 1.0`. `target_scene_count` is `integer, minimum: 8, maximum: 12`. `source_version` is `const: "v1-deterministic"`. `additionalProperties: false` everywhere. Add the same top-level `description`.

    - `testdata/contracts/structurer_output.sample.json` — canonical valid fixture matching the SCP-TEST seed run-through. `TestContract_StructurerOutput_SampleValidates` asserts the validator accepts it.

    **Do NOT** add a JSON schema for `pipeline_state.json` (Story 1.3 owns the existing `testdata/contracts/pipeline_state.json` Run fixture — out of scope here). Do NOT embed `ResearcherOutput` schema inside `StructurerOutput` schema recursively (Structurer schema references none of the Researcher schema — that coupling would force Structurer tests to depend on a Researcher fixture; keep them orthogonal).

6. **AC-FIXTURE-CORPUS:** ship a self-contained V1 corpus fixture under `testdata/fixtures/corpus/SCP-TEST/` so tests never rely on `/mnt/data/raw` (which is machine-specific and NOT in the repo). Files:

    - `testdata/fixtures/corpus/SCP-TEST/facts.json` — a realistic but compact fixture patterned on `/mnt/data/raw/SCP-173/facts.json`. Required content:
        - `scp_id`: `"SCP-TEST"`.
        - `title`: `"SCP-TEST"`.
        - `object_class`: `"Euclid"`.
        - `rating`: any integer (tests do not use it).
        - `physical_description`: ≥ 80 chars.
        - `anomalous_properties`: array of 4 distinct strings.
        - `containment_procedures`: ≥ 50 chars.
        - `behavior_and_nature`: ≥ 30 chars.
        - `origin_and_discovery`: ≥ 30 chars.
        - `incidents`, `related_documents`, `cross_references`: empty arrays.
        - `visual_elements`: populated — `appearance` (≥50 chars), `distinguishing_features` (4 strings), `environment_setting` (≥50 chars), `key_visual_moments` (4 strings).
        - `tags`: 4 distinct strings.
    - `testdata/fixtures/corpus/SCP-TEST/meta.json` — `{"scp_id":"SCP-TEST","tags":[...same 4 tags],"related_docs":[]}`.
    - `testdata/fixtures/corpus/SCP-TEST/main.txt` — ≥ 200 UTF-8 characters including at least one multi-byte character (Korean hangul or em-dash) so the truncation test can exercise rune-vs-byte counting without crafting a separate fixture.

    The `researcher_output.sample.json` and `structurer_output.sample.json` fixtures (AC-CONTRACT-FIXTURES) MUST be byte-for-byte what the V1 deterministic agents produce when given this SCP-TEST corpus. If you change the SCP-TEST fixture during dev, regenerate both samples by running the happy-path test with a scratch "write output to file" scaffold, then manually review — the PR diff MUST show both fixture file changes (consistent with architecture §Structural Rules rule #4 "contracts/ never auto-updated").

    **Do NOT** seed fixtures from `/mnt/data/raw/SCP-173/` — that file is not in the repo and its rating/tag content will drift. Author `SCP-TEST` independently with fictional but schema-valid content.

7. **AC-RESEARCHER:** `internal/pipeline/agents/researcher.go` (NEW) implements the Researcher agent as a closure conforming to `agents.AgentFunc` (the type defined by Story 3.1). Constructor signature:

    ```go
    // NewResearcher builds an AgentFunc that reads the SCP corpus via the
    // injected CorpusReader and populates state.Research with a validated
    // *domain.ResearcherOutput. Validation uses a Validator loaded from
    // testdata/contracts/researcher_output.schema.json.
    //
    // Returns domain.ErrValidation (wrapped with context) when:
    //   - state.SCPID is empty.
    //   - Corpus read returns ErrCorpusNotFound (wrapped; higher layer
    //     Classifies → 404).
    //   - The synthesized output has fewer than 3 dramatic beats (sparse
    //     corpus cannot seed 4-act structuring — see Structurer constraint).
    //   - Schema validation of the synthesized output fails (impossible if
    //     derivation is correct — fail loudly if it happens so a regression
    //     surfaces in CI rather than silently producing bad scenario.json).
    // Returns domain.ErrStageFailed (wrapped) for transient corpus I/O errors.
    func NewResearcher(corpus CorpusReader, validator *Validator) AgentFunc
    ```

    Add the V1 top-of-file comment:

    ```go
    // V1: This agent is deterministic — no LLM call. docs/prompts/scenario/
    // 01_research.md is the reference prompt template that a V1.5 upgrade
    // will wire through a domain.TextGenerator. The deterministic derivation
    // here is the scaffolding and schema contract that the LLM path must honor.
    ```

    **Derivation rules (deterministic V1 — V1.5 replaces with LLM):**

    a. `ResearcherOutput.SCPID` ← `state.SCPID` (verbatim, no case normalization).
    b. `Title`, `ObjectClass`, `PhysicalDescription`, `ContainmentProcedures`, `BehaviorAndNature`, `OriginAndDiscovery` ← corresponding `CorpusDocument.Facts.*` fields verbatim.
    c. `AnomalousProperties` ← `Facts.AnomalousProperties` (copy — do NOT alias the slice; later mutation on the output must not retroactively mutate the input).
    d. `VisualIdentity.*` ← `Facts.VisualElements.*` (field-by-field copy; same slice-copy rule for `DistinguishingFeatures` and `KeyVisualMoments`).
    e. `DramaticBeats` built via `buildDramaticBeats(facts)`:
       - For each entry in `Facts.VisualElements.KeyVisualMoments` (in order): append a beat with `Source="visual_moment"`, `Description=entry`, `EmotionalTone` assigned per AC-BEAT-TONE.
       - Then for each entry in `Facts.AnomalousProperties` (in order): append a beat with `Source="anomalous_property"`, `Description=entry`, `EmotionalTone` assigned per AC-BEAT-TONE.
       - Assign `Index` in append order (0, 1, 2, ...).
       - Hard cap at 10 beats total (trim surplus — matches the prompt spec's "6-10 dramatic beats" upper bound).
       - If fewer than 3 beats are produced, return `fmt.Errorf("sparse corpus: %d beats < 3: %w", n, domain.ErrValidation)` — the corpus is too thin for narrative structuring downstream.
    f. `MainTextExcerpt` ← `CorpusDocument.MainText` truncated at **4000 runes** (not bytes — UTF-8 correctness), then `strings.TrimSpace` at the boundary. Use `utf8.RuneCountInString` + a rune-index scan, NOT `len(s) >= 4000 { s = s[:4000] }` (byte slicing splits multi-byte runes). If the source is ≤ 4000 runes it passes through verbatim (after TrimSpace).
    g. `Tags` ← `Facts.Tags` if non-empty, else `Meta.Tags`. Copy the slice. If both are empty, set to `[]string{}` (empty slice, **not** nil — so the JSON serializes to `[]`, not `null`; the schema requires an array).
    h. `SourceVersion` ← `domain.SourceVersionV1` constant.

    After assembly, call `validator.Validate(output)`. On success, `state.Research = &output; return nil`.

    **AC-BEAT-TONE:** `emotional_tone` is assigned by cycling through a fixed rotation to guarantee variation (the prompt spec requires adjacent-scene tonal variation; we pre-seed this at the beat level so Structurer's assignment preserves the diversity):

    ```go
    var beatToneRotation = [4]string{"mystery", "horror", "tension", "revelation"}
    ```

    Rotation index `= beat.Index % 4`. This is deterministic, pairs well with `domain.ActOrder`, and the test `TestResearcher_BeatTones_Rotate` asserts that consecutive beats never share a tone.

    Tests in `internal/pipeline/agents/researcher_test.go`:
    - `TestResearcher_Run_SCPTest_Happy` — inject the `SCP-TEST` fixture corpus via `NewFilesystemCorpus(testutil.ProjectRoot(t) + "/testdata/fixtures/corpus")` (resolve the full path by whatever helper AC-SCHEMA-VALIDATOR chose), run the agent against `PipelineState{SCPID: "SCP-TEST"}`, assert no error, assert `state.Research != nil`, assert `state.Research.Title == "SCP-TEST"`, assert `len(state.Research.DramaticBeats) >= 3`, assert `state.Research.SourceVersion == domain.SourceVersionV1`, assert the marshaled output validates against `researcher_output.schema.json`.
    - `TestResearcher_Run_Validates_SampleFixture` — run Researcher against SCP-TEST, marshal its output, `testutil.AssertJSONEq` against `testdata/contracts/researcher_output.sample.json` (pins the canonical deterministic output byte-by-byte so unintended derivation changes fail loudly).
    - `TestResearcher_Run_EmptySCPID` — `PipelineState{SCPID: ""}` → `errors.Is(err, domain.ErrValidation)`; `state.Research` stays `nil`.
    - `TestResearcher_Run_MissingCorpus` — fake CorpusReader returns `ErrCorpusNotFound`; agent returns an error wrapping `domain.ErrNotFound`; `state.Research` stays `nil`.
    - `TestResearcher_Run_SparseCorpus` — fake CorpusReader returns a doc with zero `key_visual_moments` and zero `anomalous_properties` → agent returns `errors.Is(err, domain.ErrValidation)` with "sparse corpus"; state untouched.
    - `TestResearcher_Run_MainTextTruncation` — fake CorpusDocument with a 10000-rune main.txt; assert `utf8.RuneCountInString(state.Research.MainTextExcerpt) <= 4000` AND the output still validates (assert both the rune count AND schema validity).
    - `TestResearcher_Run_TagsFallback_FromMeta` — `Facts.Tags = []`, `Meta.Tags = ["a", "b"]` → output `Tags = ["a", "b"]`.
    - `TestResearcher_Run_TagsFallback_BothEmpty` — both empty → output `Tags = []` (empty slice, not nil; verify via `reflect.DeepEqual(output.Tags, []string{})` AND by checking the marshaled JSON contains `"tags":[]` not `"tags":null`).
    - `TestResearcher_Run_SliceIsolation` — mutate the input `Facts.AnomalousProperties[0]` after the agent returns; assert `state.Research.AnomalousProperties[0]` is unchanged (proves the slice was copied, not aliased).
    - `TestResearcher_BeatTones_Rotate` — for 4+ consecutive beats, assert no two adjacent beats share an `EmotionalTone`.
    - `TestResearcher_Run_CallsBlockExternalHTTP` — call `testutil.BlockExternalHTTP(t)` at the top of a table-driven test; the agent must not construct any HTTP client. Verified indirectly via AC-AGENTS-NO-NETWORK source scan plus runtime guarantee that blocked transport would reject any outbound call.

8. **AC-STRUCTURER:** `internal/pipeline/agents/structurer.go` (NEW) implements the Structurer as a pure `AgentFunc` over `state.Research`.

    ```go
    // NewStructurer returns an AgentFunc that reads state.Research and
    // populates state.Structure with a 4-act *domain.StructurerOutput.
    // Purely deterministic in V1 — no LLM, no filesystem I/O. Validation
    // is against testdata/contracts/structurer_output.schema.json.
    //
    // Returns domain.ErrValidation (wrapped) when:
    //   - state.Research is nil (Researcher must have run first).
    //   - state.Research.DramaticBeats has fewer than 4 beats (cannot seed 4 acts).
    //   - Schema validation of the synthesized output fails.
    func NewStructurer(validator *Validator) AgentFunc
    ```

    Add the V1 top-of-file comment:

    ```go
    // V1: This agent is deterministic — no LLM call. docs/prompts/scenario/
    // 02_structure.md is the reference prompt template that a V1.5 upgrade
    // will wire through a domain.TextGenerator. The deterministic derivation
    // here is the scaffolding and schema contract that the LLM path must honor.
    ```

    **Derivation rules (deterministic V1):**

    a. `StructurerOutput.SCPID` ← `state.Research.SCPID`.
    b. `Acts` constructed in `domain.ActOrder` with `Act.ID` = the ordering string, `Act.Name` = `"Act " + n + " — " + titleCase(ID)` (n = 1..4; `titleCase("incident")` = `"Incident"` — write a small local ASCII helper, no external library), `Act.DurationRatio` = `domain.ActDurationRatio[ID]`.
    c. `Acts[].SceneBudget` distribution — total target scene count T = **10 scenes** (pinned V1 constant; falls inside PRD's 8-12 envelope). Distribution computed via `distributeSceneBudget(T)` (AC-STRUCTURER-BUDGET-ALGORITHM).
    d. `Acts[].DramaticBeatIDs` distributed by beat index modulo 4 — `beat.Index % 4 == 0` → Incident, `== 1` → Mystery, `== 2` → Revelation, `== 3` → Unresolved. Ensures each act gets at least ⌊N/4⌋ beats when N ≥ 4. Sort IDs ascending within each act.
    e. `Acts[].KeyPoints` ← the `Description` strings of the assigned beats, in `Index` order (same order as `DramaticBeatIDs`).
    f. `Acts[].Synopsis` ← deterministic format: `"Act " + n + " opens with " + firstKeyPoint + ". (" + len(key_points) + " beats; " + strconv.Itoa(int(duration_ratio*100)) + "% of runtime.)"`. If `len(key_points) == 0` (should never happen given the ≥4 beats precondition): `"No dramatic beats assigned to this act (V1 deterministic placeholder)."`. The synopsis is a byte-equal deterministic string — pinned for golden comparison.
    g. `TargetSceneCount` ← T (V1 constant = 10).
    h. `SourceVersion` ← `domain.SourceVersionV1`.

    After assembly, call `validator.Validate(output)`. On success, `state.Structure = &output; return nil`.

    **AC-STRUCTURER-BUDGET-ALGORITHM:** the distribution function MUST be pure and unit-tested independently. Use the **Largest Remainder Method** (Hamilton apportionment) — it is the standard apportionment algorithm, deterministic, and matches "fair integer shares from fractional weights" which is exactly the spec. Signature:

    ```go
    // distributeSceneBudget splits `target` scenes across acts in domain.ActOrder,
    // weighted by domain.ActDurationRatio, using the Largest Remainder Method:
    //   1. quota[i]  = target * ActDurationRatio[ActOrder[i]]
    //   2. floor[i]  = floor(quota[i])
    //   3. frac[i]   = quota[i] - floor[i]
    //   4. r         = target - sum(floor)
    //   5. distribute +1 to the r acts with the largest frac[i] (ties resolved
    //      by ActOrder index ascending — Incident wins over Unresolved).
    //   6. enforce scene_budget >= 1 for every act by subtracting 1 from the
    //      act with the current largest allocation (ties: index descending)
    //      and adding 1 to the zero act. Only triggers for pathologically
    //      small targets (< 4); target ∈ [8,12] never hits this path.
    //
    // Guarantees:
    //   - Sum equals target exactly.
    //   - Every act gets scene_budget >= 1.
    //   - Deterministic across runs (no randomness, no map iteration order).
    func distributeSceneBudget(target int) [4]int
    ```

    Pinned outputs (computed by the algorithm above — test golden values):
    | target | Incident | Mystery | Revelation | Unresolved | Reasoning |
    |---|---|---|---|---|---|
    | 8  | 1 | 3 | 3 | 1 | quotas [1.2, 2.4, 3.2, 1.2], floors [1,2,3,1] sum 7, r=1, largest frac is Mystery (0.4) → Mystery+=1 |
    | 9  | 1 | 3 | 4 | 1 | quotas [1.35, 2.7, 3.6, 1.35], floors [1,2,3,1] sum 7, r=2, top fracs Mystery (0.7), Revelation (0.6) → both +=1 |
    | 10 | 2 | 3 | 4 | 1 | quotas [1.5, 3.0, 4.0, 1.5], floors [1,3,4,1] sum 9, r=1, fracs tied at 0.5 between Incident and Unresolved → ActOrder-ascending picks Incident |
    | 11 | 2 | 3 | 4 | 2 | quotas [1.65, 3.3, 4.4, 1.65], floors [1,3,4,1] sum 9, r=2, top fracs Incident (0.65), Unresolved (0.65) — wait, tied with Revelation (0.4) and Mystery (0.3)? Actually top 2 by frac desc: Incident 0.65, Unresolved 0.65 → both +=1 |
    | 12 | 2 | 3 | 5 | 2 | quotas [1.8, 3.6, 4.8, 1.8], floors [1,3,4,1] sum 9, r=3, fracs [0.8, 0.6, 0.8, 0.8] — top 3 = Incident (0.8), Revelation (0.8), Unresolved (0.8) → all +=1 |

    Tests for `distributeSceneBudget`:
    - `TestDistributeSceneBudget_Target10` → `[2, 3, 4, 1]`.
    - `TestDistributeSceneBudget_Target8` → `[1, 3, 3, 1]`.
    - `TestDistributeSceneBudget_Target9` → `[1, 3, 4, 1]`.
    - `TestDistributeSceneBudget_Target11` → `[2, 3, 4, 2]`.
    - `TestDistributeSceneBudget_Target12` → `[2, 3, 5, 2]`.
    - `TestDistributeSceneBudget_MinimumOne` — for every target ∈ [8,12], assert every act gets ≥1.
    - `TestDistributeSceneBudget_SumsToTarget` — table-driven over targets 8..12, assert sum equals target.
    - `TestDistributeSceneBudget_Deterministic` — call 100× with target=10, assert identical `[4]int` every invocation (no randomness, no map-order dependence).
    - `TestDistributeSceneBudget_TieBreaker` — target=10 produces `[2, 3, 4, 1]` (Incident wins over Unresolved on the 0.5-frac tie); if this ever flips to `[1, 3, 4, 2]` the ActOrder tie-breaker direction has regressed.

    Structurer agent tests in `internal/pipeline/agents/structurer_test.go`:
    - `TestStructurer_Run_Happy` — inject a hand-built `state.Research` with 4 beats (one per tone), assert no error, `state.Structure.Acts` in `domain.ActOrder`, each act has the expected `SceneBudget`, sum of `SceneBudget == 10`.
    - `TestStructurer_Run_Validates_SampleFixture` — run against SCP-TEST Researcher output (chain Researcher → Structurer in one test), `testutil.AssertJSONEq` the marshaled result against `testdata/contracts/structurer_output.sample.json`.
    - `TestStructurer_Run_NilResearch` — `state.Research == nil` → `errors.Is(err, domain.ErrValidation)`; `state.Structure` unchanged.
    - `TestStructurer_Run_InsufficientBeats` — `state.Research.DramaticBeats` has 3 elements → `errors.Is(err, domain.ErrValidation)` with "insufficient beats".
    - `TestStructurer_Run_BeatAssignmentModulo` — 8 beats with rotating tones → act 0 gets beats [0,4], act 1 gets [1,5], act 2 gets [2,6], act 3 gets [3,7].
    - `TestStructurer_Run_SynopsisDeterministic` — call structurer twice on the same state, assert byte-equal output both times.
    - `TestStructurer_Run_ActDurationRatioSum` — assert `sum(acts[i].duration_ratio) == 1.0` within `1e-9`.
    - `TestStructurer_Run_SourceVersionPropagates` — assert `state.Structure.SourceVersion == state.Research.SourceVersion` (both `domain.SourceVersionV1` in V1).

9. **AC-PROMPTS-NOT-WIRED:** do NOT load or reference `docs/prompts/scenario/01_research.md` or `02_structure.md` from Go code in this story. They are placeholder authoring artifacts; LLM-wired implementation is deferred (AC-DEFERRED). Add `internal/pipeline/agents/no_prompt_read_test.go`:

    ```go
    // TestAgents_NoPromptFileReferences parses the non-test source files of the
    // agents package and asserts no string literal equals the V1.5-reserved
    // prompt paths. This guards the V1 scope boundary — accidentally wiring
    // the prompt template before Story 3.3 lands would conflict with Writer's
    // LLM integration in a subtle way.
    func TestAgents_NoPromptFileReferences(t *testing.T) { ... }
    ```

    Implementation: `go/parser.ParseDir` on the agents package (excluding `_test.go` files via a filter), walk `ast.BasicLit` nodes, assert neither `"docs/prompts/scenario/01_research.md"` nor `"docs/prompts/scenario/02_structure.md"` appears. Use `testutil.ProjectRoot(t)` as the anchor. If a future story needs to reference the paths (e.g. V1.5 LLM wiring), it must delete this test.

10. **AC-AGENTS-NO-NETWORK:** extend or add `internal/pipeline/agents/network_guard_test.go`:

    ```go
    // TestAgents_PackageImports_NoNetPkgs parses the non-test source files of
    // the agents package and asserts none import "net/http", "net/url", or
    // "net". V1 agents are deterministic and file-only; if a future revision
    // adds LLM wiring via domain.TextGenerator (which is the SOLE permitted
    // route), the HTTP client is constructed in llmclient/ and passed through
    // the domain interface — NOT imported directly in agents/.
    func TestAgents_PackageImports_NoNetPkgs(t *testing.T) { ... }
    ```

    Rationale: `domain.TextGenerator` is an interface declared in `internal/domain`; its HTTP-using implementations live in `internal/llmclient/{dashscope,deepseek,gemini}`. The agents package layer-lint allowlist (`{internal/domain, internal/clock}`) already forbids `internal/llmclient` imports; this test adds the stdlib-level guardrail.

11. **AC-FR-COVERAGE:** update `testdata/fr-coverage.json` — add exactly three entries for FR9, FR10, FR11. Do NOT remove or reorder existing entries; insert between FR8 and FR38 to preserve numerical order. Entries:

    ```json
    {
      "fr_id": "FR9",
      "test_ids": [
        "TestResearcher_Run_SCPTest_Happy",
        "TestResearcher_Run_Validates_SampleFixture",
        "TestResearcher_Run_MissingCorpus",
        "TestResearcher_Run_SparseCorpus",
        "TestFilesystemCorpus_Read_Happy",
        "TestFilesystemCorpus_Read_MissingSCP"
      ],
      "annotation": "Researcher produces schema-validated summary from local /mnt/data/raw corpus (FR9); V1 deterministic — LLM wiring deferred to Story 3.3+"
    },
    {
      "fr_id": "FR10",
      "test_ids": [
        "TestStructurer_Run_Happy",
        "TestStructurer_Run_Validates_SampleFixture",
        "TestStructurer_Run_NilResearch",
        "TestStructurer_Run_InsufficientBeats",
        "TestStructurer_Run_BeatAssignmentModulo",
        "TestDistributeSceneBudget_Target10"
      ],
      "annotation": "Structurer produces 4-act narrative structure from ResearcherOutput (FR10); INCIDENT-FIRST scaffold with fixed duration ratios 0.15/0.30/0.40/0.15"
    },
    {
      "fr_id": "FR11",
      "test_ids": [
        "TestValidator_Validate_Happy",
        "TestValidator_Validate_MissingRequired",
        "TestValidator_Validate_WrongType",
        "TestContract_ResearcherOutput_SampleValidates",
        "TestContract_StructurerOutput_SampleValidates"
      ],
      "annotation": "Inter-agent JSON Schema validation at every handoff (FR11 / NFR-M3); schemas in testdata/contracts/ are SSoT"
    }
    ```

    If `scripts/frcoverage/` has a validation entry point, run it and confirm it parses; if it fails due to grace mode being off, flag in Dev Agent Record but do not block.

12. **AC-DEP:** add `github.com/santhosh-tekuri/jsonschema/v5` to `require` in `go.mod`. Pin to the current `v5` minor (run `go get github.com/santhosh-tekuri/jsonschema/v5@latest` and capture the exact version in `go.mod`; do NOT use a floating `@latest` in the committed `go.mod`). Run `go mod tidy` and commit both `go.mod` and `go.sum`. Verify:

    ```
    go build ./...
    CGO_ENABLED=0 go build ./cmd/pipeline
    go test ./...
    ```

    The CGO_ENABLED=0 check matters: `santhosh-tekuri/jsonschema/v5` is pure Go, but a bad `go mod tidy` can pull surprises. Fail loudly if CGO becomes required.

13. **AC-LAYER-CLEAN:** `scripts/lintlayers` remains green after this story. **NO changes to `scripts/lintlayers/main.go` are permitted** — Story 3.1 already added the `internal/pipeline/agents` entry with `{internal/domain, internal/clock}`. Verify:

    ```
    go run ./scripts/lintlayers
    ```

    Expected: `layer-import lint: OK`. Possible violations to watch for:
    - Accidentally importing `internal/db` in a new agent file → fix by removing the import; DB access is NOT allowed in agents.
    - Accidentally importing `internal/llmclient` → fix by removing; LLM calls route through `domain.TextGenerator`, which is NOT present in V1 agents.
    - Accidentally importing `internal/pipeline` (the parent package) → would be a cycle; fix by keeping all shared types in `agents` itself or `domain`.

14. **AC-NOT-IN-SCOPE (explicit non-goals):** the following are NOT implemented in this story; PRs adding them should be rejected during review:

    - Runner wiring (Story 3.1 owns `PhaseARunner`).
    - Any LLM call, `domain.TextGenerator` wiring, or prompt-template loading (Story 3.3 for Writer).
    - `scenario.json` file output (Story 3.1 writes the final Phase-A artifact).
    - Engine.Advance wiring (Story 3.5 — see 3.1 AC-ENGINE-ADVANCE-UNCHANGED).
    - Writer ≠ Critic provider check (FR12 — Story 3.3).
    - Forbidden-term enforcement (FR48 — Story 3.3).
    - Any migration, DB table change, or HTTP handler.
    - Modifying `PipelineStage` / `PipelineStage.DomainStage()` / `NoopAgent()`.
    - Modifying the layer-import linter allowlist (owned by Story 3.1).

    Re-state in the Dev Agent Record if scope drift is suspected.

15. **AC-DEFERRED:** append to `_bmad-output/implementation-artifacts/deferred-work.md` **two** entries (do NOT wholesale rewrite the file):

    ```markdown
    - **LLM-driven Researcher & Structurer.** Story 3.2 shipped a deterministic V1 that reads /mnt/data/raw corpus and synthesizes `*domain.ResearcherOutput` / `*domain.StructurerOutput` by field copy + modulo distribution. The prompt templates at `docs/prompts/scenario/01_research.md` and `docs/prompts/scenario/02_structure.md` are unreferenced by Go code (guarded by `TestAgents_NoPromptFileReferences`). A V1.5 story will wire `domain.TextGenerator` through both agents, swap `SourceVersion` to `"v1.5-llm"`, keep the schema validators unchanged (they are the contract), and delete the no-prompt-references test. Until then V1 output has limited narrative quality — expect the first Golden-eval round (Epic 4) to measure the gap.

    - **Production schema source for `agents.Validator`.** Story 3.2's `NewValidator(projectRoot, schemaFile)` resolves `testdata/contracts/*.schema.json` through a project-root anchor that only makes sense in tests. Production usage (when Story 3.5 wires `engine.Advance` → `PhaseARunner.Run`) will need a different source: either a `//go:embed schemas/*.json` inside `internal/pipeline/agents/` with a drift-check test against `testdata/contracts/`, or a config-specified on-disk path shipped alongside the binary. The choice is deliberately open here — premature embedding couples the validator to a layout Story 3.5 may want to change. Until Story 3.5 picks, Phase A in production is gated by the `engine.Advance` stub (Story 3.1 AC-ENGINE-ADVANCE-UNCHANGED), so no production path invokes the Validator with an undefined projectRoot.
    ```

    This preserves the deferred-work registry's role as the living "what-we-owe-ourselves" ledger.

## Tasks / Subtasks

- [ ] **T1: Domain scenario types** (AC: #1)
  - [ ] Create `internal/domain/scenario.go` — `ResearcherOutput`, `VisualIdentity`, `DramaticBeat`, `StructurerOutput`, `Act` structs with snake_case JSON tags, no `,omitempty` on required fields.
  - [ ] Declare constants `ActIncident`, `ActMystery`, `ActRevelation`, `ActUnresolved`, `SourceVersionV1`, `ActOrder`, `ActDurationRatio`.
  - [ ] Create `internal/domain/scenario_test.go` — round-trip tests + `TestResearcherOutput_NoOmitemptyOnRequired` + `TestStructurerOutput_ActOrderConstant`.

- [ ] **T2: Promote PipelineState slots to typed pointers** (AC: #2)
  - [ ] In `internal/pipeline/agents/agent.go`: change `Research` field to `*domain.ResearcherOutput`, `Structure` field to `*domain.StructurerOutput`. Add `import "github.com/sushistack/youtube.pipeline/internal/domain"`.
  - [ ] In `internal/pipeline/agents/agent_test.go`: update `TestPipelineState_JSONShape` to reflect the new typed fields (zero-valued state still marshals to `{"run_id":"","scp_id":"","started_at":"","finished_at":""}` via `omitempty`).
  - [ ] Add `TestPipelineState_ResearchStructureTyped`.

- [ ] **T3: Corpus reader** (AC: #3)
  - [ ] Create `internal/pipeline/agents/corpus.go` — `CorpusReader`, `CorpusDocument`, `SCPFacts`, `SCPVisualElements`, `SCPMeta`, `ErrCorpusNotFound`, `NewFilesystemCorpus`.
  - [ ] Create `internal/pipeline/agents/corpus_test.go` — all 6 filesystem tests (TempDir-rooted, no touching `/mnt/data/raw`).

- [ ] **T4: JSON schema validator + dependency** (AC: #4, #12)
  - [ ] `go get github.com/santhosh-tekuri/jsonschema/v5@<pinned-version>`, run `go mod tidy`. Commit `go.mod` and `go.sum`.
  - [ ] Expose a test helper `testutil.ProjectRoot(t)` (or equivalent) so the validator can resolve `testdata/contracts/` across packages. Update `internal/testutil/fixture.go` minimally; do NOT refactor existing `LoadFixture` users.
  - [ ] Create `internal/pipeline/agents/validator.go` — `Validator`, `NewValidator`, `Validate`.
  - [ ] Create `internal/pipeline/agents/validator_test.go` — 5 validator tests + 2 contract-sample tests (schema + sample happy-path).

- [ ] **T5: Contract schemas + sample fixtures** (AC: #5)
  - [ ] `testdata/contracts/researcher_output.schema.json` — Draft-07, all-required, `additionalProperties: false`, `main_text_excerpt` maxLength 4000, `source_version` const, beat tone enum, top-level description note.
  - [ ] `testdata/contracts/researcher_output.sample.json` — canonical SCP-TEST output (produced by the happy-path Researcher run).
  - [ ] `testdata/contracts/structurer_output.schema.json` — Draft-07, 4-act minItems/maxItems, act ID enum, budget 1-5, ratio 0.0-1.0, target_scene_count 8-12, `source_version` const.
  - [ ] `testdata/contracts/structurer_output.sample.json` — canonical SCP-TEST output.

- [ ] **T6: Corpus fixture (SCP-TEST)** (AC: #6)
  - [ ] `testdata/fixtures/corpus/SCP-TEST/facts.json` — populate every required field per AC-FIXTURE-CORPUS.
  - [ ] `testdata/fixtures/corpus/SCP-TEST/meta.json` — 4 tags matching `facts.json`, empty `related_docs`.
  - [ ] `testdata/fixtures/corpus/SCP-TEST/main.txt` — ≥200 UTF-8 chars, at least one multi-byte rune (Korean or em-dash).

- [ ] **T7: Researcher agent** (AC: #7, #9)
  - [ ] Create `internal/pipeline/agents/researcher.go` — V1 comment, `NewResearcher`, `buildDramaticBeats`, `copyStringSlice`, `truncateMainText`, `beatToneRotation`.
  - [ ] Create `internal/pipeline/agents/researcher_test.go` — 11 tests from AC-RESEARCHER.

- [ ] **T8: Structurer agent** (AC: #8, #9)
  - [ ] Create `internal/pipeline/agents/structurer.go` — V1 comment, `NewStructurer`, `distributeSceneBudget`, `titleCase`.
  - [ ] Create `internal/pipeline/agents/structurer_test.go` — 8 agent tests + 6 `distributeSceneBudget` tests.

- [ ] **T9: Guardrails** (AC: #9, #10)
  - [ ] Create `internal/pipeline/agents/no_prompt_read_test.go` — `TestAgents_NoPromptFileReferences` via `go/parser` literal scan.
  - [ ] Create `internal/pipeline/agents/network_guard_test.go` — `TestAgents_PackageImports_NoNetPkgs` via import-list walk.

- [ ] **T10: FR coverage + deferred work** (AC: #11, #15)
  - [ ] Insert FR9, FR10, FR11 entries into `testdata/fr-coverage.json` between FR8 and FR38.
  - [ ] Append LLM-wiring deferral entry to `_bmad-output/implementation-artifacts/deferred-work.md`.

- [ ] **T11: Build + test + lint verification** (AC: #12, #13, #14)
  - [ ] `go build ./...` — clean.
  - [ ] `CGO_ENABLED=0 go build ./cmd/pipeline` — clean.
  - [ ] `go test ./...` — all pass (especially `./internal/pipeline/agents/...` and `./internal/domain/...`).
  - [ ] `go run ./scripts/lintlayers` — prints `layer-import lint: OK`.
  - [ ] Manual: scan diff for any of: new migration, DB table change, HTTP handler, LLM import, `NoopAgent` or `PipelineStage` modification → none permitted. Record confirmation in Dev Agent Record.

## Dev Notes

### V1 Scope Rationale

The user brief pins **"외부 API 없음"** — no external API in this story. Two options considered:

1. **Ship a `TextGenerator`-aware agent with a stub fake in tests.** Rejected — creates an LLM fake code surface that later gets deleted when Story 3.3 wires the real providers. Pure waste.
2. **Ship a deterministic V1 that honors the schema contract.** Chosen — the schema contract (validation at agent boundary) IS the invariant that Story 3.3's Writer checkpoint and Story 4.1's Golden eval depend on. Deterministic derivation gives full schema coverage today; the LLM swap in V1.5 replaces the derivation function without touching the surrounding infrastructure.

The `SourceVersion` field is the gate: `"v1-deterministic"` marks the current implementation; V1.5 pins `"v1.5-llm"` and Golden fixtures will version per gate. Do NOT remove `SourceVersion` — it is the migration story's anchor.

### Architecture Compliance Map

- **Agent Purity Rule** (architecture line 1729) — satisfied. Agents are pure functions; filesystem I/O is through the injected `CorpusReader`; no DB/HTTP access. [Source: _bmad-output/planning-artifacts/architecture.md#Agent Purity Rule]
- **Plain Function Chain** (architecture line 681) — satisfied. Both agents conform to Story 3.1's `AgentFunc` type verbatim. [Source: _bmad-output/planning-artifacts/architecture.md#Pipeline Execution Model]
- **Inter-agent Schema Validation at every handoff** (architecture line 690) — satisfied. `validator.Validate(output)` runs after each agent synthesizes its output; failures wrap `domain.ErrValidation`. [Source: _bmad-output/planning-artifacts/architecture.md#Pipeline Execution Model]
- **Single-source-of-truth contracts** (architecture line 228) — satisfied. Schemas live in `testdata/contracts/`, validated via `santhosh-tekuri/jsonschema/v5`, never auto-updated. [Source: _bmad-output/planning-artifacts/architecture.md#Structural Rules]
- **Stricter agent-package imports** (Story 3.1 AC-PURITY-LINT) — satisfied. New code in `internal/pipeline/agents/` imports only `internal/domain` + stdlib + external libs. No `internal/db`, `internal/llmclient`, or `internal/pipeline`.
- **snake_case single naming** (architecture line 995) — satisfied. Every JSON tag is snake_case; no camelCase transforms anywhere.
- **CGO_ENABLED=0** (architecture §Technology Decisions) — satisfied. `santhosh-tekuri/jsonschema/v5` is pure Go.

### Testing Standards Summary

- Tests live beside code under test (`foo.go` + `foo_test.go`). Package name matches (`package agents`, not `agents_test`) unless cross-package API coverage is needed.
- `testutil.AssertEqual[T comparable]` for comparable types; `reflect.DeepEqual` + formatted errors for slices/maps of structs.
- `testutil.AssertJSONEq` for JSON structural equality (contract sample fixture tests).
- `testutil.BlockExternalHTTP(t)` at the top of every test file's setup helper.
- Every test uses `t.TempDir()` for filesystem scratch; never write to `/mnt/data/raw`, `~/.youtube-pipeline`, or `testdata/` during a test run.
- Table-driven tests for algorithmic functions (`distributeSceneBudget`, `buildDramaticBeats`). Named cases via `name string` field.
- Naming: `TestXxx_CaseName` — match the house style in [internal/pipeline/antiprogress_test.go](../../internal/pipeline/antiprogress_test.go).

### File Organization After This Story

```
internal/
  pipeline/
    agents/                             # Story 3.1 created this directory
      agent.go                          # MODIFIED — Research/Structure fields promoted to *domain.ResearcherOutput / *domain.StructurerOutput
      agent_test.go                     # MODIFIED — TestPipelineState_JSONShape updated; new TestPipelineState_ResearchStructureTyped
      noop.go                           # unchanged (Story 3.1)
      noop_test.go                      # unchanged (Story 3.1)
      doc.go                            # unchanged (Story 3.1)
      corpus.go                         # NEW (this story)
      corpus_test.go                    # NEW
      validator.go                      # NEW
      validator_test.go                 # NEW
      researcher.go                     # NEW
      researcher_test.go                # NEW
      structurer.go                     # NEW
      structurer_test.go                # NEW
      no_prompt_read_test.go            # NEW
      network_guard_test.go             # NEW
    phase_a.go                          # unchanged (Story 3.1)
    ...                                 # other pipeline files unchanged
internal/domain/
  scenario.go                           # NEW (this story)
  scenario_test.go                      # NEW
internal/testutil/
  fixture.go                            # MODIFIED — expose ProjectRoot(t) helper (minimal change)
testdata/
  contracts/
    researcher_output.schema.json       # NEW
    researcher_output.sample.json       # NEW
    structurer_output.schema.json       # NEW
    structurer_output.sample.json       # NEW
  fixtures/
    corpus/
      SCP-TEST/
        facts.json                      # NEW
        meta.json                       # NEW
        main.txt                        # NEW
  fr-coverage.json                      # MODIFIED — FR9, FR10, FR11 entries added
_bmad-output/implementation-artifacts/
  deferred-work.md                      # MODIFIED — LLM-wiring deferral entry appended
go.mod                                  # MODIFIED — santhosh-tekuri/jsonschema/v5 added
go.sum                                  # MODIFIED
```

Stories 3.3–3.5 will add `writer.go`, `visual_breaker.go`, `reviewer.go`, `critic.go` to the same directory. Do NOT create stubs for those here.

### Deferred Concerns (Tracked, Not Done)

- **LLM-driven agents** — see AC-DEFERRED; tracked in `deferred-work.md`.
- **Per-stage observability wiring** — Researcher and Structurer don't yet emit `RecordStageObservation` rows; that wires up when Story 3.1's `PhaseARunner` instruments per-agent cost/tokens (3.1 explicitly defers that instrumentation — see 3.1 AC-ENGINE-ADVANCE-UNCHANGED + the 2.7 `Recorder` already built for Epic 2).
- **Cost-cap enforcement** — no LLM = no cost. Kicks in when Story 3.3 adds Writer LLM calls.
- **Anti-progress detection** — FR8 requires retry loops; V1 deterministic agents have no retry semantics. Wires when LLM retry is added (Story 3.3 Writer, Story 3.4 Reviewer).
- **Case-insensitive corpus lookup** — deliberately NOT implemented; see AC-CORPUS-READER rationale.
- **scenario.json typed contract fixture** (`testdata/contracts/phase_a_state.json`) — Story 3.5 adds once all six slots are typed.

### Previous Story Intelligence

- **Story 3.1 is the direct prerequisite.** It is `ready-for-dev` in `sprint-status.yaml` at the time of this story's creation, which means the dev agent will usually implement 3.1 first, then 3.2. If 3.2 lands on a branch where 3.1 hasn't been merged, the dev agent MUST rebase onto 3.1 rather than inlining its scaffold. See Prerequisites section. [Source: [_bmad-output/implementation-artifacts/3-1-agent-function-chain-pipeline-runner.md](3-1-agent-function-chain-pipeline-runner.md)]
- **PipelineState typed fields are an explicit Story 3.1 hand-off.** Quote from 3.1's Dev Notes (§Deferred Work This Story May Generate): "PipelineState typed fields: Stories 3.2–3.5 each promote one slot from `json.RawMessage` to a domain type." This story promotes exactly two slots (Research + Structure); 3.3 promotes Narration + Critic (Writer+Critic), 3.4 promotes VisualBreakdown + Review, 3.5 promotes nothing new but finalizes the scenario.json schema fixture.
- **The layer-import linter's nested-package support was added by Story 3.1.** 3.2 does NOT need to touch `scripts/lintlayers/main.go`. If you see violations related to the allowlist, STOP — it means 3.1's AC-PURITY-LINT did not land cleanly. Fix 3.1 before continuing.
- **Fixture seed pattern** — see `testdata/fixtures/observability_seed.sql` for Epic 2's SQL seeds. Our fixture is a corpus directory, not SQL, but the same version-controlled / multi-test / manually-reviewed-on-change principle transfers.
- **Sentinel domain errors + `errors.Is` classification** — see `ErrAntiProgress` in Story 2.5. `ErrCorpusNotFound` in this story follows the same wrap-domain-sentinel pattern. [Source: [internal/domain/errors.go:28](../../internal/domain/errors.go#L28)]
- **`testutil.AssertEqual` + `testutil.AssertJSONEq`** — see [internal/testutil/assert.go:11](../../internal/testutil/assert.go#L11).
- **Source-tree parsing test precedent** — see `internal/domain/types_test.go` from Story 1.3 (AC-IMPORT) for the `go/parser` import-list walk approach used by our `TestAgents_PackageImports_NoNetPkgs`.
- **JSON Schema library choice is unprecedented in the repo** — this is the first schema-validation-at-runtime feature. Pick `santhosh-tekuri/jsonschema/v5` and stop; `xeipuuv/gojsonschema` is less actively maintained and has Draft-07-only limitations hurting the V1.5 upgrade path.

### Project Structure Notes

- `domain/` file count after this story: existing files (types.go, errors.go, config.go, llm.go, observability.go, metrics.go, hitl.go, resume.go) + new scenario.go = 9 Go files. None near the 300-line cap. [Source: architecture.md §Structural Rules rule #1]
- `internal/pipeline/agents/` gains 5 production files + 5 test files (+ 2 guardrail test files) — sub-package stays below any pragmatic cap.
- `testdata/contracts/` currently has 5 JSON files; after this story it has 9 (schema + sample × 2 new pairs). Still tractable; no index file needed.
- `testdata/fixtures/corpus/SCP-TEST/` is a new subtree. Previous fixtures are all `.sql`; this is the first filesystem-corpus fixture. Structure mirrors `/mnt/data/raw/{scp_id}/` so production and test paths look identical.
- `internal/testutil/fixture.go` — exposing `ProjectRoot(t)` as a public helper is a minimal change; alternative is to keep `findProjectRoot` private and duplicate ~10 lines in `agents/validator.go`'s test setup. Prefer the helper — single source of truth for project-root discovery.

### Anti-Patterns to Avoid (Observed in Prior LLM Attempts)

1. **"Let me make agents generic via reflection"** — no. Two agents, two concrete types, two tests. A generic framework helps with 10+ same-shape agents; we have 2 + 4 more following. YAGNI.
2. **"I'll cache the compiled schema globally"** — no. `Validator` is constructed per agent at factory time; each agent closure holds its own. Global caches introduce test-order sensitivity.
3. **"I'll parse main.txt into structured sections"** — no. `main_text_excerpt` is a rune-truncated raw dump. The structure is already in `facts.json`. Over-parsing `main.txt` creates a second schema with no test coverage.
4. **"I'll add a retry loop around corpus Read"** — no. Filesystem reads of a local corpus are not retryable; an I/O failure is a doctor-preflight problem. Surface `ErrStageFailed` and let the operator fix the disk.
5. **"I'll validate `ResearcherOutput` with struct tags AND the JSON schema"** — no. Single source of truth: JSON Schema. Struct tags duplicate and drift silently.
6. **"I'll make `CorpusReader` also synthesize `ResearcherOutput`"** — no. CorpusReader is a data gateway; Researcher is a derivation step. Combining them collapses two separately-testable responsibilities.
7. **"Let me normalize SCP IDs to uppercase before lookup"** — no. See AC-CORPUS-READER: deliberate case sensitivity prevents "works on my machine" bugs where operators rename corpus directories.
8. **"I'll skip the empty-slice vs nil-slice distinction"** — no. Schema `required: ["tags"]` + `type: array` fails on `null` but passes on `[]`. Test `TestResearcher_Run_TagsFallback_BothEmpty` pins the semantic.
9. **"I'll embed the prompt markdown files via `go:embed` for future use"** — no. AC-PROMPTS-NOT-WIRED forbids it; V1.5 adds the embed at wire-up time.
10. **"I'll add a `critic_score` field to `ResearcherOutput` in case we need it"** — no. Critic scoring is Story 3.3+; speculative fields create schema debt.
11. **"I'll modify Story 3.1's `PipelineState` to include Writer/VisualBreak/Review/Critic domain pointers while I'm in there"** — no. Each downstream story owns its own promotion. Touching them here creates merge conflicts and premature schema commitments.

### References

- [_bmad-output/planning-artifacts/epics.md:1158-1177 — Story 3.2 AC](../planning-artifacts/epics.md#L1158)
- [_bmad-output/planning-artifacts/epics.md:402-421 — Epic 3 scope](../planning-artifacts/epics.md#L402)
- [_bmad-output/planning-artifacts/prd.md:1256-1258 — FR9/FR10/FR11 definitions](../planning-artifacts/prd.md#L1256)
- [_bmad-output/planning-artifacts/prd.md:1450-1452 — NFR-M3 stage-boundary schemas](../planning-artifacts/prd.md#L1450)
- [_bmad-output/planning-artifacts/architecture.md:681-693 — Agent Chain: Plain Function Chain](../planning-artifacts/architecture.md#L681)
- [_bmad-output/planning-artifacts/architecture.md:785-822 — Inter-agent Data Flow + Artifact File Structure](../planning-artifacts/architecture.md#L785)
- [_bmad-output/planning-artifacts/architecture.md:1541-1563 — pipeline/ directory tree](../planning-artifacts/architecture.md#L1541)
- [_bmad-output/planning-artifacts/architecture.md:1729-1735 — Agent Purity Rule](../planning-artifacts/architecture.md#L1729)
- [_bmad-output/planning-artifacts/architecture.md:1782-1797 — Structural Rules (contracts/ never auto-updated)](../planning-artifacts/architecture.md#L1782)
- [_bmad-output/planning-artifacts/architecture.md:227-228 — contract SSoT + JSON Schema library](../planning-artifacts/architecture.md#L227)
- [docs/prompts/scenario/01_research.md](../../docs/prompts/scenario/01_research.md) — reference prompt for V1.5 LLM wiring (NOT used in V1)
- [docs/prompts/scenario/02_structure.md](../../docs/prompts/scenario/02_structure.md) — reference prompt for V1.5 LLM wiring (NOT used in V1)
- [internal/domain/types.go](../../internal/domain/types.go) — Run/Episode/Shot types precedent
- [internal/domain/errors.go](../../internal/domain/errors.go) — DomainError + Classify pattern
- [internal/pipeline/antiprogress.go](../../internal/pipeline/antiprogress.go) — pure pipeline-layer component precedent (Story 2.5)
- [internal/pipeline/similarity.go](../../internal/pipeline/similarity.go) — rune/byte handling precedent
- [internal/testutil/assert.go](../../internal/testutil/assert.go) — `AssertEqual[T]` + `AssertJSONEq` helpers
- [internal/testutil/nohttp.go](../../internal/testutil/nohttp.go) — `BlockExternalHTTP` enforcement
- [internal/testutil/fixture.go](../../internal/testutil/fixture.go) — `LoadFixture` + `findProjectRoot` helpers (expose `ProjectRoot` per T4)
- [scripts/lintlayers/main.go:21-33](../../scripts/lintlayers/main.go#L21) — layer import allowlist (Story 3.1 adds the agents entry)
- [testdata/contracts/pipeline_state.json](../../testdata/contracts/pipeline_state.json) — Story 1.3 contract fixture precedent
- [testdata/fr-coverage.json](../../testdata/fr-coverage.json) — FR coverage entries
- [_bmad-output/implementation-artifacts/3-1-agent-function-chain-pipeline-runner.md](3-1-agent-function-chain-pipeline-runner.md) — **direct prerequisite**, owns `AgentFunc`, `PipelineState`, `PipelineStage`, `PhaseARunner`, `NoopAgent`, layer-lint allowlist
- [_bmad-output/implementation-artifacts/2-5-anti-progress-detection.md](2-5-anti-progress-detection.md) — cosine detector + deterministic ACs precedent
- [_bmad-output/implementation-artifacts/1-3-domain-types-error-system-architecture-interfaces.md](1-3-domain-types-error-system-architecture-interfaces.md) — domain-type story precedent
- [/mnt/data/raw/SCP-173/facts.json](file:///mnt/data/raw/SCP-173/facts.json) — production corpus shape reference (machine-local; used only for schema shape design, NOT in tests)

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
