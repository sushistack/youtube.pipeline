import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { runStatusResponseSchema } from '../contracts/runContracts'
import { fetchRunStatus } from '../lib/apiClient'
import { isRunPollable, type RunStatusPayload } from '../lib/formatters'
import { queryKeys } from '../lib/queryKeys'

export function useRunStatus(run_id: string | null) {
  const queryClient = useQueryClient()

  useEffect(() => {
    if (!run_id) return
    const es = new EventSource(`/api/runs/${encodeURIComponent(run_id)}/status/stream`)

    es.onmessage = (event) => {
      try {
        const payload = runStatusResponseSchema.parse(JSON.parse(event.data))
        queryClient.setQueryData<RunStatusPayload>(queryKeys.runs.status(run_id), payload.data)
      } catch (e) {
        console.warn('[useRunStatus] SSE parse error — falling back to poll', e)
      }
    }

    es.addEventListener('done', () => es.close())
    // Do NOT close on error: let EventSource auto-reconnect (browser default).
    // Explicit close only on the 'done' sentinel so a clean terminal exit
    // does not trigger an unwanted reconnect loop.

    return () => es.close()
  }, [run_id, queryClient])

  return useQuery({
    enabled: Boolean(run_id),
    placeholderData: (previous) => previous,
    queryFn: () => fetchRunStatus(run_id!),
    queryKey: run_id ? queryKeys.runs.status(run_id) : queryKeys.runs.statusNone,
    // Fallback poll: SSE covers normal updates but a dropped connection or
    // parse error would freeze the UI without this safety net.
    refetchInterval: (query) => {
      const status = query.state.data?.run.status
      return status && isRunPollable(status) ? 3_000 : false
    },
  })
}
