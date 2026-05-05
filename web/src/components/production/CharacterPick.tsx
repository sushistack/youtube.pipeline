import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  CharacterCandidate,
  CharacterGroup,
  DescriptorPrefill,
  RunSummary,
  ScpCanonicalImage,
} from '../../contracts/runContracts'
import {
  ApiClientError,
  fetchCharacterCandidates,
  fetchDescriptorPrefill,
  fetchScpCanonical,
  generateScpCanonical,
  pickCharacterWithDescriptor,
  searchCharacterCandidates,
} from '../../lib/apiClient'
import { queryKeys } from '../../lib/queryKeys'
import { useUIStore } from '../../stores/useUIStore'
import { VisionDescriptorEditor } from './VisionDescriptorEditor'

interface CharacterPickProps {
  run: RunSummary
}

// 'reuse' is the entry phase when the SCP_ID has a canonical in the library:
// the operator can adopt it as-is, regenerate it, or override the reference
// with a fresh DDG search. 'preview' is the post-descriptor stage in the miss
// flow — the canonical has been generated but /pick has not fired yet, so
// the operator can still regenerate or back out before committing the stage
// advance.
type Phase = 'reuse' | 'search' | 'grid' | 'descriptor' | 'preview'

function digitToIndex(digit: string): number | null {
  if (digit === '0') return 9
  if (digit >= '1' && digit <= '9') return Number.parseInt(digit, 10) - 1
  return null
}

interface CharacterGridInternalProps {
  candidates: CharacterCandidate[]
  selected_candidate_id: string | null
  on_select: (candidate_id: string) => void
  on_confirm: () => void
  on_escape: () => void
}

function CharacterGrid({
  candidates,
  selected_candidate_id,
  on_select,
  on_confirm,
  on_escape,
}: CharacterGridInternalProps) {
  const container_ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    container_ref.current?.focus()
  }, [])

  const handle_key_down = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        on_escape()
        return
      }
      if (e.key === 'Enter') {
        e.preventDefault()
        on_confirm()
        return
      }
      const idx = digitToIndex(e.key)
      if (idx != null && idx < candidates.length) {
        e.preventDefault()
        on_select(candidates[idx].id)
      }
    },
    [candidates, on_confirm, on_escape, on_select],
  )

  return (
    <div
      aria-label="Character candidate grid"
      className="character-grid"
      data-testid="character-grid"
      onKeyDown={handle_key_down}
      ref={container_ref}
      role="listbox"
      tabIndex={0}
    >
      {candidates.map((candidate, idx) => {
        const label = idx === 9 ? '0' : String(idx + 1)
        const is_selected = selected_candidate_id === candidate.id
        return (
          <button
            aria-selected={is_selected}
            className={
              'character-grid__cell' +
              (is_selected ? ' character-grid__cell--selected' : '')
            }
            data-candidate-id={candidate.id}
            data-testid={`character-grid-cell-${label}`}
            key={candidate.id}
            onClick={() => on_select(candidate.id)}
            role="option"
            type="button"
          >
            <img
              alt={candidate.title ?? `Candidate ${label}`}
              className="character-grid__image"
              src={candidate.preview_url ?? candidate.image_url}
            />
            <span className="character-grid__label">{label}</span>
          </button>
        )
      })}
    </div>
  )
}

