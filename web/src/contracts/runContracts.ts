import { z } from "zod";

export const runStageSchema = z.enum([
  "pending",
  "research",
  "structure",
  "write",
  "visual_break",
  "review",
  "critic",
  "scenario_review",
  "character_pick",
  "image",
  "tts",
  "batch_review",
  "assemble",
  "metadata_ack",
  "complete",
]);

export const runStatusSchema = z.enum([
  "pending",
  "running",
  "waiting",
  "completed",
  "failed",
  "cancelled",
]);

export const runSummarySchema = z.object({
  character_query_key: z.string().nullable().optional(),
  cost_usd: z.number().nonnegative(),
  created_at: z.string().min(1),
  critic_prompt_hash: z.string().nullable().optional(),
  critic_prompt_version: z.string().nullable().optional(),
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
});

export const runDetailSchema = runSummarySchema;
export const SCP_ID_PATTERN = /^[A-Za-z0-9_-]+$/;

export const createRunRequestSchema = z.object({
  scp_id: z.string().regex(SCP_ID_PATTERN),
});

export const createRunResponseSchema = z.object({
  data: runDetailSchema,
  error: z.null(),
  version: z.literal(1),
});

export type RunSummary = z.infer<typeof runSummarySchema>;
export type RunDetail = z.infer<typeof runDetailSchema>;

export const hitlPausedPositionSchema = z.object({
  created_at: z.string().min(1),
  last_interaction_timestamp: z.string().min(1),
  run_id: z.string().min(1),
  scene_index: z.number().int().nonnegative(),
  stage: runStageSchema,
  updated_at: z.string().min(1),
});

export const decisionsSummarySchema = z.object({
  approved_count: z.number().int().nonnegative(),
  pending_count: z.number().int().nonnegative(),
  rejected_count: z.number().int().nonnegative(),
});

export const runStatusChangeSchema = z.object({
  after: z.string().min(1).optional(),
  before: z.string().min(1).optional(),
  kind: z.enum(["scene_status_flipped", "scene_added", "scene_removed"]),
  scene_id: z.string().min(1),
  timestamp: z.string().min(1).optional(),
});

export const runStatusPayloadSchema = z.object({
  changes_since_last_interaction: z.array(runStatusChangeSchema).optional(),
  decisions_summary: decisionsSummarySchema.optional(),
  paused_position: hitlPausedPositionSchema.optional(),
  run: runDetailSchema,
  summary: z.string().min(1).optional(),
});

export const runDetailResponseSchema = z.object({
  data: runDetailSchema,
  version: z.literal(1),
});

