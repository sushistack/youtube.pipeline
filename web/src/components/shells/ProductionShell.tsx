import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { fetchRunList } from '../../lib/apiClient'
import {
  compareRunsForInventory,
  formatContinuityMessage,
  formatCurrency,
  formatElapsed,
  formatRunSummary,
  getStageLabel,
  getStatusLabel,
} from '../../lib/formatters'
import { queryKeys } from '../../lib/queryKeys'
import { useUIStore, type ProductionLastSeenSnapshot } from '../../stores/useUIStore'
import { useRunStatus } from '../../hooks/useRunStatus'
import { CharacterPick } from '../production/CharacterPick'
import { ScenarioInspector } from '../production/ScenarioInspector'
import { ContinuityBanner } from '../shared/ContinuityBanner'
import { FailureBanner } from '../shared/FailureBanner'
import { ProductionShortcutPanel } from '../shared/ProductionShortcutPanel'
import { StageStepper } from '../shared/StageStepper'
import { StatusBar } from '../shared/StatusBar'

function toLastSeenSnapshot(
  run:
    | {
        id: string
        stage: ProductionLastSeenSnapshot['stage']
        status: ProductionLastSeenSnapshot['status']
        updated_at: string
      }
    | null
    | undefined,
) {
  if (!run) {
    return null
  }

  return {
    run_id: run.id,
    stage: run.stage,
    status: run.status,
    updated_at: run.updated_at,
  } satisfies ProductionLastSeenSnapshot
}

function snapshotsMatch(
  previous: ProductionLastSeenSnapshot | undefined,
  current: ProductionLastSeenSnapshot | null,
) {
  if (!previous || !current) {
    return false
  }

  return (
    previous.run_id === current.run_id &&
    previous.updated_at === current.updated_at &&
    previous.stage === current.stage &&
    previous.status === current.status
  )
}

