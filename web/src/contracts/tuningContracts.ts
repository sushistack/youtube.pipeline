import { z } from 'zod'

/**
 * Response envelopes returned by the Story 10.2 `/api/tuning/*` surface.
 *
 * All endpoints use the standard `{ version: 1, data: T }` wrapper; the
 * schemas below describe just the `data` payload to keep call sites
 * consistent with `apiRequest`'s generic unwrap.
 */

export const criticPromptEnvelopeSchema = z.object({
  body: z.string(),
  saved_at: z.string().nullable().optional().default(''),
  prompt_hash: z.string(),
  git_short_sha: z.string(),
  version_tag: z.string().nullable().optional().default(''),
})
export type CriticPromptEnvelope = z.infer<typeof criticPromptEnvelopeSchema>

export const criticPromptResponseSchema = z.object({
  version: z.literal(1),
  data: criticPromptEnvelopeSchema,
})

const goldenReportSchema = z.object({
  recall: z.number(),
  total_negative: z.number().int(),
  detected_negative: z.number().int(),
  false_rejects: z.number().int(),
})
export type GoldenReport = z.infer<typeof goldenReportSchema>

const goldenFreshnessSchema = z.object({
  warnings: z.array(z.string()),
  days_since_refresh: z.number().int(),
  prompt_hash_changed: z.boolean(),
  current_prompt_hash: z.string(),
})
export type GoldenFreshness = z.infer<typeof goldenFreshnessSchema>

const goldenPairSchema = z.object({
  index: z.number().int(),
  created_at: z.string(),
  positive_path: z.string(),
  negative_path: z.string(),
})
export type GoldenPair = z.infer<typeof goldenPairSchema>

export const goldenStateSchema = z.object({
  pairs: z.array(goldenPairSchema),
  pair_count: z.number().int(),
  freshness: goldenFreshnessSchema,
  last_report: goldenReportSchema.optional().nullable(),
})
export type GoldenState = z.infer<typeof goldenStateSchema>

export const goldenStateResponseSchema = z.object({
  version: z.literal(1),
  data: goldenStateSchema,
})

export const goldenReportResponseSchema = z.object({
  version: z.literal(1),
  data: goldenReportSchema,
})

export const goldenPairResponseSchema = z.object({
  version: z.literal(1),
  data: goldenPairSchema,
})

const shadowResultRowSchema = z.object({
  run_id: z.string(),
  created_at: z.string(),
  baseline_verdict: z.string(),
  baseline_score: z.number(),
  new_verdict: z.string(),
  new_retry_reason: z.string().optional().default(''),
  new_overall_score: z.number().int(),
  overall_diff: z.number(),
  false_rejection: z.boolean(),
})
export type ShadowResultRow = z.infer<typeof shadowResultRowSchema>

export const shadowReportSchema = z.object({
  window: z.number().int(),
  evaluated: z.number().int(),
  false_rejections: z.number().int(),
  empty: z.boolean(),
  summary_line: z.string(),
  results: z.array(shadowResultRowSchema),
  version_tag: z.string().optional().default(''),
})
export type ShadowReport = z.infer<typeof shadowReportSchema>

export const shadowReportResponseSchema = z.object({
  version: z.literal(1),
  data: shadowReportSchema,
})

const calibrationPointSchema = z.object({
  computed_at: z.string(),
  window_count: z.number().int(),
  provisional: z.boolean(),
  kappa: z.number().optional().nullable(),
  reason: z.string().optional().default(''),
})
export type CalibrationPoint = z.infer<typeof calibrationPointSchema>

export const calibrationSchema = z.object({
  window: z.number().int(),
  limit: z.number().int(),
  points: z.array(calibrationPointSchema),
  latest: calibrationPointSchema.optional().nullable(),
})
export type Calibration = z.infer<typeof calibrationSchema>

export const calibrationResponseSchema = z.object({
  version: z.literal(1),
  data: calibrationSchema,
})

const fastFeedbackSampleSchema = z.object({
  fixture_id: z.string(),
  verdict: z.string(),
  retry_reason: z.string().optional().default(''),
  overall_score: z.number().int(),
})
export type FastFeedbackSample = z.infer<typeof fastFeedbackSampleSchema>

export const fastFeedbackReportSchema = z.object({
  sample_count: z.number().int(),
  pass_count: z.number().int(),
  retry_count: z.number().int(),
  accept_with_notes_count: z.number().int(),
  duration_ms: z.number().int(),
  version_tag: z.string().optional().default(''),
  samples: z.array(fastFeedbackSampleSchema),
})
export type FastFeedbackReport = z.infer<typeof fastFeedbackReportSchema>

export const fastFeedbackResponseSchema = z.object({
  version: z.literal(1),
  data: fastFeedbackReportSchema,
})
