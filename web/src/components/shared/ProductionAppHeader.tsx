import { useState } from 'react'
import { ChevronDown, ChevronUp, CircleStop } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { RunStatusPayload, RunSummary } from '../../lib/formatters'
import { getRunSequence } from '../../lib/formatters'
import { cancelRun, rewindRun } from '../../lib/apiClient'
import { queryKeys } from '../../lib/queryKeys'
import { useUIStore } from '../../stores/useUIStore'
import { StageStepper, type RewindNodeKey } from './StageStepper'

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

const REWIND_LABELS: Record<RewindNodeKey, string> = {
  scenario: 'Story',
  character: 'Cast',
  assets: 'Media',
  assemble: 'Cut',
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
  const [rewind_pending_node, set_rewind_pending_node] =
    useState<RewindNodeKey | null>(null)
  const rewind_mutation = useMutation({
    mutationFn: ({
      run_id,
      target,
    }: {
      run_id: string
      target: RewindNodeKey
    }) => rewindRun(run_id, target),
    onSettled: () => {
      set_rewind_pending_node(null)
    },
    onSuccess: (_data, vars) => {
      void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(vars.run_id),
      })
    },
  })
  const cancel_mutation = useMutation({
    mutationFn: (run_id: string) => cancelRun(run_id),
    onSuccess: (_data, run_id) => {
      void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run_id),
      })
    },
  })
  const is_cancellable =
    run != null && (run.status === 'running' || run.status === 'waiting')

  function handleCancel() {
    if (!run) {
      return
    }
    if (cancel_mutation.isPending) {
      return
    }
    const ok = window.confirm(
      `Cancel ${identity ?? run.id}?\n\n` +
        `파이프라인이 중단되고 run 상태가 cancelled로 표시됩니다. ` +
        `이후 Restart 버튼으로 같은 stage부터 재시작할 수 있습니다.`,
    )
    if (!ok) {
      return
    }
    cancel_mutation.mutate(run.id)
  }

  function handleRewind(target: RewindNodeKey) {
    if (!run) {
      return
    }
    if (rewind_pending_node != null) {
      return
    }
    const label = REWIND_LABELS[target]
    const ok = window.confirm(
      `Rewind ${identity ?? run.id} to ${label}?\n\n` +
        `이 단계 이후의 모든 산출물(영상, 음성, 이미지, 시나리오, ` +
        `결정 기록 등)이 영구적으로 삭제됩니다. 이 작업은 되돌릴 수 없습니다.`,
    )
    if (!ok) {
      return
    }
    set_rewind_pending_node(target)
    rewind_mutation.mutate({ run_id: run.id, target })
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
              on_rewind_request={handleRewind}
              rewind_pending_node={rewind_pending_node}
            />
          </div>
          <div className="production-app-header__actions">
            {is_cancellable ? (
              <button
                type="button"
                className="production-app-header__cancel"
                aria-label={
                  cancel_mutation.isPending ? 'Cancelling run…' : 'Cancel run'
                }
                disabled={cancel_mutation.isPending}
                onClick={handleCancel}
              >
                <CircleStop aria-hidden="true" size={18} />
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
          {rewind_mutation.isError ? (
            <div
              className="production-app-header__rewind-error"
              role="status"
              aria-live="polite"
            >
              Rewind failed:{' '}
              {rewind_mutation.error instanceof Error
                ? rewind_mutation.error.message
                : 'Unknown error — refresh and retry.'}
            </div>
          ) : null}
          {cancel_mutation.isError ? (
            <div
              className="production-app-header__rewind-error"
              role="status"
              aria-live="polite"
            >
              Cancel failed:{' '}
              {cancel_mutation.error instanceof Error
                ? cancel_mutation.error.message
                : 'Unknown error — refresh and retry.'}
            </div>
          ) : null}
        </>
      ) : null}
    </header>
  )
}
