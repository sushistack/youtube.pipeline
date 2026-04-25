import { ChevronDown, ChevronUp } from 'lucide-react'
import type { RunStatusPayload, RunSummary } from '../../lib/formatters'
import { getRunSequence } from '../../lib/formatters'
import { useUIStore } from '../../stores/useUIStore'
import { StageStepper } from './StageStepper'

interface ProductionAppHeaderProps {
  run: RunSummary | null
  status_payload?: RunStatusPayload
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

export function ProductionAppHeader({
  run,
  status_payload,
}: ProductionAppHeaderProps) {
  const identity = formatRunIdentity(run)
  const expanded = useUIStore((state) => state.stage_stepper_expanded)
  const toggle_expanded = useUIStore(
    (state) => state.toggle_stage_stepper_expanded,
  )
  const variant = expanded ? 'expanded' : 'full'
  const ToggleIcon = expanded ? ChevronUp : ChevronDown

  return (
    <header
      className="production-app-header"
      aria-label="Production header"
      data-empty={String(run == null)}
      data-stepper-expanded={String(expanded)}
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
        <>
          <div className="production-app-header__stepper">
            <StageStepper
              stage={run.stage}
              status={run.status}
              variant={variant}
              decisions_summary={status_payload?.decisions_summary ?? null}
            />
          </div>
          <button
            type="button"
            className="production-app-header__toggle"
            aria-label={
              expanded
                ? 'Collapse pipeline view'
                : 'Expand pipeline view'
            }
            aria-pressed={expanded}
            onClick={toggle_expanded}
          >
            <ToggleIcon aria-hidden="true" size={18} />
          </button>
        </>
      ) : null}
    </header>
  )
}
