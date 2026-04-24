import { useState } from 'react'
import type { FastFeedbackReport } from '../../contracts/tuningContracts'
import { ApiClientError } from '../../lib/apiClient'
import { useFastFeedbackMutation } from '../../hooks/useTuning'

export function FastFeedbackSection() {
  const mutation = useFastFeedbackMutation()
  const [report, setReport] = useState<FastFeedbackReport | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function handleRun() {
    setError(null)
    try {
      const r = await mutation.mutateAsync()
      setReport(r)
    } catch (err) {
      setReport(null)
      setError(
        err instanceof ApiClientError
          ? err.message
          : err instanceof Error
          ? err.message
          : 'Fast Feedback failed.',
      )
    }
  }

  return (
    <section
      className="tuning-section"
      aria-labelledby="tuning-fast-feedback-heading"
    >
      <div className="tuning-section__header">
        <h2 id="tuning-fast-feedback-heading" className="tuning-section__title">
          Fast Feedback
        </h2>
        <p className="tuning-section__meta">
          Deterministic 10-sample pass against the saved Critic prompt.
        </p>
      </div>
      <div className="tuning-section__actions">
        <button
          type="button"
          className="tuning-button tuning-button--primary"
          disabled={mutation.isPending}
          onClick={handleRun}
        >
          {mutation.isPending ? 'Running…' : 'Run Fast Feedback'}
        </button>
      </div>
      {error ? (
        <p className="tuning-section__error" role="alert">
          {error}
        </p>
      ) : null}
      {report ? (
        <div className="tuning-report">
          <p className="tuning-report__summary">
            {report.sample_count} samples · {report.pass_count} pass ·{' '}
            {report.retry_count} retry · {report.accept_with_notes_count}{' '}
            accept-with-notes ·{' '}
            <span aria-label="duration">{report.duration_ms} ms</span>
            {report.version_tag ? (
              <>
                {' '}
                · prompt <code>{report.version_tag}</code>
              </>
            ) : null}
          </p>
          <ul className="tuning-report__rows">
            {report.samples.map((sample) => (
              <li key={sample.fixture_id} className="tuning-report__row">
                <code>{sample.fixture_id}</code>{' '}
                <span
                  className={`tuning-verdict tuning-verdict--${sample.verdict}`}
                >
                  {sample.verdict}
                </span>
                {sample.retry_reason ? (
                  <span className="tuning-report__reason">
                    {' '}
                    — {sample.retry_reason}
                  </span>
                ) : null}
                <span className="tuning-report__score">
                  {' '}
                  score {sample.overall_score}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </section>
  )
}
