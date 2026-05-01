import {
  Check,
  Clapperboard,
  Clock3,
  FileText,
  Image,
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

interface StageStepperProps {
  stage: RunStage
  status: RunStatus
  variant?: 'full' | 'compact' | 'expanded'
  decisions_summary?: DecisionsSummary | null
}

const NODE_ICONS: Record<StageNodeKey, typeof Clock3> = {
  assemble: Clapperboard,
  assets: Image,
  character: UserRound,
  complete: Check,
  pending: Clock3,
  scenario: FileText,
}

export function StageStepper({
  stage,
  status,
  variant = 'full',
  decisions_summary,
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

        if (variant === 'compact') {
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

        return (
          <li
            key={node.key}
            className="stage-stepper__node"
            data-state={node.state}
            aria-label={`${node.label}: ${node.state}`}
          >
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
            <span className="stage-stepper__label">{node.label}</span>
          </li>
        )
      })}
    </ol>
  )
}
