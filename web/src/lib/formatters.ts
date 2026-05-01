import type { z } from 'zod'
import {
  runStageSchema,
  runStatusChangeSchema,
  runStatusPayloadSchema,
  runStatusSchema,
  runSummarySchema,
} from '../contracts/runContracts'

export type RunStage = z.infer<typeof runStageSchema>
export type RunStatus = z.infer<typeof runStatusSchema>
export type RunStatusChange = z.infer<typeof runStatusChangeSchema>
export type RunSummary = z.infer<typeof runSummarySchema>
export type RunStatusPayload = z.infer<typeof runStatusPayloadSchema>

export type StageNodeKey =
  | 'pending'
  | 'scenario'
  | 'character'
  | 'assets'
  | 'assemble'
  | 'complete'

export type StageNodeState = 'completed' | 'active' | 'upcoming' | 'failed'

export interface StageNodeModel {
  key: StageNodeKey
  label: string
  state: StageNodeState
}

export interface StageNodeCounter {
  done: number
  total: number
  suffix: string
}

export interface StageDagNodeModel {
  id: RunStage
  label: string
  state: StageNodeState
  parent: StageNodeKey
  rel_x: number
  rel_y: number
  counter?: StageNodeCounter
}

export interface StageDagLaneModel {
  id: StageNodeKey
  label: string
  state: StageNodeState
  x: number
  width: number
  height: number
}

export type StageDagEdgeState = 'completed' | 'active' | 'upcoming' | 'failed'

export interface StageDagEdgeModel {
  id: string
  source: RunStage
  target: RunStage
  state: StageDagEdgeState
}

export interface StageDag {
  lanes: StageDagLaneModel[]
  nodes: StageDagNodeModel[]
  edges: StageDagEdgeModel[]
  canvas_width: number
  canvas_height: number
}

const STAGE_LABELS: Record<RunStage, string> = {
  assemble: 'Assemble',
  batch_review: 'Asset review',
  character_pick: 'Character pick',
  complete: 'Complete',
  critic: 'Critic pass',
  image: 'Image generation',
  metadata_ack: 'Metadata check',
  pending: 'Queued',
  research: 'Research',
  review: 'Review',
  scenario_review: 'Scenario review',
  structure: 'Structure',
  tts: 'Voice render',
  visual_break: 'Shot planning',
  write: 'Script writing',
}

const STATUS_LABELS: Record<RunStatus, string> = {
  cancelled: 'Cancelled',
  completed: 'Completed',
  failed: 'Failed',
  pending: 'Pending',
  running: 'Running',
  waiting: 'Waiting',
}

const STAGE_TO_NODE: Record<RunStage, StageNodeKey> = {
  assemble: 'assemble',
  batch_review: 'assets',
  character_pick: 'character',
  complete: 'complete',
  critic: 'scenario',
  image: 'assets',
  metadata_ack: 'assemble',
  pending: 'pending',
  research: 'scenario',
  review: 'scenario',
  scenario_review: 'scenario',
  structure: 'scenario',
  tts: 'assets',
  visual_break: 'scenario',
  write: 'scenario',
}

const STAGE_NODE_LABELS: Record<StageNodeKey, string> = {
  assemble: 'Cut',
  assets: 'Media',
  character: 'Cast',
  complete: 'Complete',
  pending: 'Pending',
  scenario: 'Story',
}

const NODE_ORDER: StageNodeKey[] = [
  'pending',
  'scenario',
  'character',
  'assets',
  'assemble',
  'complete',
]

export function mapStageToNode(stage: RunStage): StageNodeKey {
  return STAGE_TO_NODE[stage]
}

export function getStageLabel(stage: RunStage) {
  return STAGE_LABELS[stage]
}

export function getStageNodeLabel(node: StageNodeKey) {
  return STAGE_NODE_LABELS[node]
}

export function getStatusLabel(status: RunStatus) {
  return STATUS_LABELS[status]
}

export function isRunLive(status: RunStatus) {
  return status === 'running' || status === 'waiting'
}

export function isRunPollable(status: RunStatus) {
  return isRunLive(status) || status === 'pending'
}

export function formatCurrency(cost_usd: number) {
  return new Intl.NumberFormat('en-US', {
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
    style: 'currency',
  }).format(cost_usd)
}

