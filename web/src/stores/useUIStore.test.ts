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

  describe('undo stack', () => {
    it('starts with empty undo stacks', async () => {
      const { useUIStore } = await import('./useUIStore')
      expect(useUIStore.getState().undo_stacks).toEqual({})
    })

    it('push_undo_command appends commands in LIFO order', async () => {
      const { useUIStore } = await import('./useUIStore')
      const run_id = 'run-1'

      useUIStore.getState().push_undo_command({
        command_id: 'c1',
        run_id,
        kind: 'approve',
        scene_index: 0,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:00:00Z',
      })
      useUIStore.getState().push_undo_command({
        command_id: 'c2',
        run_id,
        kind: 'reject',
        scene_index: 1,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:01:00Z',
      })

      const stack = useUIStore.getState().undo_stacks[run_id]
      expect(stack).toHaveLength(2)
      expect(stack[0].command_id).toBe('c1')
      expect(stack[1].command_id).toBe('c2')
    })

    it('enforces 10-entry depth cap', async () => {
      const { useUIStore, UNDO_STACK_MAX_DEPTH } = await import('./useUIStore')
      const run_id = 'run-cap'

      for (let i = 0; i < 15; i++) {
        useUIStore.getState().push_undo_command({
          command_id: `cmd-${i}`,
          run_id,
          kind: 'approve',
          scene_index: i,
          focus_target: 'scene-card',
          created_at: new Date().toISOString(),
        })
      }

      const stack = useUIStore.getState().undo_stacks[run_id]
      expect(stack).toHaveLength(UNDO_STACK_MAX_DEPTH)
      // The last 10 should remain (newest).
      expect(stack[0].command_id).toBe('cmd-5')
      expect(stack[9].command_id).toBe('cmd-14')
    })

    it('pop_undo_command returns and removes the last entry', async () => {
      const { useUIStore } = await import('./useUIStore')
      const run_id = 'run-pop'

      useUIStore.getState().push_undo_command({
        command_id: 'first',
        run_id,
        kind: 'approve',
        scene_index: 0,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:00:00Z',
      })
      useUIStore.getState().push_undo_command({
        command_id: 'second',
        run_id,
        kind: 'reject',
        scene_index: 1,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:01:00Z',
      })

      const popped = useUIStore.getState().pop_undo_command(run_id)
      expect(popped?.command_id).toBe('second')
      expect(useUIStore.getState().undo_stacks[run_id]).toHaveLength(1)
      expect(useUIStore.getState().undo_stacks[run_id][0].command_id).toBe('first')
    })

    it('pop_undo_command returns null when stack is empty', async () => {
      const { useUIStore } = await import('./useUIStore')
      const result = useUIStore.getState().pop_undo_command('nonexistent-run')
      expect(result).toBeNull()
    })

    it('clear_undo_stack empties the run stack without affecting others', async () => {
      const { useUIStore } = await import('./useUIStore')

      useUIStore.getState().push_undo_command({
        command_id: 'a',
        run_id: 'run-a',
        kind: 'approve',
        scene_index: 0,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:00:00Z',
      })
      useUIStore.getState().push_undo_command({
        command_id: 'b',
        run_id: 'run-b',
        kind: 'reject',
        scene_index: 0,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:00:00Z',
      })

      useUIStore.getState().clear_undo_stack('run-a')

      expect(useUIStore.getState().undo_stacks['run-a']).toHaveLength(0)
      expect(useUIStore.getState().undo_stacks['run-b']).toHaveLength(1)
    })

    it('stacks are isolated per run_id', async () => {
      const { useUIStore } = await import('./useUIStore')

      useUIStore.getState().push_undo_command({
        command_id: 'r1-cmd',
        run_id: 'run-1',
        kind: 'approve',
        scene_index: 0,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:00:00Z',
      })

      expect(useUIStore.getState().undo_stacks['run-2']).toBeUndefined()
      expect(useUIStore.getState().undo_stacks['run-1']).toHaveLength(1)
    })

    it('pushes exactly one aggregate entry for approve_all_remaining (AC-4)', async () => {
      const { useUIStore } = await import('./useUIStore')
      const run_id = 'run-batch'
      const scene_indices = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]

      useUIStore.getState().push_undo_command({
        command_id: 'batch-1',
        aggregate_command_id: 'batch-1',
        run_id,
        kind: 'approve_all_remaining',
        scene_index: 0,
        scene_indices,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:00:00Z',
      })

      const stack = useUIStore.getState().undo_stacks[run_id]
      expect(stack).toHaveLength(1)
      expect(stack[0].kind).toBe('approve_all_remaining')
      expect(stack[0].aggregate_command_id).toBe('batch-1')
      expect(stack[0].scene_indices).toEqual(scene_indices)
    })

    it('undo_stacks are NOT persisted to localStorage', async () => {
      const { useUIStore } = await import('./useUIStore')

      useUIStore.getState().push_undo_command({
        command_id: 'x',
        run_id: 'run-persist-check',
        kind: 'approve',
        scene_index: 0,
        focus_target: 'scene-card',
        created_at: '2026-01-01T00:00:00Z',
      })

      const persisted = JSON.parse(localStorage.getItem(useUIStore.persist.getOptions().name ?? '') ?? '{}')
      expect(persisted.state?.undo_stacks).toBeUndefined()
    })
  })
})
