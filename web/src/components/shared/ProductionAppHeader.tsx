import { ChevronDown, ChevronUp } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { RunStatusPayload, RunSummary } from '../../lib/formatters'
import { getRunSequence } from '../../lib/formatters'
import { cancelRun } from '../../lib/apiClient'
import { queryKeys } from '../../lib/queryKeys'
import { useUIStore } from '../../stores/useUIStore'
import { StageStepper } from './StageStepper'

interface ProductionAppHeaderProps {
  run: RunSummary | null
  status_payload?: RunStatusPayload
}

// A run is cancellable while the pipeline workers still hold it (running) or
// while it is paused waiting on operator input. Pending and terminal states
// (completed/failed/cancelled) reach Cancel via different surfaces — pending
// uses the Start button, failed uses the FailureBanner Resume.
function isCancellable(status: RunSummary['status']) {
  return status === 'running' || status === 'waiting'
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
  const query_client = useQueryClient()
  const cancel_mutation = useMutation({
    mutationFn: (run_id: string) => cancelRun(run_id),
    onSuccess: (_data, run_id) => {
      void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run_id),
      })
    },
  })
  const cancellable = run != null && isCancellable(run.status)
  const cancel_pending =
    cancel_mutation.isPending && cancel_mutation.variables === run?.id

  function handleCancel() {
    if (!run) {
      return
    }
    if (cancel_pending) {
      return
    }
    const ok = window.confirm(
      `Cancel ${identity ?? run.id}? The pipeline will stop and the run will be marked cancelled.`,
    )
    if (!ok) {
      return
    }
    cancel_mutation.mutate(run.id)
  }

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
          <div className="production-app-header__actions">
            {cancellable ? (
              <button
                type="button"
                className="production-app-header__cancel"
                disabled={cancel_pending}
                onClick={handleCancel}
              >
                {cancel_pending ? 'Cancelling…' : 'Cancel run'}
              </button>
            ) : null}
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
          </div>
        </>
      ) : null}
    </header>
  )
}
