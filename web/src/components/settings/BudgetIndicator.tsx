import { formatCurrency } from '../../lib/formatters'
import type { SettingsSnapshot } from '../../contracts/settingsContracts'

function formatPercent(ratio: number) {
  return `${Math.round(ratio * 100)}%`
}

export function BudgetIndicator({
  budget,
}: {
  budget: SettingsSnapshot['budget']
}) {
  const progress = Math.max(0, Math.min(100, Math.round(budget.progress_ratio * 100)))

  return (
    <section
      className={`settings-card settings-budget settings-budget--${budget.status}`}
      aria-labelledby='settings-budget-title'
    >
      <div className='settings-card__header'>
        <div>
          <p className='route-shell__eyebrow'>Budget telemetry</p>
          <h2 id='settings-budget-title' className='settings-card__title'>
            Spend against cap
          </h2>
        </div>
        <span className={`settings-budget__pill settings-budget__pill--${budget.status}`}>
          {budget.status === 'safe'
            ? 'Safe'
            : budget.status === 'near_cap'
              ? 'Near cap'
              : 'Exceeded'}
        </span>
      </div>

      <p className='settings-budget__summary'>
        {formatPercent(budget.progress_ratio)} of hard cap used
      </p>

      <div className='settings-budget__bar' aria-hidden='true'>
        <span className='settings-budget__bar-fill' style={{ width: `${progress}%` }} />
      </div>

      <dl className='settings-budget__stats'>
        <div>
          <dt>Current spend</dt>
          <dd>{formatCurrency(budget.current_spend_usd)}</dd>
        </div>
        <div>
          <dt>Soft cap</dt>
          <dd>{formatCurrency(budget.soft_cap_usd)}</dd>
        </div>
        <div>
          <dt>Hard cap</dt>
          <dd>{formatCurrency(budget.hard_cap_usd)}</dd>
        </div>
      </dl>

      <p className='settings-budget__source'>
        {budget.source.label}
        {budget.source.run_id ? ` • ${budget.source.run_id}` : ''}
      </p>
    </section>
  )
}
