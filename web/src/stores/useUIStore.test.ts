import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { UI_STORE_PERSIST_KEY } from './useUIStore'

describe('useUIStore', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('uses the expected default values and toggle action', async () => {
    const { useUIStore } = await import('./useUIStore')

    expect(useUIStore.getState().onboarding_dismissed).toBe(false)
    expect(useUIStore.getState().production_last_seen).toEqual({})
    expect(useUIStore.getState().sidebar_collapsed).toBe(false)

    useUIStore.getState().toggle_sidebar()

    expect(useUIStore.getState().sidebar_collapsed).toBe(true)
  })

  it('hydrates sidebar_collapsed from persisted state', async () => {
    localStorage.setItem(
      UI_STORE_PERSIST_KEY,
      JSON.stringify({
        state: {
          onboarding_dismissed: true,
          production_last_seen: {
            'scp-173-run-4': {
              run_id: 'scp-173-run-4',
              stage: 'scenario_review',
              status: 'waiting',
              updated_at: '2026-04-19T00:07:00Z',
            },
          },
          sidebar_collapsed: true,
        },
        version: 0,
      }),
    )

    const { useUIStore } = await import('./useUIStore')
    await useUIStore.persist.rehydrate()

    expect(useUIStore.getState().onboarding_dismissed).toBe(true)
    expect(useUIStore.getState().production_last_seen).toEqual({
      'scp-173-run-4': {
        run_id: 'scp-173-run-4',
        stage: 'scenario_review',
        status: 'waiting',
        updated_at: '2026-04-19T00:07:00Z',
      },
    })
    expect(useUIStore.getState().sidebar_collapsed).toBe(true)
  })

  it('serializes only whitelisted keys under the configured persist key', async () => {
    const { useUIStore } = await import('./useUIStore')

    useUIStore.getState().dismiss_onboarding()
    useUIStore.getState().set_production_last_seen({
      run_id: 'scp-049-run-2',
      stage: 'write',
      status: 'running',
      updated_at: '2026-04-19T01:00:00Z',
    })
    useUIStore.getState().set_sidebar_collapsed(true)

    expect(useUIStore.persist.getOptions().name).toBe(UI_STORE_PERSIST_KEY)
    expect(JSON.parse(localStorage.getItem(UI_STORE_PERSIST_KEY) ?? '{}')).toEqual({
      state: {
        onboarding_dismissed: true,
        production_last_seen: {
          'scp-049-run-2': {
            run_id: 'scp-049-run-2',
            stage: 'write',
            status: 'running',
            updated_at: '2026-04-19T01:00:00Z',
          },
        },
        sidebar_collapsed: true,
      },
      version: 0,
    })
  })

  it('strips unknown keys from persisted state via partialize', async () => {
    localStorage.setItem(
      UI_STORE_PERSIST_KEY,
      JSON.stringify({
        state: {
          onboarding_dismissed: true,
          production_last_seen: {
            stale: {
              run_id: 'stale',
              stage: 'pending',
              status: 'pending',
              updated_at: '2026-04-19T00:00:00Z',
            },
          },
          sidebar_collapsed: true,
          legacy_unknown_flag: 'stale',
        },
        version: 0,
      }),
    )

    const { useUIStore } = await import('./useUIStore')
    await useUIStore.persist.rehydrate()

    useUIStore.getState().set_sidebar_collapsed(false)

    expect(JSON.parse(localStorage.getItem(UI_STORE_PERSIST_KEY) ?? '{}')).toEqual({
      state: {
        onboarding_dismissed: true,
        production_last_seen: {
          stale: {
            run_id: 'stale',
            stage: 'pending',
            status: 'pending',
            updated_at: '2026-04-19T00:00:00Z',
          },
        },
        sidebar_collapsed: false,
      },
      version: 0,
    })
  })

  it('stores production last-seen snapshots by run id', async () => {
    const { useUIStore } = await import('./useUIStore')

    useUIStore.getState().set_production_last_seen({
      run_id: 'scp-999-run-1',
      stage: 'character_pick',
      status: 'waiting',
      updated_at: '2026-04-19T02:00:00Z',
    })

    expect(useUIStore.getState().production_last_seen['scp-999-run-1']).toEqual({
      run_id: 'scp-999-run-1',
      stage: 'character_pick',
      status: 'waiting',
      updated_at: '2026-04-19T02:00:00Z',
    })
  })
})
