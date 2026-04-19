import { z } from 'zod'

export const runStageSchema = z.enum([
  'pending',
  'research',
  'structure',
  'write',
  'visual_break',
  'review',
  'critic',
  'scenario_review',
  'character_pick',
  'image',
  'tts',
  'batch_review',
  'assemble',
  'metadata_ack',
  'complete',
])

export const runStatusSchema = z.enum([
  'pending',
  'running',
  'waiting',
  'completed',
  'failed',
  'cancelled',
])

export const runSummarySchema = z.object({
  character_query_key: z.string().nullable().optional(),
  cost_usd: z.number().nonnegative(),
  created_at: z.string().min(1),
  critic_score: z.number().min(0).nullable().optional(),
  duration_ms: z.number().int().nonnegative(),
  frozen_descriptor: z.string().nullable().optional(),
  human_override: z.boolean(),
  id: z.string().min(1),
  retry_count: z.number().int().nonnegative(),
  retry_reason: z.string().min(1).nullable().optional(),
  scp_id: z.string().min(1),
  selected_character_id: z.string().nullable().optional(),
  stage: runStageSchema,
  status: runStatusSchema,
  token_in: z.number().int().nonnegative(),
  token_out: z.number().int().nonnegative(),
  updated_at: z.string().min(1),
})

export const runDetailSchema = runSummarySchema

export const hitlPausedPositionSchema = z.object({
  created_at: z.string().min(1),
  last_interaction_timestamp: z.string().min(1),
  run_id: z.string().min(1),
  scene_index: z.number().int().nonnegative(),
  stage: runStageSchema,
  updated_at: z.string().min(1),
})

export const decisionsSummarySchema = z.object({
  approved_count: z.number().int().nonnegative(),
  pending_count: z.number().int().nonnegative(),
  rejected_count: z.number().int().nonnegative(),
})

export const runStatusChangeSchema = z.object({
  after: z.string().min(1).optional(),
  before: z.string().min(1).optional(),
  kind: z.enum([
    'scene_status_flipped',
    'scene_added',
    'scene_removed',
  ]),
  scene_id: z.string().min(1),
  timestamp: z.string().min(1).optional(),
})

export const runStatusPayloadSchema = z.object({
  changes_since_last_interaction: z.array(runStatusChangeSchema).optional(),
  decisions_summary: decisionsSummarySchema.optional(),
  paused_position: hitlPausedPositionSchema.optional(),
  run: runDetailSchema,
  summary: z.string().min(1).optional(),
})

export const runDetailResponseSchema = z.object({
  data: runDetailSchema,
  version: z.literal(1),
})

export const runListResponseSchema = z.object({
  data: z.object({
    items: z.array(runDetailSchema),
    total: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
})

export const runResumeResponseSchema = z.object({
  data: runDetailSchema,
  version: z.literal(1),
  warnings: z.array(z.string()).optional(),
})

export const runStatusResponseSchema = z.object({
  data: runStatusPayloadSchema,
  version: z.literal(1),
})

export const sceneSchema = z.object({
  narration: z.string(),
  scene_index: z.number().int().nonnegative(),
})

export const sceneListResponseSchema = z.object({
  data: z.object({
    items: z.array(sceneSchema),
    total: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
})

export const sceneEditResponseSchema = z.object({
  data: sceneSchema,
  version: z.literal(1),
})

export const characterCandidateSchema = z.object({
  id: z.string().min(1),
  image_url: z.string().min(1),
  page_url: z.string().min(1),
  preview_url: z.string().min(1).nullable().optional(),
  source_label: z.string().min(1).nullable().optional(),
  title: z.string().min(1).nullable().optional(),
})

export const characterGroupSchema = z.object({
  candidates: z.array(characterCandidateSchema),
  query: z.string().min(1),
  query_key: z.string().min(1),
})

export const characterGroupResponseSchema = z.object({
  data: characterGroupSchema,
  version: z.literal(1),
})

export const descriptorPrefillSchema = z.object({
  auto: z.string(),
  prior: z.string().nullable(),
})

export const descriptorPrefillResponseSchema = z.object({
  data: descriptorPrefillSchema,
  version: z.literal(1),
})

export type Scene = z.infer<typeof sceneSchema>
export type CharacterCandidate = z.infer<typeof characterCandidateSchema>
export type CharacterGroup = z.infer<typeof characterGroupSchema>
export type DescriptorPrefill = z.infer<typeof descriptorPrefillSchema>
export type RunSummary = z.infer<typeof runSummarySchema>
export type RunStatusChange = z.infer<typeof runStatusChangeSchema>