export const runListResponseSchema = z.object({
  data: z.object({
    items: z.array(runDetailSchema),
    total: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
});

export const runResumeResponseSchema = z.object({
  data: runDetailSchema,
  version: z.literal(1),
  warnings: z.array(z.string()).optional(),
});

export const runStatusResponseSchema = z.object({
  data: runStatusPayloadSchema,
  version: z.literal(1),
});

export const sceneSchema = z.object({
  narration: z.string(),
  scene_index: z.number().int().nonnegative(),
});

export const sceneListResponseSchema = z.object({
  data: z.object({
    items: z.array(sceneSchema),
    total: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
});

export const sceneEditResponseSchema = z.object({
  data: sceneSchema,
  version: z.literal(1),
});

export const reviewItemShotSchema = z.object({
  duration_s: z.number().nonnegative().optional().default(0),
  image_path: z.string().optional().default(""),
  transition: z.string().optional().default(""),
  visual_descriptor: z.string().optional().default(""),
});

export const reviewItemPreviousVersionSchema = z.object({
  narration: z.string(),
  shots: z.array(reviewItemShotSchema),
});

export const reviewItemCriticBreakdownSchema = z.object({
  aggregate_score: z.number().min(0).max(100).nullable().optional(),
  emotional_variation: z.number().min(0).max(100).nullable().optional(),
  fact_accuracy: z.number().min(0).max(100).nullable().optional(),
  hook_strength: z.number().min(0).max(100).nullable().optional(),
  immersion: z.number().min(0).max(100).nullable().optional(),
});

export const priorRejectionWarningSchema = z.object({
  created_at: z.string().min(1),
  reason: z.string(),
  run_id: z.string().min(1),
  scene_index: z.number().int().nonnegative(),
  scp_id: z.string().min(1),
});

export const reviewItemSchema = z.object({
  clip_path: z.string().min(1).nullable().optional(),
  content_flags: z.array(z.string()).optional().default([]),
  critic_breakdown: reviewItemCriticBreakdownSchema.nullable().optional(),
  critic_score: z.number().min(0).max(100).nullable().optional(),
  high_leverage: z.boolean(),
  high_leverage_reason: z.string().nullable().optional(),
  high_leverage_reason_code: z.string().nullable().optional(),
  narration: z.string(),
  previous_version: reviewItemPreviousVersionSchema.nullable().optional(),
  prior_rejection: priorRejectionWarningSchema.nullable().optional(),
  regen_attempts: z.number().int().nonnegative().optional().default(0),
  retry_exhausted: z.boolean().optional().default(false),
  review_status: z.enum([
    "pending",
    "waiting_for_review",
    "auto_approved",
    "approved",
    "rejected",
  ]),
  scene_index: z.number().int().nonnegative(),
  shots: z.array(reviewItemShotSchema),
  tts_duration_ms: z.number().int().nonnegative().nullable().optional(),
  tts_path: z.string().min(1).nullable().optional(),
});

export const reviewItemListResponseSchema = z.object({
  data: z.object({
    items: z.array(reviewItemSchema),
    total: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
});

export const timelineDecisionTypeValues = [
  "approve",
  "reject",
  "skip_and_remember",
  "descriptor_edit",
  "undo",
  "system_auto_approved",
  "override",
] as const;

export const timelineDecisionTypeSchema = z.enum(timelineDecisionTypeValues);

export const timelineDecisionFilterValues = [
  "all",
  ...timelineDecisionTypeValues,
] as const;

export const timelineCursorSchema = z.object({
  before_created_at: z.string().min(1),
  before_id: z.number().int().positive(),
});

export const timelineDecisionSchema = z.object({
  created_at: z.string().min(1),
  decision_type: timelineDecisionTypeSchema,
  id: z.number().int().positive(),
  note: z.string().nullable(),
  reason_from_snapshot: z.string().nullable(),
  run_id: z.string().min(1),
  scene_id: z.string().nullable(),
  scp_id: z.string().min(1),
  superseded_by: z.number().int().positive().nullable(),
});

export const timelineListResponseSchema = z.object({
  data: z.object({
    items: z.array(timelineDecisionSchema),
    next_cursor: timelineCursorSchema.nullable(),
  }),
  version: z.literal(1),
});

export const sceneDecisionTypeSchema = z.enum([
  "approve",
  "reject",
  "skip_and_remember",
]);

export const sceneDecisionRequestSchema = z.object({
  context_snapshot: z.object({}).catchall(z.unknown()).optional(),
  decision_type: sceneDecisionTypeSchema,
  note: z.string().nullable().optional(),
  scene_index: z.number().int().nonnegative(),
});

export const sceneDecisionResponseSchema = z.object({
  data: z.object({
    decision_type: sceneDecisionTypeSchema,
    next_scene_index: z.number().int().nonnegative(),
    prior_rejection: priorRejectionWarningSchema.nullable().optional(),
    regen_attempts: z.number().int().nonnegative().optional().default(0),
    retry_exhausted: z.boolean().optional().default(false),
    scene_index: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
});

export const batchApproveAllRemainingResponseSchema = z.object({
  data: z.object({
    // Empty string is the server's signal that no target scenes existed and
    // no decision rows were inserted; the client uses this to skip pushing a
    // phantom undo entry for a batch that committed nothing.
    aggregate_command_id: z.string(),
    approved_count: z.number().int().nonnegative(),
    approved_scene_indices: z.array(z.number().int().nonnegative()),
    focus_scene_index: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
});

export const sceneRegenResponseSchema = z.object({
  data: z.object({
    regen_attempts: z.number().int().nonnegative(),
    retry_exhausted: z.boolean(),
    scene_index: z.number().int().nonnegative(),
  }),
  version: z.literal(1),
});

export const characterCandidateSchema = z.object({
  id: z.string().min(1),
  image_url: z.string().min(1),
  page_url: z.string().min(1),
  preview_url: z.string().min(1).nullable().optional(),
  source_label: z.string().min(1).nullable().optional(),
  title: z.string().min(1).nullable().optional(),
});

export const characterGroupSchema = z.object({
  candidates: z.array(characterCandidateSchema),
  query: z.string().min(1),
  query_key: z.string().min(1),
});

export const characterGroupResponseSchema = z.object({
  data: characterGroupSchema,
  version: z.literal(1),
});

export const descriptorPrefillSchema = z.object({
  auto: z.string(),
  prior: z.string().nullable(),
});

export const descriptorPrefillResponseSchema = z.object({
  data: descriptorPrefillSchema,
  version: z.literal(1),
});

export const undoFocusTargetSchema = z.enum(["scene-card", "descriptor"]);

export const undoResponseSchema = z.object({
  data: z.object({
    focus_target: undoFocusTargetSchema,
    undone_kind: z.string().min(1),
    undone_scene_index: z.number().int(),
  }),
  version: z.literal(1),
});

export type UndoResponseData = z.infer<typeof undoResponseSchema>["data"];

export type Scene = z.infer<typeof sceneSchema>;
export type ReviewItem = z.infer<typeof reviewItemSchema>;
export type TimelineCursor = z.infer<typeof timelineCursorSchema>;
export type TimelineDecision = z.infer<typeof timelineDecisionSchema>;
export type CharacterCandidate = z.infer<typeof characterCandidateSchema>;
export type CharacterGroup = z.infer<typeof characterGroupSchema>;
export type DescriptorPrefill = z.infer<typeof descriptorPrefillSchema>;
export type RunStatusChange = z.infer<typeof runStatusChangeSchema>;
export type SceneDecisionRequest = z.infer<typeof sceneDecisionRequestSchema>;
export type SceneDecisionResponse = z.infer<
  typeof sceneDecisionResponseSchema
>["data"];
export type BatchApproveAllRemainingResponse = z.infer<
  typeof batchApproveAllRemainingResponseSchema
>["data"];
export type PriorRejectionWarning = z.infer<typeof priorRejectionWarningSchema>;
export type SceneRegenResponse = z.infer<
  typeof sceneRegenResponseSchema
>["data"];
export type TimelineListResponse = z.infer<
  typeof timelineListResponseSchema
>["data"];

// --- Story 9.4: Compliance gate schemas ---

export const aiGeneratedFlagsSchema = z.object({
  narration: z.boolean(),
  imagery: z.boolean(),
  tts: z.boolean(),
});

export const modelRecordSchema = z.object({
  provider: z.string().min(1),
  model: z.string().min(1),
  voice: z.string().optional(),
});

export const metadataBundleSchema = z.object({
  version: z.number().int().nonnegative(),
  generated_at: z.string().min(1),
  run_id: z.string().min(1),
  scp_id: z.string().min(1),
  title: z.string().min(1),
  ai_generated: aiGeneratedFlagsSchema,
  models_used: z.record(z.string(), modelRecordSchema),
});

// author_name / license_url can be empty for orphan works, CC0, or some
// public-domain sources. The UI is already defensive (`?? "—"`), so parsing
// must accept "". Core compliance fields (component, source_url, license)
// stay strict — the gate is pointless without them.
export const licenseEntrySchema = z.object({
  component: z.string().min(1),
  source_url: z.string().min(1),
  author_name: z.string(),
  license: z.string().min(1),
});

export const sourceManifestSchema = z.object({
  version: z.number().int().nonnegative(),
  generated_at: z.string().min(1),
  run_id: z.string().min(1),
  scp_id: z.string().min(1),
  source_url: z.string().min(1),
  author_name: z.string(),
  license: z.string().min(1),
  license_url: z.string(),
  license_chain: z.array(licenseEntrySchema),
});

export type MetadataBundle = z.infer<typeof metadataBundleSchema>;
export type SourceManifest = z.infer<typeof sourceManifestSchema>;