export function formatElapsed(duration_ms: number) {
  const total_seconds = Math.max(0, Math.floor(duration_ms / 1000))
  const days = Math.floor(total_seconds / 86400)
  const hours = Math.floor((total_seconds % 86400) / 3600)
  const minutes = Math.floor((total_seconds % 3600) / 60)
  const seconds = total_seconds % 60

  if (days > 0) {
    return `${days}d ${hours}h`
  }

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
  }

  return `${minutes}:${String(seconds).padStart(2, '0')}`
}

export function formatTokenCount(tokens: number) {
  return new Intl.NumberFormat('en-US').format(tokens)
}

export function formatFreshness(timestamp: string) {
  const parsed_ms = new Date(timestamp).getTime()
  if (!Number.isFinite(parsed_ms)) {
    return 'Updated recently'
  }

  const delta_ms = Math.max(0, Date.now() - parsed_ms)
  const delta_minutes = Math.round(delta_ms / 60000)

  if (delta_minutes < 1) {
    return 'Updated just now'
  }

  if (delta_minutes < 60) {
    return `Updated ${delta_minutes}m ago`
  }

  const delta_hours = Math.round(delta_minutes / 60)
  if (delta_hours < 24) {
    return `Updated ${delta_hours}h ago`
  }

  const delta_days = Math.round(delta_hours / 24)
  return `Updated ${delta_days}d ago`
}

export function formatRunSummary(run: RunSummary, status_payload?: RunStatusPayload) {
  if (status_payload?.summary) {
    return status_payload.summary
  }

  if (run.status === 'completed') {
    return `${getStageLabel(run.stage)} finished successfully`
  }

  if (run.status === 'failed') {
    return `${getStageLabel(run.stage)} needs operator attention`
  }

  if (run.status === 'waiting') {
    return `${getStageLabel(run.stage)} is waiting for review`
  }

  if (run.status === 'running') {
    return `${getStageLabel(run.stage)} is actively processing`
  }

  return `${getStatusLabel(run.status)} in ${getStageLabel(run.stage)}`
}

function formatSceneLabel(scene_id: string) {
  return `Scene ${scene_id}`
}

function formatContinuityChange(change: RunStatusChange) {
  switch (change.kind) {
    case 'scene_status_flipped':
      if (change.before && change.after && change.before !== change.after) {
        return `${formatSceneLabel(change.scene_id)} moved from ${change.before} to ${change.after}`
      }

      if (change.after) {
        return `${formatSceneLabel(change.scene_id)} is now ${change.after}`
      }

      if (change.before) {
        return `${formatSceneLabel(change.scene_id)} no longer ${change.before}`
      }

      return `${formatSceneLabel(change.scene_id)} changed status`
    case 'scene_added':
      return `${formatSceneLabel(change.scene_id)} appeared in review`
    case 'scene_removed':
      return `${formatSceneLabel(change.scene_id)} was removed from the current review set`
  }
}

export function formatContinuityMessage(
  status_payload: Pick<RunStatusPayload, 'changes_since_last_interaction' | 'summary'>,
) {
  const changes = status_payload.changes_since_last_interaction ?? []

  if (changes.length > 0) {
    const [first_change] = changes
    const suffix =
      changes.length > 1 ? ` (+${changes.length - 1} more updates)` : ''

    return `${formatContinuityChange(first_change)}${suffix}`
  }

  const summary = status_payload.summary?.trim()
  if (!summary) {
    return null
  }

  return summary
}

export function getCriticTone(score?: number | null) {
  if (score == null) {
    return 'none' as const
  }

  if (score >= 80) {
    return 'high' as const
  }

  if (score >= 50) {
    return 'medium' as const
  }

  return 'low' as const
}

export function getStatusTone(status: RunStatus) {
  switch (status) {
    case 'completed':
      return 'success' as const
    case 'failed':
    case 'cancelled':
      return 'warning' as const
    case 'running':
      return 'accent' as const
    case 'waiting':
      return 'muted' as const
    default:
      return 'subtle' as const
  }
}

export function getRunSequence(run_id: string): number | string | null {
  const parts = run_id.match(/-run-(.+)$/)
  if (!parts) {
    return null
  }
  const raw = parts[1]
  const as_number = Number(raw)
  return Number.isInteger(as_number) && as_number >= 0 ? as_number : raw
}

export function compareRunsForInventory(a: RunSummary, b: RunSummary) {
  const priority = (run: RunSummary) => {
    if (run.status === 'running') {
      return 0
    }
    if (run.status === 'waiting') {
      return 1
    }
    if (run.status === 'failed') {
      return 2
    }
    if (run.status === 'pending') {
      return 3
    }
    return 4
  }

  const priority_delta = priority(a) - priority(b)
  if (priority_delta !== 0) {
    return priority_delta
  }

  return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
}

