import { useCallback, useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEditNarration, useRunScenes } from '../../hooks/useRunScenes'
import { InlineNarrationEditor } from './InlineNarrationEditor'
import { approveScenarioReview, ApiClientError } from '../../lib/apiClient'
import type { RunSummary } from '../../contracts/runContracts'
import { queryKeys } from '../../lib/queryKeys'

interface ScenarioInspectorProps {
  run_id: string
  selected_scene_index: number
}

export function ScenarioInspector({ run_id, selected_scene_index }: ScenarioInspectorProps) {
  const scenes_query = useRunScenes(run_id)
  const mutation = useEditNarration(run_id)
  const [active_index, set_active_index] = useState<number | null>(null)
  const [approved_scenes, set_approved_scenes] = useState<Set<number>>(new Set())

  // Reset edit state when the user navigates to a different scene.
  useEffect(() => {
    set_active_index(null)
  }, [selected_scene_index])

  const query_client = useQueryClient()
  const approve_mutation = useMutation<RunSummary, ApiClientError>({
    mutationFn: () => approveScenarioReview(run_id),
    onSuccess: () => {
      query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.status(run_id) })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.detail(run_id) })
    },
  })

  const toggle_scene_approval = useCallback((scene_index: number) => {
    set_approved_scenes((prev) => {
      const next = new Set(prev)
      if (next.has(scene_index)) {
        next.delete(scene_index)
      } else {
        next.add(scene_index)
      }
      return next
    })
  }, [])

  const revoke_scene_approval = useCallback((scene_index: number) => {
    set_approved_scenes((prev) => {
      if (!prev.has(scene_index)) return prev
      const next = new Set(prev)
      next.delete(scene_index)
      return next
    })
  }, [])

  if (scenes_query.isPending) {
    return (
      <div className="scenario-inspector__loading" aria-busy="true">
        Loading scenes…
      </div>
    )
  }

  if (scenes_query.isError) {
    return (
      <div className="scenario-inspector__error" role="alert">
        Failed to load scenes. Try refreshing.
      </div>
    )
  }

  const scenes = scenes_query.data ?? []
  const selected_scene =
    scenes.find((s) => s.scene_index === selected_scene_index) ?? scenes[0] ?? null

  if (scenes.length === 0) {
    return (
      <div className="scenario-inspector__empty">
        No narration scenes found for this run.
      </div>
    )
  }

  const all_approved = approved_scenes.size === scenes.length
  const is_scene_approved = selected_scene != null && approved_scenes.has(selected_scene.scene_index)

  return (
    <section className="scenario-inspector" aria-label="Scenario narration review">
      <header className="scenario-inspector__header">
        <p className="production-dashboard__eyebrow">Scenario review</p>
        <h2 className="production-dashboard__section-title">
          Narration inspector — {scenes.length} scenes
        </h2>
        <p className="scenario-inspector__hint">
          Click or press Tab on a paragraph to edit. Press Enter to save, Shift+Enter for a new line,
          or Ctrl+Z to revert.
        </p>
      </header>

      {selected_scene && (
        <div className="scenario-inspector__single">
          <InlineNarrationEditor
            is_active={active_index === selected_scene.scene_index}
            mutation={mutation}
            on_activate={() => set_active_index(selected_scene.scene_index)}
            on_deactivate={() => set_active_index(null)}
            on_save_success={() => revoke_scene_approval(selected_scene.scene_index)}
            scene={selected_scene}
          />
          <div className="scenario-inspector__scene-actions">
            <button
              type="button"
              className="scenario-inspector__scene-approve"
              data-approved={is_scene_approved ? 'true' : undefined}
              onClick={() => toggle_scene_approval(selected_scene.scene_index)}
              aria-label={
                is_scene_approved
                  ? `Unapprove scene ${selected_scene.scene_index + 1}`
                  : `Approve scene ${selected_scene.scene_index + 1}`
              }
            >
              {is_scene_approved ? 'Approved ✓' : 'Approve scene'}
            </button>
          </div>
        </div>
      )}

      <footer className="scenario-inspector__footer">
        <span className="scenario-inspector__progress">
          {approved_scenes.size} / {scenes.length} approved
        </span>
        <button
          type="button"
          className="scenario-inspector__approve"
          onClick={() => approve_mutation.mutate()}
          disabled={approve_mutation.isPending || !all_approved}
          aria-label="Approve scenario and advance to character pick"
          title={!all_approved ? `Approve all ${scenes.length} scenes first` : undefined}
        >
          {approve_mutation.isPending ? 'Approving…' : 'Approve Story'}
        </button>
        {approve_mutation.isError ? (
          <p className="scenario-inspector__approve-error" role="alert">
            {approve_mutation.error?.message ?? 'Failed to approve story.'}
          </p>
        ) : null}
      </footer>
    </section>
  )
}
