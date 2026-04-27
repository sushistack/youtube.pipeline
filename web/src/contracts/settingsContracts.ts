import { z } from 'zod'

export const settingsConfigSchema = z.object({
  writer_model: z.string().min(1),
  critic_model: z.string().min(1),
  image_model: z.string().min(1),
  tts_model: z.string().min(1),
  tts_voice: z.string().min(1),
  tts_audio_format: z.string().min(1),
  writer_provider: z.string().min(1),
  critic_provider: z.string().min(1),
  image_provider: z.string().min(1),
  tts_provider: z.string().min(1),
  dashscope_region: z.string().min(1),
  cost_cap_research: z.number().nonnegative(),
  cost_cap_write: z.number().nonnegative(),
  cost_cap_image: z.number().nonnegative(),
  cost_cap_tts: z.number().nonnegative(),
  cost_cap_assemble: z.number().nonnegative(),
  cost_cap_per_run: z.number().nonnegative(),
  dry_run: z.boolean(),
})

export const settingsSecretSchema = z.object({
  configured: z.boolean(),
})

export const settingsBudgetSourceSchema = z.object({
  kind: z.enum(['active_run', 'failed_run', 'latest_run', 'none']),
  label: z.string().min(1),
  run_id: z.string().min(1).optional(),
  status: z.string().min(1).optional(),
})

export const settingsBudgetSchema = z.object({
  source: settingsBudgetSourceSchema,
  current_spend_usd: z.number().nonnegative(),
  soft_cap_usd: z.number().nonnegative(),
  hard_cap_usd: z.number().nonnegative(),
  progress_ratio: z.number().nonnegative(),
  status: z.enum(['safe', 'near_cap', 'exceeded']),
})

export const settingsApplicationSchema = z.object({
  effective_version: z.number().int().nonnegative().optional(),
})

export const settingsSnapshotSchema = z.object({
  config: settingsConfigSchema,
  env: z.record(z.string(), settingsSecretSchema),
  budget: settingsBudgetSchema,
  application: settingsApplicationSchema,
})

export const settingsResponseSchema = z.object({
  data: settingsSnapshotSchema,
  version: z.literal(1),
})

export type SettingsConfig = z.infer<typeof settingsConfigSchema>
export type SettingsSnapshot = z.infer<typeof settingsSnapshotSchema>