export function buildStageNodes(
  stage: RunStage,
  status: RunStatus,
): StageNodeModel[] {
  const active_key = mapStageToNode(stage)
  const active_index = NODE_ORDER.indexOf(active_key)

  return NODE_ORDER.map((key, index) => {
    let state: StageNodeState = 'upcoming'

    if (status === 'completed') {
      state = 'completed'
    } else if ((status === 'failed' || status === 'cancelled') && index === active_index) {
      state = 'failed'
    } else if (index < active_index) {
      state = 'completed'
    } else if (index === active_index) {
      state = 'active'
    }

    return {
      key,
      label: STAGE_NODE_LABELS[key],
      state,
    }
  })
}

export interface DecisionsSummary {
  approved_count: number
  rejected_count: number
  pending_count: number
}


const LANE_ORDER: StageNodeKey[] = [
  'pending',
  'scenario',
  'character',
  'assets',
  'assemble',
  'complete',
]

const LANE_NODES: Record<StageNodeKey, RunStage[]> = {
  pending: ['pending'],
  scenario: [
    'research',
    'structure',
    'write',
    'visual_break',
    'review',
    'critic',
    'scenario_review',
  ],
  character: ['character_pick'],
  assets: ['image', 'tts', 'batch_review'],
  assemble: ['assemble', 'metadata_ack'],
  complete: ['complete'],
}

const STAGE_PROGRESS_LEVEL: Record<RunStage, number> = (() => {
  const level: Partial<Record<RunStage, number>> = {}
  let counter = 0
  for (const lane_key of LANE_ORDER) {
    for (const stage of LANE_NODES[lane_key]) {
      if (stage === 'tts') {
        level.tts = level.image
        continue
      }
      level[stage] = counter
      counter += 1
    }
  }
  return level as Record<RunStage, number>
})()

const NODE_WIDTH = 110
const NODE_HEIGHT = 32
const NODE_VGAP = 8
const LANE_PADDING_X = 12
const LANE_HEADER_HEIGHT = 28
const LANE_GAP = 16

function computeLaneWidth(lane_key: StageNodeKey): number {
  if (lane_key === 'assets') {
    return LANE_PADDING_X * 2 + NODE_WIDTH * 2 + LANE_GAP
  }
  return LANE_PADDING_X * 2 + NODE_WIDTH
}

function computeChildrenLayout(
  lane_key: StageNodeKey,
): Map<RunStage, { rel_x: number; rel_y: number }> {
  const layout = new Map<RunStage, { rel_x: number; rel_y: number }>()
  const stages = LANE_NODES[lane_key]

  if (lane_key === 'assets') {
    // image and tts as parallel rails in the LEFT column (vertically stacked,
    // both fed from character_pick); batch_review in the RIGHT column,
    // vertically centered between them as the merge target. This avoids the
    // character_pick→tts edge slicing through the image node.
    layout.set('image', {
      rel_x: LANE_PADDING_X,
      rel_y: LANE_HEADER_HEIGHT,
    })
    layout.set('tts', {
      rel_x: LANE_PADDING_X,
      rel_y: LANE_HEADER_HEIGHT + NODE_HEIGHT + NODE_VGAP,
    })
    layout.set('batch_review', {
      rel_x: LANE_PADDING_X + NODE_WIDTH + LANE_GAP,
      rel_y: LANE_HEADER_HEIGHT + (NODE_HEIGHT + NODE_VGAP) / 2,
    })
    return layout
  }

  stages.forEach((stage, idx) => {
    layout.set(stage, {
      rel_x: LANE_PADDING_X,
      rel_y: LANE_HEADER_HEIGHT + idx * (NODE_HEIGHT + NODE_VGAP),
    })
  })
  return layout
}

