import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CircleStop } from 'lucide-react'
import type { RunSummary } from '../../lib/formatters'
import {
  getRunSequence,
  getStatusLabel,
  getStatusTone,
} from '../../lib/formatters'
import { cancelRun } from '../../lib/apiClient'
import { queryKeys } from '../../lib/queryKeys'

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

function isCancellable(status: RunSummary['status']) {
  return status === 'running' || status === 'waiting'
}

export function RunCard({ on_select, run, selected }: RunCardProps) {
  const status_tone = getStatusTone(run.status)
  const title = formatRunTitle(run)
  const status_label = getStatusLabel(run.status)
  const cancellable = isCancellable(run.status)
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
  const cancel_pending =
    cancel_mutation.isPending && cancel_mutation.variables === run.id

  function handleCancel(e: React.MouseEvent) {
    e.stopPropagation()
    if (cancel_pending) return
    const ok = window.confirm(
      `Cancel ${title}? The pipeline will stop and the run will be marked cancelled.`,
    )
    if (!ok) return
    cancel_mutation.mutate(run.id)
  }

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
        title={status_label}
      />
      <span className="run-card__title">{title}</span>
      {cancellable ? (
        <button
          type="button"
          className="run-card__cancel"
          disabled={cancel_pending}
          onClick={handleCancel}
          aria-label={cancel_pending ? 'Cancelling…' : `Cancel ${title}`}
        >
          <CircleStop size={14} aria-hidden="true" />
        </button>
      ) : null}
    </div>
  )
}
