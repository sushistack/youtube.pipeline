import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { runStatusResponseSchema } from '../contracts/runContracts'
import { fetchRunStatus } from '../lib/apiClient'
import { isRunPollable, type RunStatusPayload } from '../lib/formatters'
import { queryKeys } from '../lib/queryKeys'

export function useRunStatus(run_id: string | null) {
  const queryClient = useQueryClient()
  const [sse_healthy, setSseHealthy] = useState(true)

  useEffect(() => {
    if (!run_id) return
    setSseHealthy(true)
    const es = new EventSource(`/api/runs/${encodeURIComponent(run_id)}/status/stream`)

    es.onopen = () => setSseHealthy(true)

    es.onmessage = (event) => {
      try {
        const payload = runStatusResponseSchema.parse(JSON.parse(event.data))
        queryClient.setQueryData<RunStatusPayload>(queryKeys.runs.status(run_id), payload.data)
        setSseHealthy(true)
      } catch (e) {
        console.warn('[useRunStatus] SSE parse error — falling back to poll', e)
        setSseHealthy(false)
      }
    }

    // Do NOT close on error: let EventSource auto-reconnect (browser default).
    // Mark unhealthy while the connection is down or retrying so the polling
    // fallback covers the gap; onopen/onmessage flip it back when SSE returns.
    es.onerror = () => {
      if (es.readyState !== EventSource.OPEN) setSseHealthy(false)
    }

    es.addEventListener('done', () => es.close())

    return () => es.close()
  }, [run_id, queryClient])

  return useQuery({
    enabled: Boolean(run_id),
    placeholderData: (previous) => previous,
    queryFn: () => fetchRunStatus(run_id!),
    queryKey: run_id ? queryKeys.runs.status(run_id) : queryKeys.runs.statusNone,
    // Fallback poll only when SSE is down. Healthy SSE → no polling.
    refetchInterval: (query) => {
      if (sse_healthy) return false
      const status = query.state.data?.run.status
      return status && isRunPollable(status) ? 3_000 : false
    },
  })
}
