import { useQuery } from '@tanstack/react-query'
import { fetchRunStatus } from '../lib/apiClient'
import { isRunPollable } from '../lib/formatters'
import { queryKeys } from '../lib/queryKeys'

export function useRunStatus(run_id: string | null) {
  return useQuery({
    enabled: Boolean(run_id),
    placeholderData: (previous) => previous,
    queryFn: () => fetchRunStatus(run_id!),
    queryKey: run_id ? queryKeys.runs.status(run_id) : queryKeys.runs.statusNone,
    refetchInterval: (query) => {
      const status = query.state.data?.run.status
      return status && isRunPollable(status) ? 5000 : false
    },
  })
}