export function ProductionShell() {
  const [search_params, set_search_params] = useSearchParams()
  const production_last_seen = useUIStore((state) => state.production_last_seen)
  const set_production_last_seen = useUIStore((state) => state.set_production_last_seen)
  const [dismissed_run_id, set_dismissed_run_id] = useState<string | null>(null)
  const [dismissed_continuity_key, set_dismissed_continuity_key] = useState<string | null>(null)
  const latest_snapshot_ref = useRef<ProductionLastSeenSnapshot | null>(null)
  const banner_active_ref = useRef(false)
  const selected_run_id = search_params.get('run')
  const runs_query = useQuery({
    queryFn: fetchRunList,
    queryKey: queryKeys.runs.list(),
    staleTime: 5_000,
  })
  const runs = (runs_query.data ?? []).slice().sort(compareRunsForInventory)
  const fallback_run = runs[0] ?? null
  const selected_run = runs.find((run) => run.id === selected_run_id) ?? fallback_run
  const run_status_query = useRunStatus(selected_run?.id ?? null)
  const status_payload =
    run_status_query.data?.run.id === selected_run?.id ? run_status_query.data : undefined
  const current_run = status_payload?.run ?? selected_run
  const is_status_ready =
    current_run?.id != null &&
    run_status_query.isFetched &&
    status_payload?.run.id === current_run.id
  const current_snapshot = toLastSeenSnapshot(current_run)
  const previous_snapshot =
    current_run != null ? production_last_seen[current_run.id] : undefined
  const continuity_key = current_snapshot
    ? `${current_snapshot.run_id}:${current_snapshot.updated_at}:${current_snapshot.stage}:${current_snapshot.status}`
    : null
  const continuity_message =
    current_snapshot &&
    previous_snapshot &&
    is_status_ready &&
    !snapshotsMatch(previous_snapshot, current_snapshot)
      ? formatContinuityMessage(status_payload ?? {})
      : null
  const is_failure_banner_dismissed =
    current_run?.id != null && dismissed_run_id === current_run.id
  const show_continuity_banner =
    continuity_message != null &&
    continuity_key != null &&
    dismissed_continuity_key !== continuity_key

  useEffect(() => {
    if (!runs_query.isSuccess) {
      return
    }

    if (selected_run_id && !selected_run) {
      set_search_params((current) => {
        const next = new URLSearchParams(current)
        next.delete('run')
        return next
      }, { replace: true })
      return
    }

    if (!selected_run || selected_run_id === selected_run.id) {
      return
    }

    set_search_params((current) => {
      const next = new URLSearchParams(current)
      next.set('run', selected_run.id)
      return next
    }, { replace: true })
  }, [runs_query.isSuccess, selected_run, selected_run_id, set_search_params])

  useEffect(() => {
    if (is_status_ready) {
      latest_snapshot_ref.current = current_snapshot
    }
  }, [current_snapshot, is_status_ready])

  useEffect(() => {
    banner_active_ref.current = show_continuity_banner
  })

  useEffect(() => {
    return () => {
      if (banner_active_ref.current) {
        return
      }
      if (latest_snapshot_ref.current) {
        set_production_last_seen(latest_snapshot_ref.current)
      }
    }
  }, [set_production_last_seen])

  useEffect(() => {
    if (
      current_snapshot &&
      previous_snapshot &&
      !snapshotsMatch(previous_snapshot, current_snapshot) &&
      is_status_ready &&
      !continuity_message
    ) {
      set_production_last_seen(current_snapshot)
    }
  }, [
    continuity_message,
    current_snapshot,
    is_status_ready,
    previous_snapshot,
    set_production_last_seen,
  ])

  function dismissContinuityBanner() {
    if (!current_snapshot) {
      return
    }

    set_production_last_seen(current_snapshot)
    set_dismissed_continuity_key(continuity_key)
  }

  return (
    <section className="route-shell" aria-labelledby="production-shell-title">
      <p className="route-shell__eyebrow">Production workflow</p>
      <h1 id="production-shell-title" className="route-shell__title">
        Production
      </h1>
      <p className="route-shell__body">
        Monitor live pipeline telemetry, inspect the selected run, and keep the
        operator shell ready for the next review surface.
      </p>

      <StatusBar run={current_run} status_payload={status_payload} />

      {current_run ? (
        <div className="production-dashboard">
          {current_run.status === 'failed' && !is_failure_banner_dismissed ? (
            <FailureBanner
              on_dismiss={() => set_dismissed_run_id(current_run.id)}
              run={current_run}
            />
          ) : null}

          {show_continuity_banner ? (
            <ContinuityBanner
              message={continuity_message}
              on_dismiss={dismissContinuityBanner}
            />
          ) : null}

          <section className="production-dashboard__hero">
            <div className="production-dashboard__hero-copy">
              <p className="production-dashboard__eyebrow">Selected run</p>
              <h2 className="production-dashboard__title">{current_run.id}</h2>
              <p className="production-dashboard__summary">
                {formatRunSummary(current_run, status_payload)}
              </p>
            </div>

            <div className="production-dashboard__hero-meta">
              <span className="production-dashboard__badge">
                {getStatusLabel(current_run.status)}
              </span>
              <span className="production-dashboard__meta">
                {getStageLabel(current_run.stage)}
              </span>
            </div>
          </section>

          <section className="production-dashboard__panel">
            <header className="production-dashboard__panel-header">
              <div>
                <p className="production-dashboard__eyebrow">Stage progress</p>
                <h2 className="production-dashboard__section-title">
                  Six-node pipeline map
                </h2>
              </div>
            </header>
            <StageStepper
              stage={current_run.stage}
              status={current_run.status}
              variant="full"
            />
          </section>

          <section className="production-dashboard__metrics">
            <article className="production-dashboard__metric-card">
              <p className="production-dashboard__eyebrow">Elapsed</p>
              <strong>{formatElapsed(current_run.duration_ms)}</strong>
            </article>
            <article className="production-dashboard__metric-card">
              <p className="production-dashboard__eyebrow">Spend</p>
              <strong>{formatCurrency(current_run.cost_usd)}</strong>
            </article>
            <article className="production-dashboard__metric-card">
              <p className="production-dashboard__eyebrow">Decision state</p>
              <strong>
                {status_payload?.decisions_summary
                  ? `${status_payload.decisions_summary.approved_count} approved`
                  : 'No review summary yet'}
              </strong>
            </article>
          </section>

          {current_run.stage === 'scenario_review' && current_run.status === 'waiting' ? (
            <ScenarioInspector run_id={current_run.id} />
          ) : current_run.stage === 'character_pick' && current_run.status === 'waiting' ? (
            // key on run.id so switching between runs remounts the component
            // and resets phase/selection/refs — preserving the same instance
            // across runs would leak phase='descriptor' and a selected
            // candidate from a previously-viewed run into the new one.
            <CharacterPick key={current_run.id} run={current_run} />
          ) : (
            <ProductionShortcutPanel />
          )}
        </div>
      ) : (
        <div className="production-empty-state">
          <h2 className="production-dashboard__section-title">No runs yet</h2>
          <p className="route-shell__body">
            Start or resume a pipeline run to populate the Production dashboard.
          </p>
        </div>
      )}
    </section>
  )
}
