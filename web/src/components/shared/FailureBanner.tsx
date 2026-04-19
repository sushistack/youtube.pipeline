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
      on_dismiss()
      void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      })
    },
  })

  useKeyboardShortcuts(
    [
      {
        enabled: run.status === 'failed' && !resume_mutation.isPending,
        handler: () => {
          resume_mutation.mutate()
        },
        key: 'enter',
        prevent_default: true,
        scope: 'context',
      },
      {
        enabled: run.status === 'failed',
        handler: () => {
          on_dismiss()
        },
        key: 'escape',
        prevent_default: true,
        scope: 'context',
      },
    ],
    { enabled: run.status === 'failed' },
  )

  if (run.status !== 'failed') {
    return null
  }

  const variant_class =
    run.retry_reason === 'rate_limit'
      ? 'failure-banner--retryable'
      : 'failure-banner--fatal'

  return (
    <section
      aria-label="Run failure recovery"
      className={`failure-banner ${variant_class}`}
      role="alert"
    >
      <div className="failure-banner__content">
        <div className="failure-banner__copy">
          <p className="failure-banner__eyebrow">Pipeline failed</p>
          <h2 className="failure-banner__title">
            {getFailureMessage(run.retry_reason)}
          </h2>
          <p className="failure-banner__meta">
            Spend so far: <strong>{formatCurrency(run.cost_usd)}</strong>
          </p>
          <p className="failure-banner__reassurance">
            No work was lost. Completed stages remain intact.
          </p>
          {resume_mutation.isError ? (
            <p className="failure-banner__error" role="status">
              Resume failed: {resume_mutation.error instanceof Error
                ? resume_mutation.error.message
                : 'Unknown error — try again or check the run log.'}
            </p>
          ) : null}
        </div>

        <div className="failure-banner__actions">
          <button
            className="failure-banner__resume"
            disabled={resume_mutation.isPending}
            onClick={() => resume_mutation.mutate()}
            type="button"
          >
            <span className="failure-banner__shortcut">[Enter]</span>
            <span>{resume_mutation.isPending ? 'Resuming...' : 'Resume'}</span>
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
      </div>
    </section>
  )
}
