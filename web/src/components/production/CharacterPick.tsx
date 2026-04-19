import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  CharacterCandidate,
  CharacterGroup,
  DescriptorPrefill,
  RunSummary,
} from '../../contracts/runContracts'
import {
  ApiClientError,
  fetchCharacterCandidates,
  fetchDescriptorPrefill,
  pickCharacterWithDescriptor,
  searchCharacterCandidates,
} from '../../lib/apiClient'
import { queryKeys } from '../../lib/queryKeys'
import { VisionDescriptorEditor } from './VisionDescriptorEditor'

interface CharacterPickProps {
  run: RunSummary
}

type Phase = 'search' | 'grid' | 'descriptor'

// Digit-to-candidate-index: 1–9 select positions 0–8, 0 selects position 9.
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

  // Autofocus the grid container so 1–9/0/Esc/Enter flow without a click.
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
  const initial_phase: Phase = run.character_query_key ? 'grid' : 'search'
  const [phase, set_phase] = useState<Phase>(initial_phase)
  // Initialize query_input from the run's stored query key so the input is
  // controlled from first render. Using `defaultValue` left the input showing
  // the prefilled text while internal state stayed '', causing the first
  // Enter/submit to silently no-op.
  const [query_input, set_query_input] = useState(run.character_query_key ?? '')
  const [selected_candidate_id, set_selected_candidate_id] = useState<string | null>(
    run.selected_character_id ?? null,
  )
  // current_descriptor_ref mirrors the editor's latest draft without forcing a
  // re-render on every keystroke. At confirm time we prefer the edited value
  // when non-empty, otherwise fall back to the prefill.
  const current_descriptor_ref = useRef<string>('')
  // Preload guard keyed on the candidate group identity — new search or cache
  // refresh yields a new CharacterGroup reference, which reopens the preload
  // window. A single boolean ref silently skipped preloading for every
  // post-first candidate set.
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

  const pick_mutation = useMutation<
    RunSummary,
    ApiClientError,
    { candidate_id: string; frozen_descriptor: string }
  >({
    mutationFn: ({ candidate_id, frozen_descriptor }) =>
      pickCharacterWithDescriptor(run.id, candidate_id, frozen_descriptor),
    onSuccess: () => {
      // Narrow invalidation: only the list + this run's status/detail need
      // to refetch. Using queryKeys.runs.all would refetch characters and
      // descriptor queries for a run that has just advanced past the
      // character_pick stage — those calls would then 404 or return stale
      // data while the component unmounts. We also remove the now-consumed
      // character/descriptor caches so a future re-entry starts clean.
      query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.status(run.id) })
      query_client.invalidateQueries({ queryKey: queryKeys.runs.detail(run.id) })
      query_client.removeQueries({ queryKey: queryKeys.runs.characters(run.id) })
      query_client.removeQueries({ queryKey: queryKeys.runs.descriptor(run.id) })
    },
  })

  // Image preloading — fires each time a new candidate group arrives (keyed
  // on the group reference itself, not a one-shot boolean). TanStack Query v5
  // removed onSuccess from useQuery options, so this is the canonical pattern
  // and it survives re-searches that produce fresh candidate sets.
  useEffect(() => {
    if (!candidates_query.data) return
    if (preload_last_ref.current === candidates_query.data) return
    preload_last_ref.current = candidates_query.data
    for (const c of candidates_query.data.candidates) {
      const img = new Image()
      img.src = c.preview_url ?? c.image_url
    }
  }, [candidates_query.data])

  // Cache-fallback 404 recovery: when we auto-loaded the grid based on
  // run.character_query_key but the cache row has been evicted, the server
  // returns 404 and the UI would otherwise be stranded at phase='grid' with
  // no input to recover. Fall back to the search phase during render (the
  // React-recommended pattern for deriving state from props/query results)
  // so the operator can re-issue a query.
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
      // Guard against double-submit from repeat Enter presses while the
      // DDG search mutation is in flight.
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
    // Clearing the selection on Esc prevents a stale candidate ID (from a
    // prior search) from leaking into a fresh pick after the operator
    // re-searches with a different query.
    set_selected_candidate_id(null)
    set_phase('search')
  }, [])

  const handle_descriptor_change = useCallback((v: string) => {
    current_descriptor_ref.current = v
  }, [])

  const handle_descriptor_confirm = useCallback(() => {
    if (!selected_candidate_id) return
    // Guard against double-Enter landing while pick is already in flight.
    if (pick_mutation.isPending) return
    const edited = current_descriptor_ref.current.trim()
    const value = edited !== '' ? edited : descriptor_prefill.trim()
    pick_mutation.mutate({
      candidate_id: selected_candidate_id,
      frozen_descriptor: value,
    })
  }, [descriptor_prefill, pick_mutation, selected_candidate_id])

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
              <p className="character-pick__hint">
                Press 1–9 or 0 to select, Enter to confirm, Esc to search again.
              </p>
              <CharacterGrid
                candidates={candidates}
                on_confirm={handle_grid_confirm}
                on_escape={handle_grid_escape}
                on_select={set_selected_candidate_id}
                selected_candidate_id={selected_candidate_id}
              />
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
              is_submitting={pick_mutation.isPending}
              onConfirm={handle_descriptor_confirm}
              onDescriptorChange={handle_descriptor_change}
              prefill={descriptor_prefill}
            />
          )}
          {pick_mutation.isError && (
            <p className="character-pick__error" role="alert">
              {pick_mutation.error?.message ?? 'Failed to confirm pick.'}
            </p>
          )}
        </>
      )}
    </section>
  )
}
