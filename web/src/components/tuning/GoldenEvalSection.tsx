import { useState } from 'react'
import type { GoldenReport, GoldenState } from '../../contracts/tuningContracts'
import { ApiClientError } from '../../lib/apiClient'
import {
  useGoldenRunMutation,
  useGoldenStateQuery,
} from '../../hooks/useTuning'

export interface GoldenEvalSectionProps {
  onGoldenPassed?: () => void
}

export function GoldenEvalSection({ onGoldenPassed }: GoldenEvalSectionProps) {
  const query = useGoldenStateQuery()
  const mutation = useGoldenRunMutation()
  const [report, setReport] = useState<GoldenReport | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function handleRun() {
    setError(null)
    try {
      const r = await mutation.mutateAsync()
      setReport(r)
      // AC-6 session gate: Golden pass unlocks Shadow. "Pass" requires
      // (a) at least one negative fixture was graded — an empty manifest
      // trivially has false_rejects=0 but has not actually exercised the
      // prompt, so a zero-pair state must not open the gate; and
      // (b) zero false rejections on the positive fixtures. Recall is
      // intentionally not part of the gate so a thin fixture set still
      // allows Shadow once false_rejects=0 holds.
      if (r.total_negative > 0 && r.false_rejects === 0) {
        onGoldenPassed?.()
      }
    } catch (err) {
      setReport(null)
      setError(
        err instanceof ApiClientError
          ? err.message
          : err instanceof Error
          ? err.message
          : 'Golden eval failed.',
      )
    }
  }

  return (
    <section
      className="tuning-section"
      aria-labelledby="tuning-golden-heading"
    >
      <div className="tuning-section__header">
        <h2 id="tuning-golden-heading" className="tuning-section__title">
          Golden Eval
        </h2>
        <GoldenStateSummary state={query.data ?? null} isPending={query.isPending} />
      </div>
      <div className="tuning-section__actions">
        <button
          type="button"
          className="tuning-button tuning-button--primary"
          disabled={mutation.isPending}
          onClick={handleRun}
        >
          {mutation.isPending ? 'Running…' : 'Run Golden eval'}
        </button>
      </div>
      {error ? (
        <p className="tuning-section__error" role="alert">
          {error}
        </p>
      ) : null}
      {report ? (
        <p className="tuning-report__summary">
          recall {(report.recall * 100).toFixed(1)}% · detected{' '}
          {report.detected_negative}/{report.total_negative} negatives ·{' '}
          {report.false_rejects === 0 ? (
            <span className="tuning-verdict tuning-verdict--pass">
              0 false rejects
            </span>
          ) : (
            <span className="tuning-verdict tuning-verdict--retry">
              {report.false_rejects} false rejects
            </span>
          )}
        </p>
      ) : null}
    </section>
  )
}

function GoldenStateSummary({
  state,
  isPending,
}: {
  state: GoldenState | null
  isPending: boolean
}) {
  if (isPending) {
    return <p className="tuning-section__meta">Loading state…</p>
  }
  if (!state) {
    return null
  }
  return (
    <div className="tuning-section__meta">
      <p>
        {state.pair_count} fixture pair{state.pair_count === 1 ? '' : 's'} ·
        last refreshed {state.freshness.days_since_refresh}d ago
      </p>
      {state.freshness.warnings.length > 0 ? (
        <ul
          className="tuning-warnings"
          role="note"
          aria-label="Golden freshness warnings"
        >
          {state.freshness.warnings.map((warning) => (
            <li key={warning} className="tuning-warning">
              {warning}
            </li>
          ))}
        </ul>
      ) : null}
      {state.last_report ? (
        <p>
          Last run: recall {(state.last_report.recall * 100).toFixed(1)}%, false
          rejects {state.last_report.false_rejects}
        </p>
      ) : null}
    </div>
  )
}
