import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { RunSummary } from '../../contracts/runContracts'
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { useRunCache } from '../../hooks/useRunCache'
import { resumeRun } from '../../lib/apiClient'
import { formatCurrency, isPhaseAEntryStage } from '../../lib/formatters'
import { queryKeys } from '../../lib/queryKeys'
import { CachePanel } from './CachePanel'

interface FailureBannerProps {
  on_dismiss: () => void
  run: RunSummary
}

function getFailureMessage(retry_reason: string | null | undefined) {
  if (retry_reason === 'rate_limit') {
    return 'DashScope rate limit - request throttled'
  }

  if (retry_reason) {
    return retry_reason
  }

  return 'Stage failed - check the run log for details'
}

export function FailureBanner({ on_dismiss, run }: FailureBannerProps) {
  const query_client = useQueryClient()
  // Cache panel surfaces only when the failed stage is a Phase A entry stage —
  // those are the only stages that consult `_cache/`. useRunCache's gate
  // mirrors the same predicate; this state-set lives here because a failed
  // run's resume body is the consumer.
  const cache_query = useRunCache(run.id, run.status, run.stage)
  const cache_entries = cache_query.data ?? []
  const show_cache_panel =
    isPhaseAEntryStage(run.stage) && cache_entries.length > 0
  const [dropped_cache_stages, set_dropped_cache_stages] = useState<Set<string>>(
    new Set(),
  )

  const toggle_drop_cache = (stage: string) => {
    set_dropped_cache_stages((previous) => {
      const next = new Set(previous)
      if (next.has(stage)) {
        next.delete(stage)
      } else {
        next.add(stage)
      }
      return next
    })
  }

  const resume_mutation = useMutation({
    mutationFn: () => {
      const drop_caches = Array.from(dropped_cache_stages)
      // Single-arg call when nothing is dropped: keeps the existing call shape
      // intact for the (overwhelming-majority) Phase B/C resume path and lets
      // legacy tests assert `toHaveBeenCalledWith(run_id)` without churning.
      return drop_caches.length > 0
        ? resumeRun(run.id, { drop_caches })
        : resumeRun(run.id)
    },
    onSuccess: () => {
      // status가 cancelled/failed → running/waiting 으로 바뀌면 부모 셸에서 배너 조건이
      // 자연스럽게 false가 되어 사라진다. on_dismiss를 같이 호출하면 부모의
      // dismissed_run_id에 run.id가 박혀, 같은 run이 다시 cancelled 됐을 때
      // 배너가 다시 뜨지 않는 cycle 버그가 발생.
      void query_client.invalidateQueries({ queryKey: queryKeys.runs.list() })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      })
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.cache(run.id),
      })
      // Drop the local keep/drop selection: if the same run later fails again
      // at the same stage, the previous unchecks should not silently re-drop.
      // Run-id changes are also handled by `key={run.id}` at the mount site,
      // but the in-place re-fail case lacks a re-mount and needs this reset.
      set_dropped_cache_stages(new Set())
    },
  })

  const is_resumable = run.status === 'failed' || run.status === 'cancelled'

  useKeyboardShortcuts(
    [
      {
        enabled: is_resumable && !resume_mutation.isPending,
        handler: () => {
          resume_mutation.mutate()
        },
        key: 'enter',
        prevent_default: true,
        scope: 'context',
      },
      {
        enabled: is_resumable,
        handler: () => {
          on_dismiss()
        },
        key: 'escape',
        prevent_default: true,
        scope: 'context',
      },
    ],
    { enabled: is_resumable },
  )

  if (!is_resumable) {
    return null
  }

  const is_cancelled = run.status === 'cancelled'
  const variant_class = is_cancelled
    ? 'failure-banner--cancelled'
    : run.retry_reason === 'rate_limit'
      ? 'failure-banner--retryable'
      : 'failure-banner--fatal'

  return (
    <section
      aria-label={is_cancelled ? 'Run cancelled recovery' : 'Run failure recovery'}
      className={`failure-banner ${variant_class}`}
      role="alert"
    >
      <div className="failure-banner__content">
        <p className="failure-banner__meta">
          {is_cancelled
            ? <><strong>Run cancelled</strong> · Spend <strong>{formatCurrency(run.cost_usd)}</strong></>
            : <><strong>Pipeline failed</strong> — {getFailureMessage(run.retry_reason)} · Spend <strong>{formatCurrency(run.cost_usd)}</strong></>
          }
        </p>
        {show_cache_panel ? (
          <CachePanel
            entries={cache_entries}
            dropped_stages={dropped_cache_stages}
            on_toggle={toggle_drop_cache}
            id_prefix="failure-cache-keep"
          />
        ) : null}
        <div className="failure-banner__actions">
          <button
            className="failure-banner__resume"
            disabled={resume_mutation.isPending}
            onClick={() => resume_mutation.mutate()}
            type="button"
          >
            <span className="failure-banner__shortcut">[Enter]</span>
            <span>{resume_mutation.isPending ? (is_cancelled ? 'Restarting...' : 'Resuming...') : (is_cancelled ? 'Restart' : 'Resume')}</span>
          </button>
          <button
            aria-label="Dismiss failure banner"
            className="failure-banner__dismiss"
            onClick={on_dismiss}
            type="button"
          >
            ×
          </button>
        </div>
        {resume_mutation.isError ? (
          <p className="failure-banner__error" role="status">
            Resume failed: {resume_mutation.error instanceof Error
              ? resume_mutation.error.message
              : 'Unknown error — try again or check the run log.'}
          </p>
        ) : null}
      </div>
    </section>
  )
}
