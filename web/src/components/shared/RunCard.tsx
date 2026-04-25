import type { RunSummary } from '../../lib/formatters'
import {
  getRunSequence,
  getStatusLabel,
  getStatusTone,
} from '../../lib/formatters'

interface RunCardProps {
  on_select: (run_id: string) => void
  run: RunSummary
  selected: boolean
}

function formatRunTitle(run: RunSummary) {
  const seq = getRunSequence(run.id)
  if (seq == null) {
    return `SCP-${run.scp_id}`
  }
  return `SCP-${run.scp_id} Run #${seq}`
}

export function RunCard({ on_select, run, selected }: RunCardProps) {
  const status_tone = getStatusTone(run.status)
  const title = formatRunTitle(run)
  const status_label = getStatusLabel(run.status)

  return (
    <div
      role="button"
      tabIndex={0}
      className="run-card"
      aria-label={`${title} (${status_label})`}
      data-selected={String(selected)}
      data-status={run.status}
      data-tone={status_tone}
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
      <span
        className="run-card__dot"
        aria-hidden="true"
        data-tone={status_tone}
      />
      <span className="run-card__title">{title}</span>
      <span className="stage-stepper__sr-only">{status_label}</span>
    </div>
  )
}
