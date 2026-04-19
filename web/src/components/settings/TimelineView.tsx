import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { TimelineDecision } from '../../contracts/runContracts'
import {
  timelineDecisionFilterValues,
} from '../../contracts/runContracts'
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { fetchDecisionsTimeline } from '../../lib/apiClient'
import { queryKeys } from '../../lib/queryKeys'

const INITIAL_TIMELINE_PARAMS = { limit: 100 } as const
const FILTER_DEBOUNCE_MS = 100

function resolveDecisionReason(item: TimelineDecision) {
  return item.note ?? item.reason_from_snapshot ?? ''
}

function formatTimestamp(created_at: string) {
  const parsed = new Date(created_at)
  if (Number.isNaN(parsed.getTime())) {
    return created_at
  }
  return parsed.toLocaleString()
}

export function TimelineView() {
  const decisions_query = useQuery({
    queryFn: () => fetchDecisionsTimeline(INITIAL_TIMELINE_PARAMS),
    queryKey: queryKeys.decisions.timeline(INITIAL_TIMELINE_PARAMS),
    staleTime: 10_000,
  })
  const [decision_type, set_decision_type] = useState<
    (typeof timelineDecisionFilterValues)[number]
  >('all')
  const [reason_query, set_reason_query] = useState('')
  const [debounced_reason_query, set_debounced_reason_query] = useState('')
  const [selected_decision_id, set_selected_decision_id] = useState<
    number | null
  >(null)
  const row_refs = useRef(new Map<number, HTMLLIElement>())

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      set_debounced_reason_query(reason_query)
    }, FILTER_DEBOUNCE_MS)
    return () => {
      window.clearTimeout(timeout)
    }
  }, [reason_query])

  const items = decisions_query.data?.items
  const normalized_reason_query = debounced_reason_query.trim().toLowerCase()
  const visible_items = useMemo(() => {
    return (items ?? []).filter((item) => {
      if (decision_type !== 'all' && item.decision_type !== decision_type) {
        return false
      }
      if (normalized_reason_query.length === 0) {
        return true
      }
      return resolveDecisionReason(item)
        .toLowerCase()
        .includes(normalized_reason_query)
    })
  }, [decision_type, items, normalized_reason_query])

  const effective_selected_decision_id = visible_items.some(
    (item) => item.id === selected_decision_id,
  )
    ? selected_decision_id
    : visible_items[0]?.id ?? null

  useEffect(() => {
    if (effective_selected_decision_id == null) {
      return
    }
    const selected_node = row_refs.current.get(effective_selected_decision_id)
    if (!selected_node || typeof selected_node.scrollIntoView !== 'function') {
      return
    }
    selected_node.scrollIntoView({ block: 'nearest', behavior: 'auto' })
  }, [effective_selected_decision_id])

  useKeyboardShortcuts([
    {
      action: 'timeline-next',
      allow_in_editable: false,
      handler: () => {
        if (visible_items.length === 0) {
          return
        }
        const current_index = visible_items.findIndex(
          (item) => item.id === effective_selected_decision_id,
        )
        const next_index =
          current_index < 0
            ? 0
            : Math.min(visible_items.length - 1, current_index + 1)
        set_selected_decision_id(visible_items[next_index].id)
      },
      key: 'j',
      prevent_default: true,
      scope: 'context',
    },
    {
      action: 'timeline-prev',
      allow_in_editable: false,
      handler: () => {
        if (visible_items.length === 0) {
          return
        }
        const current_index = visible_items.findIndex(
          (item) => item.id === effective_selected_decision_id,
        )
        const next_index = current_index < 0 ? 0 : Math.max(0, current_index - 1)
        set_selected_decision_id(visible_items[next_index].id)
      },
      key: 'k',
      prevent_default: true,
      scope: 'context',
    },
  ])

  const has_active_filters =
    decision_type !== 'all' || reason_query.trim().length > 0

  if (decisions_query.isPending) {
    return (
      <div className='timeline-view__loading' aria-busy='true'>
        Loading decisions history…
      </div>
    )
  }

  if (decisions_query.isError) {
    return (
      <div className='timeline-view__error' role='alert'>
        <span>Failed to load decisions history.</span>
        <button
          type='button'
          className='timeline-filter-bar__clear'
          onClick={() => {
            void decisions_query.refetch()
          }}
        >
          Retry
        </button>
      </div>
    )
  }

  if ((items ?? []).length === 0) {
    return <div className='timeline-view__empty'>No decisions yet.</div>
  }

  return (
    <section
      className='timeline-view'
      aria-labelledby='settings-history-title'
    >
      <div className='timeline-view__header'>
        <div>
          <p className='route-shell__eyebrow'>Decisions history</p>
          <h2 id='settings-history-title' className='timeline-view__title'>
            Timeline
          </h2>
        </div>
        <p className='timeline-view__body'>
          Browse the latest review decisions across every run in one
          chronological feed.
        </p>
      </div>

      <div className='timeline-filter-bar'>
        <label className='timeline-filter-bar__field'>
          <span className='timeline-filter-bar__label'>Decision type</span>
          <select
            aria-label='Decision type filter'
            className='timeline-filter-bar__control'
            value={decision_type}
            onChange={(event) => {
              set_decision_type(
                event.target
                  .value as (typeof timelineDecisionFilterValues)[number],
              )
            }}
          >
            {timelineDecisionFilterValues.map((value) => (
              <option key={value} value={value}>
                {value === 'all' ? 'all' : value}
              </option>
            ))}
          </select>
        </label>

        <label className='timeline-filter-bar__field timeline-filter-bar__field--search'>
          <span className='timeline-filter-bar__label'>Reason search</span>
          <input
            type='search'
            aria-label='Reason search'
            className='timeline-filter-bar__control'
            placeholder='Search reasons'
            value={reason_query}
            onChange={(event) => {
              set_reason_query(event.target.value)
            }}
          />
        </label>

        <button
          type='button'
          className='timeline-filter-bar__clear'
          onClick={() => {
            set_decision_type('all')
            set_reason_query('')
          }}
          disabled={!has_active_filters}
        >
          Clear filters
        </button>
      </div>

      {visible_items.length === 0 ? (
        <div className='timeline-view__empty'>
          No decisions match the current filters.
        </div>
      ) : (
        <ol className='timeline-view__list' aria-label='Decisions history timeline' role='listbox'>
          {visible_items.map((item) => {
            const is_selected = item.id === effective_selected_decision_id
            const reason = resolveDecisionReason(item)
            const scene_label = item.scene_id == null ? 'Run-level' : `Scene ${item.scene_id}`
            return (
              <li
                key={item.id}
                ref={(node) => {
                  if (node) {
                    row_refs.current.set(item.id, node)
                    return
                  }
                  row_refs.current.delete(item.id)
                }}
                aria-selected={is_selected}
                className={`timeline-row${is_selected ? ' timeline-row--selected' : ''}${item.superseded_by != null ? ' timeline-row--superseded' : ''}`}
                role='option'
                tabIndex={is_selected ? 0 : -1}
                onClick={() => {
                  set_selected_decision_id(item.id)
                }}
              >
                <span className='timeline-row__timestamp'>
                  {formatTimestamp(item.created_at)}
                </span>
                <div className='timeline-row__body'>
                  <div className='timeline-row__meta'>
                    <span className='timeline-row__scp'>{item.scp_id}</span>
                    <span className='timeline-row__type'>{item.decision_type}</span>
                    <span className='timeline-row__scene'>{scene_label}</span>
                    {item.superseded_by != null ? (
                      <span className='timeline-row__badge'>Undone</span>
                    ) : null}
                  </div>
                  <p className='timeline-row__reason'>{reason}</p>
                </div>
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}
