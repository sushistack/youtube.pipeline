import { z } from 'zod'
import {
  characterGroupResponseSchema,
  descriptorPrefillResponseSchema,
  runDetailResponseSchema,
  runListResponseSchema,
  runResumeResponseSchema,
  runStatusResponseSchema,
  sceneEditResponseSchema,
  sceneListResponseSchema,
} from '../contracts/runContracts'

const API_ROOT = '/api'

const errorEnvelopeSchema = z.object({
  error: z
    .object({
      code: z.string().min(1),
      message: z.string().min(1),
    })
    .optional(),
  version: z.literal(1).optional(),
})

export class ApiClientError extends Error {
  code?: string
  status: number

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.code = code
    this.name = 'ApiClientError'
    this.status = status
  }
}

async function parseJson(response: Response) {
  const text = await response.text()
  return text.length === 0 ? null : JSON.parse(text)
}

async function apiRequest<T>(
  path: string,
  schema: z.ZodType<{ data: T }>,
  init?: RequestInit,
) {
  const response = await fetch(`${API_ROOT}${path}`, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.headers ?? {}),
    },
  })

  const payload = await parseJson(response)

  if (!response.ok) {
    const parsed_error = errorEnvelopeSchema.safeParse(payload)
    throw new ApiClientError(
      parsed_error.data?.error?.message ?? `API request failed (${response.status})`,
      response.status,
      parsed_error.data?.error?.code,
    )
  }

  return schema.parse(payload).data
}

export function fetchRunList() {
  return apiRequest('/runs', runListResponseSchema).then((data) => data.items)
}

export function fetchRunDetail(run_id: string) {
  return apiRequest(`/runs/${encodeURIComponent(run_id)}`, runDetailResponseSchema)
}

export function fetchRunStatus(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/status`,
    runStatusResponseSchema,
  )
}

export function resumeRun(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/resume`,
    runResumeResponseSchema,
    {
      body: JSON.stringify({}),
      headers: { 'Content-Type': 'application/json' },
      method: 'POST',
    },
  )
}

export function fetchRunScenes(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/scenes`,
    sceneListResponseSchema,
  ).then((data) => data.items)
}

export function editSceneNarration(run_id: string, scene_index: number, narration: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/scenes/${scene_index}/edit`,
    sceneEditResponseSchema,
    {
      method: 'POST',
      body: JSON.stringify({ narration }),
      headers: { 'Content-Type': 'application/json' },
    },
  )
}

export function fetchCharacterCandidates(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters`,
    characterGroupResponseSchema,
  )
}

export function searchCharacterCandidates(run_id: string, query: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters?query=${encodeURIComponent(query)}`,
    characterGroupResponseSchema,
  )
}

export function fetchDescriptorPrefill(run_id: string) {
  return apiRequest(
    `/runs/${encodeURIComponent(run_id)}/characters/descriptor`,
    descriptorPrefillResponseSchema,
  )
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
      method: 'POST',
      body: JSON.stringify({ candidate_id, frozen_descriptor }),
      headers: { 'Content-Type': 'application/json' },
    },
  )
}
