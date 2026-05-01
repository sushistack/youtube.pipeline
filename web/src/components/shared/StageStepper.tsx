import {
  Check,
  Clapperboard,
  Clock3,
  FileText,
  Image,
  RotateCcw,
  UserRound,
  X,
} from 'lucide-react'
import {
  buildStageNodes,
  type DecisionsSummary,
  getStageNodeLabel,
  mapStageToNode,
  type RunStage,
  type RunStatus,
  type StageNodeKey,
} from '../../lib/formatters'
import { StageGraphView } from './StageGraphView'

// Only the four work-phase nodes are rewindable; pending and complete are
// lifecycle markers, not phases the operator re-runs.
export type RewindNodeKey = 'scenario' | 'character' | 'assets' | 'assemble'

interface StageStepperProps {
  stage: RunStage
  status: RunStatus
  variant?: 'full' | 'compact' | 'expanded'
  decisions_summary?: DecisionsSummary | null
  /**
   * When provided, completed work-phase nodes become clickable. The handler
   * is fired with the rewind target and is responsible for confirmation UI
   * + the API call. Omit to keep the stepper read-only (legacy behavior).
   */
  on_rewind_request?: (node: RewindNodeKey) => void
  /**
   * Per-node disable flag — used by the parent to mark a rewind in flight
   * (so the operator can't double-click). Independent of node state.
   */
  rewind_pending_node?: RewindNodeKey | null
}

const NODE_ICONS: Record<StageNodeKey, typeof Clock3> = {
  assemble: Clapperboard,
  assets: Image,
  character: UserRound,
  complete: Check,
  pending: Clock3,
  scenario: FileText,
}

function isRewindable(key: StageNodeKey): key is RewindNodeKey {
  return (
    key === 'scenario' ||
    key === 'character' ||
    key === 'assets' ||
    key === 'assemble'
  )
}

export function StageStepper({
  stage,
  status,
  variant = 'full',
  decisions_summary,
  on_rewind_request,
  rewind_pending_node,
}: StageStepperProps) {
  const active_node = mapStageToNode(stage)

  if (variant === 'expanded') {
    return (
      <StageGraphView
        stage={stage}
        status={status}
        decisions_summary={decisions_summary}
      />
    )
  }

  const nodes = buildStageNodes(stage, status).filter(
    (node) => node.key !== 'pending' && node.key !== 'complete',
  )

  return (
    <ol
      className="stage-stepper"
      data-variant={variant}
      aria-label={`Pipeline progress: ${getStageNodeLabel(active_node)}`}
    >
      {nodes.map((node, idx) => {
        const Icon = NODE_ICONS[node.key]
        const step_num = idx + 1
        const can_rewind =
          on_rewind_request != null &&
          node.state === 'completed' &&
          isRewindable(node.key)
        const is_pending_target =
          rewind_pending_node != null && rewind_pending_node === node.key

        if (variant === 'compact') {
          // Compact mode: icons only, no rewind affordance — too dense for
          // a click target without breaking the screen-reader contract.
          return (
            <li
              key={node.key}
              className="stage-stepper__node"
              data-state={node.state}
              aria-label={`${node.label}: ${node.state}`}
            >
              <span className="stage-stepper__icon-wrap" aria-hidden="true">
                <Icon className="stage-stepper__icon" strokeWidth={2} />
              </span>
              <span className="stage-stepper__sr-only">{node.label}</span>
            </li>
          )
        }

        const indicator = (
          <span className="stage-stepper__indicator" aria-hidden="true">
            {node.state === 'completed' ? (
              <Check className="stage-stepper__indicator-icon" strokeWidth={3} />
            ) : node.state === 'failed' ? (
              <X className="stage-stepper__indicator-icon" strokeWidth={3} />
            ) : node.state === 'active' ? (
              <span className="stage-stepper__pulse-dot" />
            ) : (
              <span className="stage-stepper__num">{step_num}</span>
            )}
          </span>
        )

        if (can_rewind && isRewindable(node.key)) {
          const target = node.key
          return (
            <li
              key={node.key}
              className="stage-stepper__node"
              data-state={node.state}
              data-rewindable="true"
              aria-label={`${node.label}: ${node.state}`}
            >
              <button
                type="button"
                className="stage-stepper__rewind-btn"
                disabled={is_pending_target}
                aria-label={`Rewind to ${node.label}`}
                aria-busy={is_pending_target}
                onClick={() => on_rewind_request!(target)}
              >
                {indicator}
                <span className="stage-stepper__label">{node.label}</span>
                <RotateCcw
                  className="stage-stepper__rewind-icon"
                  size={12}
                  aria-hidden="true"
                  strokeWidth={2.5}
                />
              </button>
            </li>
          )
        }

        return (
          <li
            key={node.key}
            className="stage-stepper__node"
            data-state={node.state}
            aria-label={`${node.label}: ${node.state}`}
          >
            {indicator}
            <span className="stage-stepper__label">{node.label}</span>
          </li>
        )
      })}
    </ol>
  )
}
