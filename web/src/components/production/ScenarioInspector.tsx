import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEditNarration, useRunScenes } from '../../hooks/useRunScenes'
import { InlineNarrationEditor } from './InlineNarrationEditor'
import { approveScenarioReview, ApiClientError } from '../../lib/apiClient'
import type { RunSummary } from '../../contracts/runContracts'
import { queryKeys } from '../../lib/queryKeys'

interface ScenarioInspectorProps {
  run_id: string
}

export function ScenarioInspector({ run_id }: ScenarioInspectorProps) {
  const scenes_query = useRunScenes(run_id)
  const mutation = useEditNarration(run_id)
  const [active_index, set_active_index] = useState<number | null>(null)
  const query_client = useQueryClient()
  const approve_mutation = useMutation<RunSummary, ApiClientError>({
    mutationFn: () => approveScenarioReview(run_id),
    onSuccess: () => {
      // Same narrow invalidation as CharacterPick: list + status + detail.
      // The run flips to character_pick/waiting so this inspector unmounts
      // and the CharacterPick panel mounts on the next render.
      query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.status(run_id) })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.detail(run_id) })
    },
  })

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

  if (scenes.length === 0) {
    return (
      <div className="scenario-inspector__empty">
        No narration scenes found for this run.
      </div>
    )
  }

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

      <ol className="scenario-inspector__list">
        {scenes.map((scene) => (
          <li key={scene.scene_index} className="scenario-inspector__item">
            <InlineNarrationEditor
              is_active={active_index === scene.scene_index}
              mutation={mutation}
              on_activate={() => set_active_index(scene.scene_index)}
              on_deactivate={() => set_active_index(null)}
              scene={scene}
            />
          </li>
        ))}
      </ol>

      <footer className="scenario-inspector__footer">
        <button
          type="button"
          className="scenario-inspector__approve"
          onClick={() => approve_mutation.mutate()}
          disabled={approve_mutation.isPending}
          aria-label="Approve scenario and advance to character pick"
        >
          {approve_mutation.isPending ? 'Approving…' : 'Approve scenario'}
        </button>
        {approve_mutation.isError ? (
          <p className="scenario-inspector__approve-error" role="alert">
            {approve_mutation.error?.message ?? 'Failed to approve scenario.'}
          </p>
        ) : null}
      </footer>
    </section>
  )
}
