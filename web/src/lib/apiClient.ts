import { z } from "zod";
import {
  characterGroupResponseSchema,
  batchApproveAllRemainingResponseSchema,
  createRunResponseSchema,
  descriptorPrefillResponseSchema,
  metadataBundleSchema,
  reviewItemListResponseSchema,
  runDetailResponseSchema,
  runListResponseSchema,
  runResumeResponseSchema,
  sceneDecisionResponseSchema,
  sceneRegenResponseSchema,
  runStatusResponseSchema,
  sceneEditResponseSchema,
  sceneListResponseSchema,
  scpCanonicalImageResponseSchema,
  sourceManifestSchema,
  timelineListResponseSchema,
  undoResponseSchema,
} from "../contracts/runContracts";
import {
  settingsResponseSchema,
  type SettingsConfig,
} from "../contracts/settingsContracts";
import {
  calibrationResponseSchema,
  criticPromptResponseSchema,
  fastFeedbackResponseSchema,
  goldenPairResponseSchema,
  goldenReportResponseSchema,
  goldenStateResponseSchema,
  shadowReportResponseSchema,
} from "../contracts/tuningContracts";

const API_ROOT = "/api";

const errorEnvelopeSchema = z.object({
  error: z
    .object({
      code: z.string().min(1),
      details: z.unknown().optional(),
      message: z.string().min(1),
    })
    .optional(),
  version: z.literal(1).optional(),
});

export class ApiClientError extends Error {
  code?: string;
  details?: unknown;
  status: number;

  constructor(message: string, status: number, code?: string, details?: unknown) {
    super(message);
    this.code = code;
    this.details = details;
    this.name = "ApiClientError";
    this.status = status;
  }
}

async function parseJson(response: Response) {
  const text = await response.text();
  return text.length === 0 ? null : JSON.parse(text);
}

async function fetchWithErrorEnvelope(path: string, init?: RequestInit) {
  const response = await fetch(`${API_ROOT}${path}`, {
    ...init,
    headers: {
      Accept: "application/json",
      ...(init?.headers ?? {}),
    },
  });

  const payload = await parseJson(response);

  if (!response.ok) {
    const parsed_error = errorEnvelopeSchema.safeParse(payload);
    throw new ApiClientError(
      parsed_error.data?.error?.message ??
        `API request failed (${response.status})`,
      response.status,
      parsed_error.data?.error?.code,
      parsed_error.data?.error?.details,
    );
  }

  return { payload, headers: response.headers };
}

async function fetchPayloadOnly(path: string, init?: RequestInit) {
  const { payload } = await fetchWithErrorEnvelope(path, init);
  return payload;
}

async function apiRequest<T>(
  path: string,
  schema: z.ZodType<{ data: T }>,
  init?: RequestInit,
) {
  const payload = await fetchPayloadOnly(path, init);
  return schema.parse(payload).data;
}

/**
 * apiRequestRaw parses the response body directly with the provided schema —
 * no `{data: T}` wrapper — while still extracting the standard error envelope
 * on non-2xx responses. Use for endpoints that serve raw JSON artifacts.
 */
async function apiRequestRaw<T>(
  path: string,
  schema: z.ZodType<T>,
  init?: RequestInit,
) {
  const payload = await fetchPayloadOnly(path, init);
  return schema.parse(payload);
}

export function fetchRunList() {
  return apiRequest("/runs", runListResponseSchema).then((data) => data.items);
}

export function createRun(scp_id: string) {
  return apiRequest("/runs", createRunResponseSchema, {
    body: JSON.stringify({ scp_id }),
    headers: { "Content-Type": "application/json" },
    method: "POST",
  });
}

export function fetchRunDetail(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}`,
    runDetailResponseSchema,
  );
}

export function fetchRunStatus(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/status`,
    runStatusResponseSchema,
  );
}

// resumeRun re-enters a failed or waiting run at its current stage.
//
// Optional drop_caches: list of deterministic-agent stages whose cached
// artifacts the operator unchecked in the failure-banner cache panel. Sent as
// `{"drop_caches": [...]}` body ONLY when the array is non-empty — keeping
// the legacy resume call shape (empty body object) for callers that don't
// pass options. Mirrors advanceRun's contract so the same UI panel composes
// against either endpoint depending on the run's status surface.
export function resumeRun(
  run_id: string,
  options?: { drop_caches?: string[] },
) {
  const drop_caches = options?.drop_caches ?? [];
  const body = drop_caches.length > 0 ? { drop_caches } : {};
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/resume`,
    runResumeResponseSchema,
    {
      body: JSON.stringify(body),
      headers: { "Content-Type": "application/json" },
      method: "POST",
    },
  );
}

// cancelRun aborts a running or waiting run. Server returns 409 for any other
// state; surface the error so the operator can decide whether to refresh or
// pick a different run.
export function cancelRun(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/cancel`,
    runDetailResponseSchema,
    {
      method: "POST",
    },
  );
}

