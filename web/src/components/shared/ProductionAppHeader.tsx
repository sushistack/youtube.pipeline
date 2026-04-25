import type { RunSummary } from '../../lib/formatters'
import { getRunSequence } from '../../lib/formatters'
import { StageStepper } from './StageStepper'

interface ProductionAppHeaderProps {
  run: RunSummary | null
}

function formatRunIdentity(run: RunSummary | null) {
  if (!run) {
    return null
  }
  const seq = getRunSequence(run.id)
  if (seq == null) {
    return run.id
  }
  return `SCP-${run.scp_id} Run #${seq}`
}

export function ProductionAppHeader({ run }: ProductionAppHeaderProps) {
  const identity = formatRunIdentity(run)

  return (
    <header
      className="production-app-header"
      aria-label="Production header"
      data-empty={String(run == null)}
    >
      <div className="production-app-header__identity">
        {identity ? (
          <h2 className="production-app-header__run-id">{identity}</h2>
        ) : (
          <h2 className="production-app-header__run-id production-app-header__run-id--empty">
            No run selected
          </h2>
        )}
      </div>
      {run ? (
        <div className="production-app-header__stepper">
          <StageStepper stage={run.stage} status={run.status} variant="full" />
        </div>
      ) : null}
    </header>
  )
}
