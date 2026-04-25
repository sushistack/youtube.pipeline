import {
  Check,
  Clapperboard,
  Clock3,
  FileText,
  Image,
  UserRound,
} from 'lucide-react'
import {
  buildStageGraph,
  buildStageNodes,
  type DecisionsSummary,
  getStageNodeLabel,
  mapStageToNode,
  type RunStage,
  type RunStatus,
  type StageNodeKey,
  type SubStageNodeModel,
} from '../../lib/formatters'

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
    const { nodes, sub_nodes } = buildStageGraph(stage, status, decisions_summary)

    return (
      <div className="stage-stepper" data-variant="expanded">
        <ol
          className="stage-stepper__nodes"
          aria-label={`Pipeline progress: ${getStageNodeLabel(active_node)}`}
        >
          {nodes.map((node) => {
            const Icon = NODE_ICONS[node.key]
            const rail = sub_nodes[node.key]

            return (
              <li
                key={node.key}
                className="stage-stepper__column"
                data-node={node.key}
              >
                <div
                  className="stage-stepper__node"
                  data-state={node.state}
                  aria-label={`${node.label}: ${node.state}`}
                >
                  <span className="stage-stepper__icon-wrap" aria-hidden="true">
                    <Icon className="stage-stepper__icon" strokeWidth={2} />
                  </span>
                  <span className="stage-stepper__label">{node.label}</span>
                </div>
                {rail && rail.length > 0 ? (
                  <ol
                    className="stage-stepper__rail"
                    aria-label={`${node.label} sub-stages`}
                  >
                    {rail.map((sub) => (
                      <SubNode key={sub.stage} sub={sub} />
                    ))}
                  </ol>
                ) : null}
              </li>
            )
          })}
        </ol>
      </div>
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

function SubNode({ sub }: { sub: SubStageNodeModel }) {
  return (
    <li
      className="stage-stepper__sub-node"
      data-state={sub.state}
      data-stage={sub.stage}
      aria-label={`${sub.label}: ${sub.state}`}
    >
      <span className="stage-stepper__sub-dot" aria-hidden="true" />
      <span className="stage-stepper__sub-label">{sub.label}</span>
      {sub.counter ? (
        <span className="stage-stepper__sub-counter">
          {sub.counter.done}/{sub.counter.total} {sub.counter.suffix}
        </span>
      ) : null}
    </li>
  )
}