/**
 * rewindRun rolls a run back to the chosen stepper work-phase node, deleting
 * every artifact produced after that point (DB rows, on-disk files, decisions,
 * HITL session). The server validates that target_stage_node ∈ {scenario,
 * character, assets, assemble} AND that the target is strictly before the
 * run's current stage; mismatches surface as 400 / 409.
 *
 * Synchronous: the response is the post-rewind run snapshot, ready to render
 * the new stepper position immediately.
 */
export type RewindStageNode =
  | "scenario"
  | "character"
  | "assets"
  | "assemble";

export function rewindRun(run_id: string, target_stage_node: RewindStageNode) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/rewind`,
    runDetailResponseSchema,
    {
      method: "POST",
      body: JSON.stringify({ target_stage_node }),
      headers: { "Content-Type": "application/json" },
    },
  );
}

// advanceRun kicks off a freshly-created pending run via Phase A entry.
// resumeRun rejects pending status by design (it is the failed/waiting recovery
// path), so the UI's Start-run button on the pending guidance card calls this
// instead. Backend route: POST /api/runs/{id}/advance.
//
// Optional drop_caches: list of deterministic-agent stages whose cached
// artifacts the operator unchecked in the pending-state cache panel. Sent as
// `{"drop_caches": [...]}` body ONLY when the array is non-empty — keeping
// the legacy advance path byte-identical for callers that don't pass options.
export function advanceRun(
  run_id: string,
  options?: { drop_caches?: string[] },
) {
  const drop_caches = options?.drop_caches ?? [];
  const init: RequestInit = { method: "POST" };
  if (drop_caches.length > 0) {
    init.body = JSON.stringify({ drop_caches });
    init.headers = { "Content-Type": "application/json" };
  }
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/advance`,
    runDetailResponseSchema,
    init,
  );
}

// --- Cache panel (pending runs) ---

// runCacheEntrySchema mirrors the Go cacheEntryResponse on the wire. stage is
// open-ended (snake_case key) rather than enum so the backend can introduce
// new stages without bumping a schema; the UI just renders whatever is
// present. source_version may be empty when the cache file is unparseable.
const runCacheEntrySchema = z.object({
  stage: z.string().min(1),
  filename: z.string().min(1),
  size_bytes: z.number().int().nonnegative(),
  modified_at: z.string().min(1),
  source_version: z.string(),
});

const runCacheResponseSchema = z.object({
  data: z.object({
    caches: z.array(runCacheEntrySchema),
  }),
});

export type RunCacheEntry = z.infer<typeof runCacheEntrySchema>;

/**
 * GET /api/runs/{id}/cache — lists deterministic-agent caches present on
 * disk for the run. Returns an empty caches array when the run dir is
 * missing or empty. Status-gating is the caller's responsibility (the hook
 * gates on pending status to avoid polling during Phase A execution).
 */
export function fetchRunCache(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/cache`,
    runCacheResponseSchema,
  ).then((data) => data.caches);
}

export function fetchRunScenes(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/scenes`,
    sceneListResponseSchema,
  ).then((data) => data.items);
}

export function fetchBatchReviewItems(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/review-items`,
    reviewItemListResponseSchema,
  ).then((data) => data.items);
}

export function fetchDecisionsTimeline(params?: {
  before_created_at?: string;
  before_id?: number;
  decision_type?: string;
  limit?: number;
}) {
  const search_params = new URLSearchParams();
  if (params?.decision_type) {
    search_params.set("decision_type", params.decision_type);
  }
  if (params?.limit != null) {
    search_params.set("limit", String(params.limit));
  }
  if (params?.before_created_at) {
    search_params.set("before_created_at", params.before_created_at);
  }
  if (params?.before_id != null) {
    search_params.set("before_id", String(params.before_id));
  }

  const query = search_params.toString();
  const path = query.length > 0 ? `/decisions?${query}` : "/decisions";
  return apiRequest(path, timelineListResponseSchema);
}

export function editSceneNarration(
  run_id: string,
  scene_index: number,
  narration: string,
) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/scenes/${scene_index}/edit`,
    sceneEditResponseSchema,
    {
      method: "POST",
      body: JSON.stringify({ narration }),
      headers: { "Content-Type": "application/json" },
    },
  );
}

