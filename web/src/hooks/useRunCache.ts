import { useQuery } from '@tanstack/react-query'
import { fetchRunCache } from '../lib/apiClient'
import { queryKeys } from '../lib/queryKeys'

/**
 * useRunCache fetches the list of deterministic-agent cache files present on
 * disk for a pending run. The cache panel only appears at pending state, so
 * the hook is gated on `run_id != null && run_status === 'pending'` — once
 * Phase A starts the engine writes/refreshes caches, and we don't want to
 * poll them during execution.
 *
 * Mirrors the shape of useRunScenes (single useQuery, sentinel queryKey
 * when disabled, 30s staleTime). Caller: ProductionShell.renderPendingDetail.
 */
export function useRunCache(run_id: string | null, run_status: string | null) {
  const enabled = run_id != null && run_status === 'pending'
  return useQuery({
    enabled,
    queryFn: () => fetchRunCache(run_id!),
    queryKey: enabled ? queryKeys.runs.cache(run_id!) : queryKeys.runs.statusNone,
    staleTime: 30_000,
  })
}