export function CharacterPick({ run }: CharacterPickProps) {
  const query_client = useQueryClient()
  const push_undo_command = useUIStore((s) => s.push_undo_command)

  // Canonical lookup decides initial phase: hit → 'reuse', miss → existing
  // search/grid flow. 404 is a normal outcome (no canonical yet); we surface
  // it as null data, not as a query error, so the UI doesn't render an error
  // banner for the expected first-time case.
  const canonical_query = useQuery<ScpCanonicalImage | null, ApiClientError>({
    queryFn: async () => {
      try {
        const result = await fetchScpCanonical(run.id)
        return result
      } catch (e) {
        if (e instanceof ApiClientError && e.status === 404) return null
        throw e
      }
    },
    queryKey: queryKeys.runs.canonical(run.id),
    staleTime: 30_000,
    retry: false,
  })

  // Phase is derived: the canonical query decides the entry phase (reuse vs.
  // search/grid), and a navigation override takes over once the operator
  // drives a transition (e.g. "Search a different reference"). Deriving
  // instead of mirroring with useEffect avoids cascading-render lint and
  // keeps loading states stable: while the canonical query is pending we
  // return null, which renders the spinner without flicker.
  const [phase_override, set_phase] = useState<Phase | null>(null)
  const phase: Phase | null = useMemo(() => {
    if (phase_override !== null) return phase_override
    if (canonical_query.isPending) return null
    if (canonical_query.data) return 'reuse'
    return run.character_query_key ? 'grid' : 'search'
  }, [
    phase_override,
    canonical_query.data,
    canonical_query.isPending,
    run.character_query_key,
  ])

  const [query_input, set_query_input] = useState(run.character_query_key ?? '')
  const [selected_candidate_id, set_selected_candidate_id] = useState<string | null>(
    run.selected_character_id ?? null,
  )
  const current_descriptor_ref = useRef<string>('')
  const preload_last_ref = useRef<CharacterGroup | undefined>(undefined)

  const candidates_query = useQuery<CharacterGroup, ApiClientError>({
    enabled: phase === 'grid' && Boolean(run.character_query_key),
    queryFn: () => fetchCharacterCandidates(run.id),
    queryKey: queryKeys.runs.characters(run.id),
    staleTime: 60_000,
    retry: false,
  })

  const descriptor_query = useQuery<DescriptorPrefill, ApiClientError>({
    enabled: phase === 'descriptor',
    queryFn: () => fetchDescriptorPrefill(run.id),
    queryKey: queryKeys.runs.descriptor(run.id),
    staleTime: 60_000,
  })

  const search_mutation = useMutation<CharacterGroup, ApiClientError, string>({
    mutationFn: (q) => searchCharacterCandidates(run.id, q),
    onSuccess: (group) => {
      query_client.setQueryData(queryKeys.runs.characters(run.id), group)
      set_phase('grid')
    },
  })

  const generate_mutation = useMutation<
    ScpCanonicalImage,
    ApiClientError,
    { regenerate: boolean; candidate_id?: string; frozen_descriptor?: string }
  >({
    mutationFn: (opts) => generateScpCanonical(run.id, opts),
    onSuccess: (rec) => {
      query_client.setQueryData(queryKeys.runs.canonical(run.id), rec)
    },
  })

  const pick_mutation = useMutation<
    RunSummary,
    ApiClientError,
    { candidate_id: string; frozen_descriptor: string }
  >({
    mutationFn: ({ candidate_id, frozen_descriptor }) =>
      pickCharacterWithDescriptor(run.id, candidate_id, frozen_descriptor),
    onSuccess: (_result, variables) => {
      const prev_descriptor = run.frozen_descriptor ?? ''
      if (variables.frozen_descriptor !== prev_descriptor) {
        push_undo_command({
          command_id: `${run.id}-descriptor_edit-${Date.now()}`,
          run_id: run.id,
          kind: 'descriptor_edit',
          focus_target: 'descriptor',
          created_at: new Date().toISOString(),
        })
      }
      query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.status(run.id) })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.detail(run.id) })
      query_client.removeQueries({ queryKey: queryKeys.runs.characters(run.id) })
      query_client.removeQueries({ queryKey: queryKeys.runs.descriptor(run.id) })
      query_client.removeQueries({ queryKey: queryKeys.runs.canonical(run.id) })
    },
  })

  useEffect(() => {
    if (!candidates_query.data) return
    if (preload_last_ref.current === candidates_query.data) return
    preload_last_ref.current = candidates_query.data
    for (const c of candidates_query.data.candidates) {
      const img = new Image()
      img.src = c.preview_url ?? c.image_url
    }
  }, [candidates_query.data])

  if (
    phase === 'grid' &&
    candidates_query.isError &&
    candidates_query.error?.status === 404
  ) {
    set_phase('search')
  }

  const candidates = candidates_query.data?.candidates ?? []

  const descriptor_prefill = useMemo(() => {
    if (!descriptor_query.data) return ''
    const { auto, prior } = descriptor_query.data
    return prior != null && prior !== '' ? prior : auto
  }, [descriptor_query.data])

  const handle_search_submit = useCallback(
    (e: React.FormEvent<HTMLFormElement>) => {
      e.preventDefault()
      if (search_mutation.isPending) return
      const trimmed = query_input.trim()
      if (trimmed === '') return
      search_mutation.mutate(trimmed)
    },
    [query_input, search_mutation],
  )

  const handle_grid_confirm = useCallback(() => {
    if (!selected_candidate_id) return
    set_phase('descriptor')
  }, [selected_candidate_id])

  const handle_grid_escape = useCallback(() => {
    set_selected_candidate_id(null)
    set_phase('search')
  }, [])

  const handle_descriptor_change = useCallback((v: string) => {
    current_descriptor_ref.current = v
  }, [])

  // Miss-flow descriptor confirm: trigger canonical generation with the picked
  // candidate + descriptor as overrides (run.selected_character_id is still
  // null at this point — /pick has not been called). On success we move to
  // the preview phase so the operator can review before committing.
  const handle_descriptor_confirm_to_preview = useCallback(() => {
    if (!selected_candidate_id) return
    if (generate_mutation.isPending) return
    const edited = current_descriptor_ref.current.trim()
    const value = edited !== '' ? edited : descriptor_prefill.trim()
    if (value === '') return
    generate_mutation.mutate(
      {
        regenerate: false,
        candidate_id: selected_candidate_id,
        frozen_descriptor: value,
      },
      {
        onSuccess: () => set_phase('preview'),
      },
    )
  }, [descriptor_prefill, generate_mutation, selected_candidate_id])

  const handle_preview_regenerate = useCallback(() => {
    if (!selected_candidate_id) return
    if (generate_mutation.isPending) return
    const edited = current_descriptor_ref.current.trim()
    const value = edited !== '' ? edited : descriptor_prefill.trim()
    generate_mutation.mutate({
      regenerate: true,
      candidate_id: selected_candidate_id,
      frozen_descriptor: value,
    })
  }, [descriptor_prefill, generate_mutation, selected_candidate_id])

  const handle_preview_confirm = useCallback(() => {
    if (!selected_candidate_id) return
    if (pick_mutation.isPending) return
    const edited = current_descriptor_ref.current.trim()
    const value = edited !== '' ? edited : descriptor_prefill.trim()
    pick_mutation.mutate({
      candidate_id: selected_candidate_id,
      frozen_descriptor: value,
    })
  }, [descriptor_prefill, pick_mutation, selected_candidate_id])

  // Reuse-flow handlers. The canonical's source_candidate_id is the DDG row
  // selected in a prior run — it survives in character_search_cache as long
  // as that cache row is intact, so /pick on this fresh run can reuse it
  // without forcing the operator to redo the search.
  const canonical = canonical_query.data ?? null
  const handle_reuse_adopt = useCallback(() => {
    if (!canonical) return
    set_selected_candidate_id(canonical.source_candidate_id)
    set_phase('descriptor')
  }, [canonical])

  const handle_reuse_regenerate = useCallback(() => {
    if (generate_mutation.isPending) return
    if (!canonical) return
    generate_mutation.mutate({
      regenerate: true,
      candidate_id: canonical.source_candidate_id,
      frozen_descriptor: canonical.frozen_descriptor,
    })
  }, [canonical, generate_mutation])

  const handle_reuse_search_again = useCallback(() => {
    set_selected_candidate_id(null)
    set_phase('search')
  }, [])

  if (phase === null) {
    return (
      <section className="character-pick" aria-busy="true">
        <p className="character-pick__loading">Loading…</p>
      </section>
    )
  }

  return (
    <section
      aria-label="Character pick"
      className="character-pick"
      data-phase={phase}
    >
      <header className="character-pick__header">
        <p className="production-dashboard__eyebrow">Character reference</p>
        <h2 className="production-dashboard__section-title">
          Pick a reference &amp; confirm the Vision Descriptor
        </h2>
      </header>

      {phase === 'reuse' && canonical && (
        <div className="character-pick__reuse" data-testid="character-pick-reuse">
          <p className="character-pick__hint">
            Existing canonical found for {canonical.scp_id} (v{canonical.version}).
          </p>
          <img
            alt={`Canonical for ${canonical.scp_id}`}
            className="character-pick__canonical-preview"
            src={canonicalImageSrc(canonical)}
          />
          <div className="character-pick__reuse-actions">
            <button
              className="character-pick__primary"
              onClick={handle_reuse_adopt}
              type="button"
            >
              Use as-is
            </button>
            <button
              className="character-pick__secondary"
              disabled={generate_mutation.isPending}
              onClick={handle_reuse_regenerate}
              type="button"
            >
              {generate_mutation.isPending ? 'Regenerating…' : 'Regenerate'}
            </button>
            <button
              className="character-pick__secondary"
              onClick={handle_reuse_search_again}
              type="button"
            >
              Search a different reference
            </button>
          </div>
          {generate_mutation.isError && (
            <p className="character-pick__error" role="alert">
              {generate_mutation.error?.message ?? 'Regeneration failed.'}
            </p>
          )}
        </div>
      )}

      {phase === 'search' && (
        <form className="character-pick__search" onSubmit={handle_search_submit}>
          <label className="character-pick__label" htmlFor="character-pick-query">
            Search query
          </label>
          <input
            autoFocus
            className="character-pick__input"
            id="character-pick-query"
            name="query"
            onChange={(e) => set_query_input(e.target.value)}
            placeholder="e.g. SCP-049"
            type="text"
            value={query_input}
          />
          <button
            className="character-pick__submit"
            disabled={search_mutation.isPending}
            type="submit"
          >
            {search_mutation.isPending ? 'Searching…' : 'Search'}
          </button>
          {search_mutation.isError && (
            <p className="character-pick__error" role="alert">
              {search_mutation.error?.message ?? 'Search failed.'}
            </p>
          )}
        </form>
      )}

      {phase === 'grid' && (
        <>
          {candidates_query.isPending && (
            <p aria-busy="true" className="character-pick__loading">
              Loading candidates…
            </p>
          )}
          {candidates_query.isError && (
            <p className="character-pick__error" role="alert">
              {candidates_query.error?.message ?? 'Failed to load candidates.'}
            </p>
          )}
          {candidates.length > 0 && (
            <>
              <CharacterGrid
                candidates={candidates}
                on_confirm={handle_grid_confirm}
                on_escape={handle_grid_escape}
                on_select={set_selected_candidate_id}
                selected_candidate_id={selected_candidate_id}
              />
              <footer className="character-pick__grid-actions">
                <button
                  className="character-pick__secondary"
                  onClick={handle_grid_escape}
                  type="button"
                >
                  Search again
                </button>
                <p className="character-pick__hint character-pick__hint--inline">
                  1–9/0 select · Enter confirm · Esc search again
                </p>
                <button
                  className="character-pick__primary"
                  disabled={!selected_candidate_id}
                  onClick={handle_grid_confirm}
                  type="button"
                >
                  Confirm selection
                </button>
              </footer>
            </>
          )}
        </>
      )}

      {phase === 'descriptor' && (
        <>
          {descriptor_query.isPending && (
            <p aria-busy="true" className="character-pick__loading">
              Loading descriptor…
            </p>
          )}
          {descriptor_query.isError && (
            <p className="character-pick__error" role="alert">
              {descriptor_query.error?.message ?? 'Failed to load descriptor.'}
            </p>
          )}
          {descriptor_query.data && (
            <VisionDescriptorEditor
              is_submitting={generate_mutation.isPending}
              onConfirm={
                canonical_query.data
                  ? handle_preview_confirm
                  : handle_descriptor_confirm_to_preview
              }
              onDescriptorChange={handle_descriptor_change}
              prefill={descriptor_prefill}
            />
          )}
          {generate_mutation.isError && (
            <p className="character-pick__error" role="alert">
              {generate_mutation.error?.message ?? 'Failed to generate cartoon.'}
            </p>
          )}
        </>
      )}

      {phase === 'preview' && canonical && (
        <div className="character-pick__preview" data-testid="character-pick-preview">
          <p className="character-pick__hint">
            Cartoon canonical generated. Confirm to advance, or regenerate to try a
            different seed.
          </p>
          <img
            alt={`Canonical for ${canonical.scp_id}`}
            className="character-pick__canonical-preview"
            src={canonicalImageSrc(canonical)}
          />
          <div className="character-pick__preview-actions">
            <button
              className="character-pick__secondary"
              disabled={generate_mutation.isPending}
              onClick={handle_preview_regenerate}
              type="button"
            >
              {generate_mutation.isPending ? 'Regenerating…' : 'Regenerate'}
            </button>
            <button
              className="character-pick__primary"
              disabled={pick_mutation.isPending}
              onClick={handle_preview_confirm}
              type="button"
            >
              {pick_mutation.isPending ? 'Advancing…' : 'Confirm & continue'}
            </button>
          </div>
          {pick_mutation.isError && (
            <p className="character-pick__error" role="alert">
              {pick_mutation.error?.message ?? 'Failed to confirm pick.'}
            </p>
          )}
        </div>
      )}
    </section>
  )
}

// canonicalImageSrc appends the library row version as a query string so the
// browser refetches the image after a regenerate. The static-serve handler
// sets Cache-Control: no-cache, but some browsers skip the revalidation
// roundtrip entirely when the <img src> attribute is byte-identical to the
// previous render — version-busting forces a fresh GET on every version bump.
function canonicalImageSrc(canonical: ScpCanonicalImage): string {
  return `${canonical.image_url}?v=${canonical.version}`
}

