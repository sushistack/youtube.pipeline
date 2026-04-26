import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { advanceRun, fetchRunList } from '../../lib/apiClient'
import {
  compareRunsForInventory,
  formatContinuityMessage,
  getRunSequence,
  getStageLabel,
  getStatusLabel,
  getStatusTone,
} from '../../lib/formatters'
import { queryKeys } from '../../lib/queryKeys'
import { useUIStore, type ProductionLastSeenSnapshot } from '../../stores/useUIStore'
import { useRunScenes } from '../../hooks/useRunScenes'
import { useRunStatus } from '../../hooks/useRunStatus'
import { BatchReview } from '../production/BatchReview'
import { CharacterPick } from '../production/CharacterPick'
import { ComplianceGate } from '../production/ComplianceGate'
import { CompletionReward } from '../production/CompletionReward'
import { useNewRunCoordinator } from '../production/useNewRunCoordinator'
import { ScenarioInspector } from '../production/ScenarioInspector'
import { ContinuityBanner } from '../shared/ContinuityBanner'
import { DetailPanel } from '../shared/DetailPanel'
import { FailureBanner } from '../shared/FailureBanner'
import { ProductionAppHeader } from '../shared/ProductionAppHeader'
import { ProductionMasterDetail } from '../shared/ProductionMasterDetail'
import { SceneCard } from '../shared/SceneCard'
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
  const { open_new_run_panel } = useNewRunCoordinator()
  const production_last_seen = useUIStore((state) => state.production_last_seen)
  const set_production_last_seen = useUIStore((state) => state.set_production_last_seen)
  const [dismissed_run_id, set_dismissed_run_id] = useState<string | null>(null)
  const [dismissed_continuity_key, set_dismissed_continuity_key] = useState<string | null>(null)
  const latest_snapshot_ref = useRef<ProductionLastSeenSnapshot | null>(null)
  const banner_active_ref = useRef(false)
  const empty_state_button_ref = useRef<HTMLButtonElement | null>(null)
  const selected_run_id = search_params.get('run')
  const query_client = useQueryClient()
  const runs_query = useQuery({
    queryFn: fetchRunList,
    queryKey: queryKeys.runs.list(),
    staleTime: 5_000,
  })
  // pending → Phase A entry. Server's POST /api/runs/{id}/resume rejects
  // pending status with 409 by design — only failed/waiting are resumable.
  // The Start-run button calls /advance instead, which dispatches Engine.Advance.
  const advance_mutation = useMutation({
    mutationFn: (run_id: string) => advanceRun(run_id),
    onSuccess: (_data, run_id) => {
      void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run_id),
      })
    },
  })
  const runs = (runs_query.data ?? []).slice().sort(compareRunsForInventory)
  const fallback_run = runs[0] ?? null
  const matched_selected_run =
    runs.find((run) => run.id === selected_run_id) ?? null
  const selected_run = matched_selected_run ?? fallback_run
  const status_run_id = selected_run_id ?? selected_run?.id ?? null
  const run_status_query = useRunStatus(status_run_id)
  const status_payload =
    run_status_query.data?.run.id === status_run_id ? run_status_query.data : undefined
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

  // Fallback selection is bootstrap-only: it runs at most once per shell mount,
  // when the inventory has loaded and the route does not yet carry an explicit
  // `?run=`. After it has either seeded a default run or confirmed the URL was
  // already authoritative, it never re-fires — preventing it from racing an
  // explicit post-create selection write from `Sidebar.handleNewRunSuccess`.
  const has_bootstrapped_selection_ref = useRef(false)
  useEffect(() => {
    if (has_bootstrapped_selection_ref.current) {
      return
    }
    if (!runs_query.isSuccess) {
      return
    }

    if (selected_run_id) {
      has_bootstrapped_selection_ref.current = true
      return
    }

    if (!selected_run) {
      return
    }

    has_bootstrapped_selection_ref.current = true
    set_search_params((current) => {
      const next = new URLSearchParams(current)
      next.set('run', selected_run.id)
      return next
    }, { replace: true })
  }, [
    runs_query.isSuccess,
    selected_run,
    selected_run_id,
    set_search_params,
  ])

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

  function renderPendingDetail() {
    if (!current_run) {
      return null
    }
    const seq = getRunSequence(current_run.id)
    const display_title =
      seq != null ? `SCP-${current_run.scp_id} Run #${seq}` : current_run.id
    return (
      <section className="production__pending-state" aria-label="Pending run guidance">
        <div className="production__pending-state-copy">
          <p className="production-dashboard__eyebrow">Run created</p>
          <h2 className="production-dashboard__section-title">{display_title}</h2>
          <p className="production-dashboard__summary">{current_run.id}</p>
        </div>

        <div className="production__pending-state-meta">
          <span
            className="run-card__badge"
            data-tone={getStatusTone(current_run.status)}
          >
            {getStatusLabel(current_run.status)}
          </span>
        </div>

        <p className="route-shell__body">
          Run created. It has not started yet. Click <strong>Start run</strong>
          {' '}to begin Phase A.
        </p>

        <div className="production__pending-state-actions">
          <button
            type="button"
            className="production__pending-resume-btn"
            disabled={
              advance_mutation.isPending &&
              advance_mutation.variables === current_run.id
            }
            onClick={() => {
              advance_mutation.mutate(current_run.id)
            }}
          >
            {advance_mutation.isPending &&
            advance_mutation.variables === current_run.id
              ? 'Starting…'
              : 'Start run'}
          </button>
          {advance_mutation.isError &&
          advance_mutation.variables === current_run.id ? (
            <span className="production__pending-resume-error" role="status">
              Start failed: {advance_mutation.error instanceof Error
                ? advance_mutation.error.message
                : 'Unknown error — check the run log and retry.'}
            </span>
          ) : null}
        </div>
      </section>
    )
  }

  function renderStageDetail() {
    if (!current_run) {
      return null
    }
    if (current_run.stage === 'pending' && current_run.status === 'pending') {
      return renderPendingDetail()
    }
    if ((current_run.stage === 'image' || current_run.stage === 'tts') && current_run.status === 'waiting') {
      const is_pending = advance_mutation.isPending && advance_mutation.variables === current_run.id
      return (
        <section className="production__pending-state" aria-label="Asset generation gate">
          <div className="production__pending-state-copy">
            <p className="production-dashboard__eyebrow">Ready to generate</p>
            <h2 className="production-dashboard__section-title">Generate Assets</h2>
          </div>
          <p className="route-shell__body">
            Scenario and character confirmed. Click <strong>Generate Assets</strong> to start
            image generation and voice rendering for all scenes.
          </p>
          <div className="production__pending-state-actions">
            <button
              type="button"
              className="production__pending-resume-btn"
              disabled={is_pending}
              onClick={() => advance_mutation.mutate(current_run.id)}
            >
              {is_pending ? 'Starting…' : 'Generate Assets'}
            </button>
            {advance_mutation.isError && advance_mutation.variables === current_run.id ? (
              <span className="production__pending-resume-error" role="status">
                Failed: {advance_mutation.error instanceof Error
                  ? advance_mutation.error.message
                  : 'Unknown error — check the run log and retry.'}
              </span>
            ) : null}
          </div>
        </section>
      )
    }
    if (current_run.stage === 'scenario_review' && current_run.status === 'waiting') {
      // key on run.id matches sibling stage branches: switching runs while in
      // scenario_review must remount the inspector to flush its
      // active_index/draft state instead of leaking it across runs.
      return <ScenarioInspector key={current_run.id} run_id={current_run.id} />
    }
    if (current_run.stage === 'character_pick' && current_run.status === 'waiting') {
      return <CharacterPick key={current_run.id} run={current_run} />
    }
    if (current_run.stage === 'metadata_ack' && current_run.status === 'waiting') {
      return <ComplianceGate key={current_run.id} run={current_run} />
    }
    if (current_run.stage === 'complete' && current_run.status === 'completed') {
      return <CompletionReward key={current_run.id} run={current_run} />
    }
    // SCL-5: at non-HITL stages with populated segments, show the read-only
    // DetailPanel for the selected scene instead of a generic placeholder.
    if (active_scene != null) {
      return <DetailPanel key={`${current_run.id}-${active_scene.scene_index}`} item={active_scene} />
    }
    return (
      <section className="production__stage-placeholder" aria-label="Stage in progress">
        <p className="production-dashboard__eyebrow">Stage in progress</p>
        <p className="route-shell__body">
          Pipeline is working through this stage. Scene-level review surfaces
          will appear once the run reaches a review checkpoint.
        </p>
      </section>
    )
  }

  function renderMasterPane() {
    if (!has_scenes) {
      return null
    }
    const selected_index = clamped_active_index
    return (
      <ol className="production-master-detail__scene-list" role="listbox" aria-label="Scenes">
        {scenes.map((scene, index) => (
          <li key={scene.scene_index} className="production-master-detail__scene-list-item">
            <SceneCard
              item={scene}
              on_select={() => set_active_scene_index(index)}
              selected={index === selected_index}
            />
          </li>
        ))}
      </ol>
    )
  }

  const is_batch_review_surface =
    current_run?.stage === 'batch_review' && current_run.status === 'waiting'

  // SCL-5: master scene list renders at every post-Phase-A surface (not just
  // batch_review). pending stage is excluded because Phase A has not produced
  // segments yet; batch_review continues to render its own full-bleed master.
  const should_fetch_scenes =
    current_run != null &&
    current_run.stage !== 'pending' &&
    !is_batch_review_surface
  const scenes_query = useRunScenes(should_fetch_scenes ? current_run!.id : null)
  const scenes = scenes_query.data ?? []
  const [active_scene_index, set_active_scene_index] = useState(0)
  useEffect(() => {
    set_active_scene_index(0)
  }, [current_run?.id])
  const has_scenes = scenes.length > 0
  const clamped_active_index = scenes.length > 0 ? Math.min(active_scene_index, scenes.length - 1) : 0
  const active_scene = has_scenes ? scenes[clamped_active_index] : null

  function getMasterEmptyMessage() {
    if (!current_run) {
      return 'Scenes will appear once a run is selected.'
    }
    if (current_run.stage === 'pending') {
      return 'Scenes will appear once Phase A finishes.'
    }
    return `Scenes are not yet available — ${getStageLabel(current_run.stage).toLowerCase()} in progress.`
  }

  return (
    <section className="route-shell production-shell" aria-labelledby="production-shell-title">
      <h1 id="production-shell-title" className="stage-stepper__sr-only">
        Production
      </h1>

      <ProductionAppHeader run={current_run ?? null} status_payload={status_payload} />

      {current_run && (current_run.status === 'failed' || current_run.status === 'cancelled') && !is_failure_banner_dismissed ? (
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

      <div className="production-shell__layout">
        {current_run ? (
          is_batch_review_surface ? (
            <BatchReview key={current_run.id} run={current_run} />
          ) : (
            <ProductionMasterDetail
              master={renderMasterPane()}
              detail={renderStageDetail()}
              master_empty_message={getMasterEmptyMessage()}
            />
          )
        ) : (
          <ProductionMasterDetail
            master_empty_message="Scenes will appear once a run is selected."
            detail={
              <div className="production-empty-state">
                <h2 className="production-dashboard__section-title">No runs yet</h2>
                <p className="route-shell__body">
                  Start or resume a pipeline run to populate the Production dashboard.
                </p>
                <button
                  ref={empty_state_button_ref}
                  type="button"
                  className="production-empty-state__action"
                  onClick={() => {
                    open_new_run_panel({
                      restore_focus_to: empty_state_button_ref.current,
                    })
                  }}
                >
                  New Run
                </button>
              </div>
            }
          />
        )}
      </div>

      <StatusBar run={current_run ?? null} status_payload={status_payload} />
    </section>
  )
}
