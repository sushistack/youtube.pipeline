import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  fetchSettings,
  resetSettingsToDefaults,
  updateSettings,
} from '../lib/apiClient'
import { queryKeys } from '../lib/queryKeys'
import type { SettingsConfig } from '../contracts/settingsContracts'

/**
 * useSettingsQuery fetches `{snapshot, etag}` and polls every 5s while
 * mounted so the embedded BudgetIndicator reflects live run spend without a
 * separate cost subscription (D5). The poll is cheap because settings rarely
 * change — react-query dedupes identical responses.
 */
export function useSettingsQuery() {
  return useQuery({
    queryKey: queryKeys.settings.detail(),
    queryFn: fetchSettings,
    staleTime: 2_000,
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
  })
}

export function useSettingsMutation() {
  const query_client = useQueryClient()

  return useMutation({
    mutationFn: (payload: {
      config: SettingsConfig
      env: Record<string, string | null>
      etag?: string | null
    }) => updateSettings(payload),
    onSuccess: (snapshot) => {
      // Invalidate so the next refetch refreshes the ETag alongside the data.
      query_client.invalidateQueries({ queryKey: queryKeys.settings.detail() })
      // Optimistic cache write keeps the UI responsive between invalidate
      // and the refetch.
      query_client.setQueryData(queryKeys.settings.detail(), {
        snapshot,
        etag: snapshot.application.effective_version != null
          ? `"${snapshot.application.effective_version}"`
          : null,
      })
    },
  })
}

export function useSettingsResetMutation() {
  const query_client = useQueryClient()
  return useMutation({
    mutationFn: () => resetSettingsToDefaults(),
    onSuccess: () => {
      query_client.invalidateQueries({ queryKey: queryKeys.settings.detail() })
    },
  })
}