export function recordSceneDecision(
  run_id: string,
  payload: {
    scene_index: number;
    decision_type: "approve" | "reject" | "skip_and_remember";
    context_snapshot?: Record<string, unknown>;
    note?: string | null;
  },
) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/decisions`,
    sceneDecisionResponseSchema,
    {
      method: "POST",
      body: JSON.stringify(payload),
      headers: { "Content-Type": "application/json" },
    },
  );
}

export function dispatchSceneRegeneration(run_id: string, scene_index: number) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/scenes/${scene_index}/regen`,
    sceneRegenResponseSchema,
    {
      method: "POST",
      body: JSON.stringify({}),
      headers: { "Content-Type": "application/json" },
    },
  );
}

export function undoLastDecision(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/undo`,
    undoResponseSchema,
    {
      method: "POST",
      body: JSON.stringify({}),
      headers: { "Content-Type": "application/json" },
    },
  );
}

export function approveAllRemaining(run_id: string, focus_scene_index: number) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/approve-all-remaining`,
    batchApproveAllRemainingResponseSchema,
    {
      method: "POST",
      body: JSON.stringify({ focus_scene_index }),
      headers: { "Content-Type": "application/json" },
    },
  );
}

export function fetchCharacterCandidates(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters`,
    characterGroupResponseSchema,
  );
}

export function searchCharacterCandidates(run_id: string, query: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters?query=${encodeURIComponent(query)}`,
    characterGroupResponseSchema,
  );
}

export function fetchDescriptorPrefill(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters/descriptor`,
    descriptorPrefillResponseSchema,
  );
}

export function pickCharacterWithDescriptor(
  run_id: string,
  candidate_id: string,
  frozen_descriptor: string,
) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters/pick`,
    runDetailResponseSchema,
    {
      method: "POST",
      body: JSON.stringify({ candidate_id, frozen_descriptor }),
      headers: { "Content-Type": "application/json" },
    },
  );
}

export function fetchScpCanonical(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters/canonical`,
    scpCanonicalImageResponseSchema,
  );
}

// generateScpCanonical posts to the canonical endpoint. regenerate=false is
// idempotent: when a library hit exists the server returns the stored record
// without invoking ComfyUI. regenerate=true forces a new image-edit call and
// bumps the version on success. When the run has not yet completed /pick
// (the "preview before commit" miss flow), the operator's chosen candidate
// and descriptor are passed as overrides so the server can generate without
// requiring run.selected_character_id to be persisted.
export function generateScpCanonical(
  run_id: string,
  options: {
    regenerate: boolean;
    candidate_id?: string;
    frozen_descriptor?: string;
  },
) {
  const body: Record<string, unknown> = { regenerate: options.regenerate };
  if (options.candidate_id) body.candidate_id = options.candidate_id;
  if (options.frozen_descriptor) body.frozen_descriptor = options.frozen_descriptor;
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters/canonical`,
    scpCanonicalImageResponseSchema,
    {
      method: "POST",
      body: JSON.stringify(body),
      headers: { "Content-Type": "application/json" },
    },
  );
}

// --- Scenario review approve gate ---

/**
 * POST /api/runs/{id}/scenario/approve — operator approve at scenario_review.
 * Transitions scenario_review/waiting → character_pick/waiting. Returns 409
 * when the run is not paused at scenario_review.
 */
export function approveScenarioReview(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/scenario/approve`,
    runDetailResponseSchema,
    { method: "POST" },
  );
}

/**
 * POST /api/runs/{id}/batch-review/approve — operator finalizes batch review.
 * Transitions batch_review/waiting → assemble/waiting once every scene has a
 * decision. Returns 409 when scenes are still pending or the run is not at
 * batch_review/waiting. Operator triggers Phase C separately via /advance.
 */
export function approveBatchReview(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/batch-review/approve`,
    runDetailResponseSchema,
    { method: "POST" },
  );
}

// --- Story 9.4: Compliance gate ---

