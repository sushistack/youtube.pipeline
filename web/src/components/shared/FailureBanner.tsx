import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { RunSummary } from '../../contracts/runContracts'
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { resumeRun } from '../../lib/apiClient'
import { formatCurrency } from '../../lib/formatters'
import { queryKeys } from '../../lib/queryKeys'

interface FailureBannerProps {
  on_dismiss: () => void
  run: RunSummary
}

function getFailureMessage(retry_reason: string | null | undefined) {
  if (retry_reason === 'rate_limit') {
    return 'DashScope rate limit - request throttled'
  }

  if (retry_reason) {
    return retry_reason
  }

  return 'Stage failed - check the run log for details'
}

export function FailureBanner({ on_dismiss, run }: FailureBannerProps) {
  const query_client = useQueryClient()
  const resume_mutation = useMutation({
    mutationFn: () => resumeRun(run.id),
    onSuccess: () => {
      // status가 cancelled/failed → running/waiting 으로 바뀌면 부모 셸에서 배너 조건이
      // 자연스럽게 false가 되어 사라진다. on_dismiss를 같이 호출하면 부모의
      // dismissed_run_id에 run.id가 박혀, 같은 run이 다시 cancelled 됐을 때
      // 배너가 다시 뜨지 않는 cycle 버그가 발생.
      void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      })
    },
  })

  const is_resumable = run.status === 'failed' || run.status === 'cancelled'

  useKeyboardShortcuts(
    [
      {
        enabled: is_resumable && !resume_mutation.isPending,
        handler: () => {
          resume_mutation.mutate()
        },
        key: 'enter',
        prevent_default: true,
        scope: 'context',
      },
      {
        enabled: is_resumable,
        handler: () => {
          on_dismiss()
        },
        key: 'escape',
        prevent_default: true,
        scope: 'context',
      },
    ],
    { enabled: is_resumable },
  )

  if (!is_resumable) {
    return null
  }

  const is_cancelled = run.status === 'cancelled'
  const variant_class = is_cancelled
    ? 'failure-banner--cancelled'
    : run.retry_reason === 'rate_limit'
      ? 'failure-banner--retryable'
      : 'failure-banner--fatal'

  return (
    <section
      aria-label={is_cancelled ? 'Run cancelled recovery' : 'Run failure recovery'}
      className={`failure-banner ${variant_class}`}
      role="alert"
    >
      <div className="failure-banner__content">
        <p className="failure-banner__meta">
          {is_cancelled
            ? <><strong>Run cancelled</strong> · Spend <strong>{formatCurrency(run.cost_usd)}</strong></>
            : <><strong>Pipeline failed</strong> — {getFailureMessage(run.retry_reason)} · Spend <strong>{formatCurrency(run.cost_usd)}</strong></>
          }
        </p>
        <div className="failure-banner__actions">
          <button
            className="failure-banner__resume"
            disabled={resume_mutation.isPending}
            onClick={() => resume_mutation.mutate()}
            type="button"
          >
            <span className="failure-banner__shortcut">[Enter]</span>
            <span>{resume_mutation.isPending ? (is_cancelled ? 'Restarting...' : 'Resuming...') : (is_cancelled ? 'Restart' : 'Resume')}</span>
          </button>
          <button
            aria-label="Dismiss failure banner"
            className="failure-banner__dismiss"
            onClick={on_dismiss}
            type="button"
          >
            ×
          </button>
        </div>
        {resume_mutation.isError ? (
          <p className="failure-banner__error" role="status">
            Resume failed: {resume_mutation.error instanceof Error
              ? resume_mutation.error.message
              : 'Unknown error — try again or check the run log.'}
          </p>
        ) : null}
      </div>
    </section>
  )
}
