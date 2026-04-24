import { ApiClientError } from '../../lib/apiClient'
import { useCalibrationQuery } from '../../hooks/useTuning'

const DEFAULT_WINDOW = 20
const DEFAULT_LIMIT = 30

export function CalibrationSection() {
  const query = useCalibrationQuery({
    window: DEFAULT_WINDOW,
    limit: DEFAULT_LIMIT,
  })

  if (query.isPending) {
    return (
      <section
        className="tuning-section"
        aria-labelledby="tuning-calibration-heading"
      >
        <h2 id="tuning-calibration-heading" className="tuning-section__title">
          Calibration
        </h2>
        <p className="tuning-section__body">Loading kappa trend…</p>
      </section>
    )
  }

  if (query.isError || !query.data) {
    const message =
      query.error instanceof ApiClientError
        ? query.error.message
        : 'Failed to load calibration trend.'
    return (
      <section
        className="tuning-section"
        aria-labelledby="tuning-calibration-heading"
      >
        <h2 id="tuning-calibration-heading" className="tuning-section__title">
          Calibration
        </h2>
        <p className="tuning-section__error" role="alert">
          {message}
        </p>
      </section>
    )
  }

  const { latest, points } = query.data

  return (
    <section
      className="tuning-section"
      aria-labelledby="tuning-calibration-heading"
    >
      <div className="tuning-section__header">
        <h2 id="tuning-calibration-heading" className="tuning-section__title">
          Calibration
        </h2>
        <p className="tuning-section__meta">
          Cohen&apos;s kappa over the last {query.data.window} decisions.
        </p>
      </div>

      {latest ? (
        <div className="tuning-calibration__summary">
          <span className="tuning-calibration__kappa">
            {latest.kappa != null ? `κ = ${latest.kappa.toFixed(3)}` : 'κ unavailable'}
          </span>
          {latest.provisional ? (
            <span className="tuning-badge tuning-badge--provisional">
              provisional (n = {latest.window_count})
            </span>
          ) : (
            <span className="tuning-badge">n = {latest.window_count}</span>
          )}
          <span className="tuning-calibration__timestamp">
            latest {latest.computed_at}
          </span>
          {latest.reason ? (
            <span className="tuning-calibration__reason">{latest.reason}</span>
          ) : null}
        </div>
      ) : (
        <p className="tuning-section__body">
          No calibration snapshots have been recorded yet.
        </p>
      )}

      {points.length > 0 ? (
        <ol
          className="tuning-calibration__trend"
          aria-label="Calibration trend oldest to newest"
        >
          {points.map((point, i) => (
            <li
              key={`${point.computed_at}-${i}`}
              className="tuning-calibration__point"
            >
              <span className="tuning-calibration__timestamp">
                {point.computed_at}
              </span>
              <span>
                {point.kappa != null ? point.kappa.toFixed(3) : '—'}
                {point.provisional ? ' (prov)' : ''}
              </span>
            </li>
          ))}
        </ol>
      ) : null}
    </section>
  )
}