/** POST /api/runs/{id}/metadata/ack — NFR-L1 gate. Transitions metadata_ack → complete. */
export function acknowledgeMetadata(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/metadata/ack`,
    runDetailResponseSchema,
    { method: "POST" },
  );
}

/**
 * GET /api/runs/{id}/metadata — serves the raw metadata.json file.
 * Not wrapped in the version envelope; parse the JSON body directly.
 * Error responses still use the standard envelope so `code` is populated.
 */
export function fetchRunMetadata(run_id: string) {
  return apiRequestRaw(
    `/runs/${encodeURIComponent(run_id)}/metadata`,
    metadataBundleSchema,
  );
}

/**
 * GET /api/runs/{id}/manifest — serves the raw manifest.json file.
 * Not wrapped in the version envelope; parse the JSON body directly.
 * Error responses still use the standard envelope so `code` is populated.
 */
export function fetchRunManifest(run_id: string) {
  return apiRequestRaw(
    `/runs/${encodeURIComponent(run_id)}/manifest`,
    sourceManifestSchema,
  );
}

/**
 * fetchSettings returns the settings snapshot together with the ETag header
 * so the caller can echo it as If-Match on subsequent writes (D6 optimistic
 * concurrency). Consumers that don't care about the ETag can discard it.
 */
export async function fetchSettings() {
  const { payload, headers } = await fetchWithErrorEnvelope("/settings");
  const data = settingsResponseSchema.parse(payload).data;
  return { snapshot: data, etag: headers.get("ETag") ?? null };
}

/**
 * updateSettings sends a settings save. `env` accepts:
 *   - `string` → set this secret to the given value
 *   - `null`   → clear this secret from .env (D11)
 *   - key omitted → leave the secret unchanged
 *
 * `etag` is echoed as the If-Match header so concurrent saves fail with
 * ApiClientError.status=409 (SETTINGS_STALE).
 */
export function updateSettings(payload: {
  config: SettingsConfig;
  env: Record<string, string | null>;
  etag?: string | null;
}) {
  const { etag, ...body } = payload;
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (etag) {
    headers["If-Match"] = etag;
  }
  return apiRequest("/settings", settingsResponseSchema, {
    method: "PUT",
    body: JSON.stringify(body),
    headers,
  });
}

/**
 * resetSettingsToDefaults rewrites config.yaml with the built-in defaults —
 * used as the recovery action when config.yaml has become unreadable.
 */
export function resetSettingsToDefaults() {
  return apiRequest("/settings/reset", settingsResponseSchema, {
    method: "POST",
  });
}

// --- Story 10.2: Tuning surface ---

export function fetchCriticPrompt() {
  return apiRequest("/tuning/critic-prompt", criticPromptResponseSchema);
}

export function saveCriticPrompt(body: string) {
  return apiRequest("/tuning/critic-prompt", criticPromptResponseSchema, {
    method: "PUT",
    body: JSON.stringify({ body }),
    headers: { "Content-Type": "application/json" },
  });
}

export function fetchGoldenState() {
  return apiRequest("/tuning/golden", goldenStateResponseSchema);
}

export function runGolden() {
  return apiRequest("/tuning/golden/run", goldenReportResponseSchema, {
    method: "POST",
    body: JSON.stringify({}),
    headers: { "Content-Type": "application/json" },
  });
}

export async function addGoldenPair(positive: File, negative: File) {
  const form = new FormData();
  form.append("positive", positive);
  form.append("negative", negative);
  const payload = await fetchPayloadOnly("/tuning/golden/pairs", {
    method: "POST",
    body: form,
  });
  return goldenPairResponseSchema.parse(payload).data;
}

export function runShadow() {
  return apiRequest("/tuning/shadow/run", shadowReportResponseSchema, {
    method: "POST",
    body: JSON.stringify({}),
    headers: { "Content-Type": "application/json" },
  });
}

export function runFastFeedback() {
  return apiRequest("/tuning/fast-feedback", fastFeedbackResponseSchema, {
    method: "POST",
    body: JSON.stringify({}),
    headers: { "Content-Type": "application/json" },
  });
}

export function fetchCalibration(params?: { window?: number; limit?: number }) {
  const search = new URLSearchParams();
  if (params?.window != null) {
    search.set("window", String(params.window));
  }
  if (params?.limit != null) {
    search.set("limit", String(params.limit));
  }
  const query = search.toString();
  const path = query.length > 0 ? `/tuning/calibration?${query}` : "/tuning/calibration";
  return apiRequest(path, calibrationResponseSchema);
}
