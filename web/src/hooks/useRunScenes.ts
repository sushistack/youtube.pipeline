import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Scene } from '../contracts/runContracts'
import { ApiClientError, editSceneNarration, fetchRunScenes } from '../lib/apiClient'
import { queryKeys } from '../lib/queryKeys'

export function useRunScenes(run_id: string | null) {
  return useQuery({
    enabled: run_id != null,
    queryFn: () => fetchRunScenes(run_id!),
    queryKey: run_id != null ? queryKeys.runs.scenes(run_id) : queryKeys.runs.statusNone,
    staleTime: 30_000,
  })
}

export function useEditNarration(run_id: string) {
  const client = useQueryClient()
  return useMutation<Scene, ApiClientError, { narration: string; scene_index: number }>({
    mutationFn: ({ narration, scene_index }: { narration: string; scene_index: number }) =>
      editSceneNarration(run_id, scene_index, narration),
    onSuccess: (saved, { scene_index }) => {
      client.setQueryData<Scene[]>(
        queryKeys.runs.scenes(run_id),
        (old) => old?.map((s) => (s.scene_index === scene_index ? saved : s)),
      )
      client.invalidateQueries({ queryKey: queryKeys.runs.scenes(run_id) })
    },
  })
}
