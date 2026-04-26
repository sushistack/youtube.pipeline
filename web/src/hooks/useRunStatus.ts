import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { runStatusResponseSchema } from '../contracts/runContracts'
import { fetchRunStatus } from '../lib/apiClient'
import { type RunStatusPayload } from '../lib/formatters'
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
      } catch {
        // ignore parse errors
      }
    }

    es.addEventListener('done', () => es.close())
    es.onerror = () => es.close()

    return () => es.close()
  }, [run_id, queryClient])

  return useQuery({
    enabled: Boolean(run_id),
    placeholderData: (previous) => previous,
    queryFn: () => fetchRunStatus(run_id!),
    queryKey: run_id ? queryKeys.runs.status(run_id) : queryKeys.runs.statusNone,
  })
}
