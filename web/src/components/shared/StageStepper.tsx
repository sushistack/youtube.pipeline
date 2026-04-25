import {
  Check,
  Clapperboard,
  Clock3,
  FileText,
  Image,
  UserRound,
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

  const nodes = buildStageNodes(stage, status)

  return (
    <ol
      className="stage-stepper"
      data-variant={variant}
      aria-label={`Pipeline progress: ${getStageNodeLabel(active_node)}`}
    >
      {nodes.map((node) => {
        const Icon = NODE_ICONS[node.key]

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
            {variant === 'full' ? (
              <span className="stage-stepper__label">{node.label}</span>
            ) : (
              <span className="stage-stepper__sr-only">{node.label}</span>
            )}
          </li>
        )
      })}
    </ol>
  )
}
