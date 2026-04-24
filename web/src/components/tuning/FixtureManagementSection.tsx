import { useState } from 'react'
import { ApiClientError } from '../../lib/apiClient'
import {
  useGoldenAddPairMutation,
  useGoldenStateQuery,
} from '../../hooks/useTuning'

export function FixtureManagementSection() {
  const query = useGoldenStateQuery()
  const mutation = useGoldenAddPairMutation()
  const [positive, setPositive] = useState<File | null>(null)
  const [negative, setNegative] = useState<File | null>(null)
  const [status, setStatus] = useState<string | null>(null)

  async function handleAdd() {
    if (!positive || !negative) {
      setStatus('Both positive and negative fixtures are required.')
      return
    }
    setStatus(null)
    try {
      const meta = await mutation.mutateAsync({ positive, negative })
      setPositive(null)
      setNegative(null)
      setStatus(`Added pair ${String(meta.index).padStart(6, '0')}.`)
    } catch (err) {
      setStatus(
        err instanceof ApiClientError
          ? err.message
          : err instanceof Error
          ? err.message
          : 'Failed to add fixture pair.',
      )
    }
  }

  return (
    <section
      className="tuning-section"
      aria-labelledby="tuning-fixtures-heading"
    >
      <div className="tuning-section__header">
        <h2 id="tuning-fixtures-heading" className="tuning-section__title">
          Fixture Management
        </h2>
        <p className="tuning-section__meta">
          Positive + negative pairs only. The 1:1 rule is enforced by the
          backend.
        </p>
      </div>

      {query.data ? (
        <ul className="tuning-fixture-list" aria-label="Registered fixture pairs">
          {query.data.pairs.length === 0 ? (
            <li className="tuning-fixture-list__empty">
              No fixture pairs registered yet.
            </li>
          ) : (
            query.data.pairs.map((pair) => (
              <li key={pair.index} className="tuning-fixture-list__item">
                <code>{String(pair.index).padStart(6, '0')}</code> ·{' '}
                {pair.positive_path} / {pair.negative_path}
              </li>
            ))
          )}
        </ul>
      ) : null}

      <div className="tuning-fixture-upload">
        <label className="tuning-fixture-upload__field">
          <span>Positive fixture</span>
          <input
            type="file"
            accept="application/json"
            aria-label="Positive fixture file"
            onChange={(event) => setPositive(event.target.files?.[0] ?? null)}
          />
        </label>
        <label className="tuning-fixture-upload__field">
          <span>Negative fixture</span>
          <input
            type="file"
            accept="application/json"
            aria-label="Negative fixture file"
            onChange={(event) => setNegative(event.target.files?.[0] ?? null)}
          />
        </label>
        <button
          type="button"
          className="tuning-button tuning-button--primary"
          disabled={mutation.isPending || !positive || !negative}
          onClick={handleAdd}
        >
          {mutation.isPending ? 'Uploading…' : 'Add fixture pair'}
        </button>
      </div>

      {status ? (
        <p className="tuning-section__status" role="status">
          {status}
        </p>
      ) : null}
    </section>
  )
}
