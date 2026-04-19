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
  cost_usd: z.number().nonnegative(),
  created_at: z.string().min(1),
  id: z.string().min(1),
  retry_count: z.number().int().nonnegative(),
  scp_id: z.string().min(1),
  stage: runStageSchema,
  status: runStatusSchema,
  updated_at: z.string().min(1),
})

export const runDetailSchema = runSummarySchema

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

