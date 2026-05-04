import type { RunCacheEntry } from '../../lib/apiClient'
import { formatRelativeMtime } from '../../lib/formatters'

interface CachePanelProps {
  entries: RunCacheEntry[]
  dropped_stages: Set<string>
  on_toggle: (stage: string) => void
  // id_prefix scopes the per-row checkbox id so two CachePanel instances on
  // the same page (theoretical, but cheap to guard) can't collide on an `id`.
  id_prefix?: string
}

/**
 * CachePanel is the shared keep/drop-row layout used at every operator surface
 * where Phase A may re-run: pending detail (fresh start / post-rewind) and
 * the failure banner (failed Phase A entry stage). Caller owns the drop-set
 * state and feeds the unchecked rows into the next /advance or /resume
 * request as the `drop_caches` body. An empty entries array renders nothing.
 */
export function CachePanel({
  entries,
  dropped_stages,
  on_toggle,
  id_prefix = 'cache-keep',
}: CachePanelProps) {
  if (entries.length === 0) {
    return null
  }
  return (
    <div className="pending-cache-panel" aria-label="Cached artifacts">
      <p className="production-dashboard__eyebrow">Cached artifacts</p>
      <ul className="pending-cache-panel__list">
        {entries.map((entry) => {
          const is_kept = !dropped_stages.has(entry.stage)
          const checkbox_id = `${id_prefix}-${entry.stage}`
          return (
            <li key={entry.stage} className="pending-cache-panel__row">
              <input
                id={checkbox_id}
                type="checkbox"
                checked={is_kept}
                onChange={() => on_toggle(entry.stage)}
              />
              <label
                htmlFor={checkbox_id}
                className="pending-cache-panel__label"
              >
                <span className="pending-cache-panel__stage">{entry.stage}</span>
                <span className="pending-cache-panel__meta">
                  {entry.source_version || 'unknown version'}
                  {' · '}
                  {formatRelativeMtime(entry.modified_at)}
                </span>
              </label>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
