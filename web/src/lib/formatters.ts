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
  counter?: StageNodeCounter
}

export type StageDagEdgeState = 'completed' | 'active' | 'upcoming' | 'failed'

export interface StageDagEdgeModel {
  id: string
  source: RunStage
  target: RunStage
  state: StageDagEdgeState
}

export interface StageDag {
  nodes: StageDagNodeModel[]
  edges: StageDagEdgeModel[]
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
  assemble: 'Assemble',
  assets: 'Assets',
  character: 'Character',
  complete: 'Complete',
  pending: 'Pending',
  scenario: 'Scenario',
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

const DAG_LINEAR_HEAD: RunStage[] = [
  'pending',
  'research',
  'structure',
  'write',
  'visual_break',
  'review',
  'critic',
  'scenario_review',
  'character_pick',
]

const DAG_LINEAR_TAIL: RunStage[] = [
  'batch_review',
  'assemble',
  'metadata_ack',
  'complete',
]

const DAG_PARALLEL_BRANCH: RunStage[] = ['image', 'tts']

const STAGE_TO_PARENT_NODE: Record<RunStage, StageNodeKey> = STAGE_TO_NODE

function progressIndex(stage: RunStage): number {
  if (stage === 'image' || stage === 'tts') {
    return DAG_LINEAR_HEAD.length
  }
  const head = DAG_LINEAR_HEAD.indexOf(stage)
  if (head !== -1) {
    return head
  }
  const tail = DAG_LINEAR_TAIL.indexOf(stage)
  if (tail !== -1) {
    return DAG_LINEAR_HEAD.length + 1 + tail
  }
  return -1
}

export function buildStageDagTopology(
  stage: RunStage,
  status: RunStatus,
  decisions_summary?: DecisionsSummary | null,
): StageDag {
  const current_index = progressIndex(stage)
  const is_terminal_failed = status === 'failed' || status === 'cancelled'
  const is_completed = status === 'completed'

  function deriveState(node_stage: RunStage): StageNodeState {
    const idx = progressIndex(node_stage)
    if (is_completed) {
      return 'completed'
    }
    if (idx < current_index) {
      return 'completed'
    }
    if (idx === current_index) {
      const is_current_node =
        node_stage === stage ||
        (DAG_PARALLEL_BRANCH.includes(stage) &&
          DAG_PARALLEL_BRANCH.includes(node_stage))
      if (!is_current_node) {
        return 'upcoming'
      }
      return is_terminal_failed ? 'failed' : 'active'
    }
    return 'upcoming'
  }

  const all_nodes: RunStage[] = [
    ...DAG_LINEAR_HEAD,
    ...DAG_PARALLEL_BRANCH,
    ...DAG_LINEAR_TAIL,
  ]

  const nodes: StageDagNodeModel[] = all_nodes.map((node_stage) => {
    const node: StageDagNodeModel = {
      id: node_stage,
      label: STAGE_LABELS[node_stage],
      state: deriveState(node_stage),
      parent: STAGE_TO_PARENT_NODE[node_stage],
    }
    if (
      node_stage === 'batch_review' &&
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
    return node
  })

  const node_state = new Map(nodes.map((n) => [n.id, n.state]))

  function deriveEdgeState(source: RunStage, target: RunStage): StageDagEdgeState {
    const s = node_state.get(source)
    const t = node_state.get(target)
    if (s === 'completed' && t === 'completed') {
      return 'completed'
    }
    if (t === 'failed' || s === 'failed') {
      return 'failed'
    }
    if (t === 'active' || s === 'active') {
      return 'active'
    }
    return 'upcoming'
  }

  const edges: StageDagEdgeModel[] = []

  for (let i = 0; i < DAG_LINEAR_HEAD.length - 1; i += 1) {
    const source = DAG_LINEAR_HEAD[i]
    const target = DAG_LINEAR_HEAD[i + 1]
    edges.push({
      id: `${source}__${target}`,
      source,
      target,
      state: deriveEdgeState(source, target),
    })
  }

  const fork_source = DAG_LINEAR_HEAD[DAG_LINEAR_HEAD.length - 1]
  const merge_target = DAG_LINEAR_TAIL[0]
  for (const branch of DAG_PARALLEL_BRANCH) {
    edges.push({
      id: `${fork_source}__${branch}`,
      source: fork_source,
      target: branch,
      state: deriveEdgeState(fork_source, branch),
    })
    edges.push({
      id: `${branch}__${merge_target}`,
      source: branch,
      target: merge_target,
      state: deriveEdgeState(branch, merge_target),
    })
  }

  for (let i = 0; i < DAG_LINEAR_TAIL.length - 1; i += 1) {
    const source = DAG_LINEAR_TAIL[i]
    const target = DAG_LINEAR_TAIL[i + 1]
    edges.push({
      id: `${source}__${target}`,
      source,
      target,
      state: deriveEdgeState(source, target),
    })
  }

  return { nodes, edges }
}