export function buildStageDagTopology(
  stage: RunStage,
  status: RunStatus,
  decisions_summary?: DecisionsSummary | null,
): StageDag {
  const current_level = STAGE_PROGRESS_LEVEL[stage]
  const is_terminal_failed = status === 'failed' || status === 'cancelled'
  const is_completed = status === 'completed'
  const parallel_branch: ReadonlySet<RunStage> = new Set(['image', 'tts'])

  function deriveStageState(node_stage: RunStage): StageNodeState {
    const level = STAGE_PROGRESS_LEVEL[node_stage]
    if (is_completed) {
      return 'completed'
    }
    if (level < current_level) {
      return 'completed'
    }
    if (level === current_level) {
      const is_current =
        node_stage === stage ||
        (parallel_branch.has(stage) && parallel_branch.has(node_stage))
      if (!is_current) {
        return 'upcoming'
      }
      return is_terminal_failed ? 'failed' : 'active'
    }
    return 'upcoming'
  }

  function deriveLaneState(lane_key: StageNodeKey): StageNodeState {
    const stages = LANE_NODES[lane_key]
    const states = stages.map(deriveStageState)
    if (states.includes('failed')) return 'failed'
    if (states.includes('active')) return 'active'
    if (states.every((s) => s === 'completed')) return 'completed'
    return 'upcoming'
  }

  // Lane positions (LR)
  let cursor_x = 0
  const lane_widths: Record<StageNodeKey, number> = {} as Record<
    StageNodeKey,
    number
  >
  const lane_positions: Record<StageNodeKey, number> = {} as Record<
    StageNodeKey,
    number
  >
  for (const lane_key of LANE_ORDER) {
    const w = computeLaneWidth(lane_key)
    lane_widths[lane_key] = w
    lane_positions[lane_key] = cursor_x
    cursor_x += w + LANE_GAP
  }
  const canvas_width = cursor_x - LANE_GAP

  // Lane height = max content height, applied uniformly for visual alignment
  const lane_content_heights: number[] = LANE_ORDER.map((key) => {
    const child_count = LANE_NODES[key].length
    const rows = key === 'assets' ? 2 : child_count
    return (
      LANE_HEADER_HEIGHT + rows * NODE_HEIGHT + (rows - 1) * NODE_VGAP + 12
    )
  })
  const lane_height = Math.max(...lane_content_heights)
  const canvas_height = lane_height

  const lanes: StageDagLaneModel[] = LANE_ORDER.map((lane_key) => ({
    id: lane_key,
    label: STAGE_NODE_LABELS[lane_key],
    state: deriveLaneState(lane_key),
    x: lane_positions[lane_key],
    width: lane_widths[lane_key],
    height: lane_height,
  }))

  const nodes: StageDagNodeModel[] = []
  for (const lane_key of LANE_ORDER) {
    const child_layout = computeChildrenLayout(lane_key)
    for (const stage_id of LANE_NODES[lane_key]) {
      const pos = child_layout.get(stage_id)
      if (!pos) continue
      const node: StageDagNodeModel = {
        id: stage_id,
        label: STAGE_LABELS[stage_id],
        state: deriveStageState(stage_id),
        parent: lane_key,
        rel_x: pos.rel_x,
        rel_y: pos.rel_y,
      }
      if (
        stage_id === 'batch_review' &&
        stage === 'batch_review' &&
        decisions_summary
      ) {
        const done =
          decisions_summary.approved_count + decisions_summary.rejected_count
        const total = done + decisions_summary.pending_count
        if (total > 0) {
          node.counter = { done, total, suffix: 'reviewed' }
        }
      }
      nodes.push(node)
    }
  }

  const node_state = new Map(nodes.map((n) => [n.id, n.state]))

  function deriveEdgeState(
    source: RunStage,
    target: RunStage,
  ): StageDagEdgeState {
    const s = node_state.get(source)
    const t = node_state.get(target)
    if (s === 'completed' && t === 'completed') return 'completed'
    if (s === 'failed' || t === 'failed') return 'failed'
    if (s === 'active' || t === 'active') return 'active'
    return 'upcoming'
  }

  const edge_pairs: Array<[RunStage, RunStage]> = [
    ['pending', 'research'],
    ['research', 'structure'],
    ['structure', 'write'],
    ['write', 'visual_break'],
    ['visual_break', 'review'],
    ['review', 'critic'],
    ['critic', 'scenario_review'],
    ['scenario_review', 'character_pick'],
    ['character_pick', 'image'],
    ['character_pick', 'tts'],
    ['image', 'batch_review'],
    ['tts', 'batch_review'],
    ['batch_review', 'assemble'],
    ['assemble', 'metadata_ack'],
    ['metadata_ack', 'complete'],
  ]

  const edges: StageDagEdgeModel[] = edge_pairs.map(([source, target]) => ({
    id: `${source}__${target}`,
    source,
    target,
    state: deriveEdgeState(source, target),
  }))

  return { lanes, nodes, edges, canvas_width, canvas_height }
}
