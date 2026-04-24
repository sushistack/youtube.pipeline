import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  addGoldenPair,
  fetchCalibration,
  fetchCriticPrompt,
  fetchGoldenState,
  runFastFeedback,
  runGolden,
  runShadow,
  saveCriticPrompt,
} from '../lib/apiClient'
import { queryKeys } from '../lib/queryKeys'

/**
 * Story 10.2 Tuning hooks.
 *
 * The underlying endpoints are synchronous from the UI's perspective
 * (handlers block until evaluator calls return). Golden, Shadow, and
 * Fast Feedback are therefore modeled as one-shot mutations rather than
 * queries with a refetch knob — operators trigger them explicitly.
 */

export function useCriticPromptQuery() {
  return useQuery({
    queryKey: queryKeys.tuning.prompt(),
    queryFn: fetchCriticPrompt,
    staleTime: 10_000,
  })
}

export function useCriticPromptMutation() {
  const query_client = useQueryClient()
  return useMutation({
    mutationFn: (body: string) => saveCriticPrompt(body),
    onSuccess: (envelope) => {
      query_client.setQueryData(queryKeys.tuning.prompt(), envelope)
      // Freshness warnings depend on prompt hash; re-fetch golden so the
      // "prompt changed since last Golden" banner updates.
      query_client.invalidateQueries({ queryKey: queryKeys.tuning.golden() })
    },
  })
}

export function useGoldenStateQuery() {
  return useQuery({
    queryKey: queryKeys.tuning.golden(),
    queryFn: fetchGoldenState,
    staleTime: 5_000,
  })
}

export function useGoldenRunMutation() {
  const query_client = useQueryClient()
  return useMutation({
    mutationFn: () => runGolden(),
    onSuccess: () => {
      // Success path refreshes manifest (prompt_hash, last_report).
      query_client.invalidateQueries({ queryKey: queryKeys.tuning.golden() })
    },
  })
}

export function useGoldenAddPairMutation() {
  const query_client = useQueryClient()
  return useMutation({
    mutationFn: ({ positive, negative }: { positive: File; negative: File }) =>
      addGoldenPair(positive, negative),
    onSuccess: () => {
      query_client.invalidateQueries({ queryKey: queryKeys.tuning.golden() })
    },
  })
}

export function useShadowRunMutation() {
  return useMutation({
    mutationFn: () => runShadow(),
  })
}

export function useFastFeedbackMutation() {
  return useMutation({
    mutationFn: () => runFastFeedback(),
  })
}

export function useCalibrationQuery(params: { window?: number; limit?: number }) {
  return useQuery({
    queryKey: queryKeys.tuning.calibration(params),
    queryFn: () => fetchCalibration(params),
    staleTime: 30_000,
  })
}
