import type { RunStatusPayload, RunSummary } from '../../lib/formatters'
import {
  formatCurrency,
  formatFreshness,
  formatRunSummary,
  getCriticTone,
  getRunSequence,
  getStatusLabel,
  getStatusTone,
} from '../../lib/formatters'
import { StageStepper } from './StageStepper'

interface RunCardProps {
  on_select: (run_id: string) => void
  run: RunSummary
  selected: boolean
  status_payload?: RunStatusPayload
}

export function RunCard({
  on_select,
  run,
  selected,
  status_payload,
}: RunCardProps) {
  const critic_tone = getCriticTone(run.critic_score)
  const status_tone = getStatusTone(run.status)
  const run_sequence = getRunSequence(run.id)

  return (
    <div
      role="button"
      tabIndex={0}
      className="run-card"
      data-selected={String(selected)}
      data-status={run.status}
      onClick={() => {
        on_select(run.id)
      }}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          on_select(run.id)
        }
      }}
    >
      <div className="run-card__header">
        <div>
          <p className="run-card__scp">SCP-{run.scp_id}</p>
          <h3 className="run-card__title">
            Run {run_sequence ?? 'Current'}
          </h3>
        </div>
        <span className="run-card__badge" data-tone={status_tone}>
          {getStatusLabel(run.status)}
        </span>
      </div>

      <p className="run-card__summary">{formatRunSummary(run, status_payload)}</p>

      <StageStepper stage={run.stage} status={run.status} variant="compact" />

      <div className="run-card__footer">
        <span>{formatFreshness(run.updated_at)}</span>
        <span>{formatCurrency(run.cost_usd)}</span>
      </div>

      {run.critic_score != null ? (
        <div className="run-card__critic" data-tone={critic_tone}>
          <span>Critic</span>
          <strong>{Math.round(run.critic_score)}</strong>
        </div>
      ) : null}
    </div>
  )
}
