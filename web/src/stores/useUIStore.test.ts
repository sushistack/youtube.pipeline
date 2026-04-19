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

    expect(useUIStore.getState().sidebar_collapsed).toBe(false)

    useUIStore.getState().toggle_sidebar()

    expect(useUIStore.getState().sidebar_collapsed).toBe(true)
  })

  it('hydrates sidebar_collapsed from persisted state', async () => {
    localStorage.setItem(
      UI_STORE_PERSIST_KEY,
      JSON.stringify({
        state: {
          sidebar_collapsed: true,
        },
        version: 0,
      }),
    )

    const { useUIStore } = await import('./useUIStore')
    await useUIStore.persist.rehydrate()

    expect(useUIStore.getState().sidebar_collapsed).toBe(true)
  })

  it('serializes only whitelisted keys under the configured persist key', async () => {
    const { useUIStore } = await import('./useUIStore')

    useUIStore.getState().set_sidebar_collapsed(true)

    expect(useUIStore.persist.getOptions().name).toBe(UI_STORE_PERSIST_KEY)
    expect(JSON.parse(localStorage.getItem(UI_STORE_PERSIST_KEY) ?? '{}')).toEqual({
      state: {
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
        sidebar_collapsed: false,
      },
      version: 0,
    })
  })
})
