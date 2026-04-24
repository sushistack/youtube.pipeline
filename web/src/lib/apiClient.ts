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
  sourceManifestSchema,
  timelineListResponseSchema,
  undoResponseSchema,
} from "../contracts/runContracts";
import {
  settingsResponseSchema,
  type SettingsConfig,
} from "../contracts/settingsContracts";

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

export function resumeRun(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/resume`,
    runResumeResponseSchema,
    {
      body: JSON.stringify({}),
      headers: { "Content-Type": "application/json" },
      method: "POST",
    },
  );
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
