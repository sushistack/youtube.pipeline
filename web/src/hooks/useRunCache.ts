import { useQuery } from '@tanstack/react-query'
import { fetchRunCache } from '../lib/apiClient'
import { isPhaseAEntryStage, type RunStage } from '../lib/formatters'
import { queryKeys } from '../lib/queryKeys'

/**
 * useRunCache fetches the list of deterministic-agent cache files present on
 * disk for a run. Enabled at every state where the operator can re-enter
 * Phase A and therefore wants per-cache keep/drop control:
 *
 *  - status === 'pending' — fresh start or post-scenario-rewind. Caller
 *    surfaces the panel on the pending detail card.
 *  - status ∈ {'failed','cancelled'} AND stage is a Phase A entry stage —
 *    the failure banner surfaces the same panel so the operator can drop
 *    caches before clicking Resume. Phase B/C failures don't consult
 *    `_cache/`, so the panel stays hidden there.
 *
 * Once Phase A is running, the engine writes/refreshes caches itself, so
 * polling is wasted; the gate excludes those states.
 *
 * Mirrors the shape of useRunScenes (single useQuery, sentinel queryKey when
 * disabled, 30s staleTime).
 */
export function useRunCache(
  run_id: string | null,
  run_status: string | null,
  run_stage: RunStage | null = null,
) {
  const is_pending = run_status === 'pending'
  const is_failed_phase_a =
    (run_status === 'failed' || run_status === 'cancelled') &&
    isPhaseAEntryStage(run_stage)
  const enabled = run_id != null && (is_pending || is_failed_phase_a)
  return useQuery({
    enabled,
    queryFn: () => fetchRunCache(run_id!),
    queryKey: enabled ? queryKeys.runs.cache(run_id!) : queryKeys.runs.statusNone,
    staleTime: 30_000,
  })
}
