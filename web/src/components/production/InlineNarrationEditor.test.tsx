import { render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { UseMutationResult } from '@tanstack/react-query'
import type { Scene } from '../../contracts/runContracts'
import type { ApiClientError } from '../../lib/apiClient'
import { InlineNarrationEditor } from './InlineNarrationEditor'

type MutationResult = UseMutationResult<Scene, ApiClientError, { narration: string; scene_index: number }>

function buildMutation(overrides?: Partial<MutationResult>): MutationResult {
  return {
    data: undefined,
    error: null,
    failureCount: 0,
    failureReason: null,
    isError: false,
    isIdle: true,
    isPaused: false,
    isPending: false,
    isSuccess: false,
    mutate: vi.fn(),
    mutateAsync: vi.fn(),
    reset: vi.fn(),
    status: 'idle',
    submittedAt: 0,
    variables: undefined,
    context: undefined,
    ...overrides,
  } as unknown as MutationResult
}

const base_scene: Scene = {
  narration: '첫 번째 장면의 나레이션입니다.',
  scene_index: 0,
}

// ── Read mode ────────────────────────────────────────────────────────────────

describe('InlineNarrationEditor — read mode', () => {
  it('renders scene label and narration text', () => {
    const mutation = buildMutation()
    render(
      <InlineNarrationEditor
        is_active={false}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )
    expect(screen.getByText('Scene 1')).toBeInTheDocument()
    expect(screen.getByText(base_scene.narration)).toBeInTheDocument()
  })

  it('shows an empty placeholder when narration is blank', () => {
    const mutation = buildMutation()
    render(
      <InlineNarrationEditor
        is_active={false}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={{ ...base_scene, narration: '' }}
      />,
    )
    expect(screen.getByText('No narration')).toBeInTheDocument()
  })
})

// ── Edit mode activation ──────────────────────────────────────────────────────

describe('InlineNarrationEditor — edit activation', () => {
  it('click enters edit mode and calls on_activate', async () => {
    const user = userEvent.setup()
    const on_activate = vi.fn()
    const mutation = buildMutation()

    render(
      <InlineNarrationEditor
        is_active={false}
        mutation={mutation}
        on_activate={on_activate}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    await user.click(screen.getByRole('button'))
    expect(on_activate).toHaveBeenCalledOnce()
  })

  it('shows textarea when is_active=true', () => {
    const mutation = buildMutation()
    render(
      <InlineNarrationEditor
        is_active={true}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )
    expect(screen.getByRole('textbox')).toBeInTheDocument()
  })

  it('keyboard Enter on read-mode button enters edit mode', async () => {
    const user = userEvent.setup()
    const on_activate = vi.fn()
    const mutation = buildMutation()

    render(
      <InlineNarrationEditor
        is_active={false}
        mutation={mutation}
        on_activate={on_activate}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    const btn = screen.getByRole('button')
    btn.focus()
    await user.keyboard('{Enter}')
    expect(on_activate).toHaveBeenCalledOnce()
  })

  it('Tab key on read-mode paragraph enters edit mode', async () => {
    const user = userEvent.setup()
    const on_activate = vi.fn()
    const mutation = buildMutation()

    render(
      <InlineNarrationEditor
        is_active={false}
        mutation={mutation}
        on_activate={on_activate}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    const btn = screen.getByRole('button')
    btn.focus()
    await user.keyboard('{Tab}')
    expect(on_activate).toHaveBeenCalledOnce()
  })
})

// ── Save behaviour ─────────────────────────────────────────────────────────

describe('InlineNarrationEditor — save on Enter', () => {
  it('Enter key calls mutation.mutate with updated narration', async () => {
    const user = userEvent.setup()
    const mutate = vi.fn()
    const mutation = buildMutation({ mutate } as Partial<MutationResult>)

    render(
      <InlineNarrationEditor
        is_active={true}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    const textarea = screen.getByRole('textbox')
    await user.clear(textarea)
    await user.type(textarea, '새 나레이션')
    await user.keyboard('{Enter}')

    expect(mutate).toHaveBeenCalledWith(
      { narration: '새 나레이션', scene_index: 0 },
      expect.objectContaining({ onSuccess: expect.any(Function), onError: expect.any(Function) }),
    )
  })

  it('Shift+Enter does not save', async () => {
    const user = userEvent.setup()
    const mutate = vi.fn()
    const mutation = buildMutation({ mutate } as Partial<MutationResult>)

    render(
      <InlineNarrationEditor
        is_active={true}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    const textarea = screen.getByRole('textbox')
    textarea.focus()
    await user.keyboard('{Shift>}{Enter}{/Shift}')
    expect(mutate).not.toHaveBeenCalled()
  })

  it('concurrent blur + Enter does not trigger duplicate save', async () => {
    const user = userEvent.setup()
    const mutate = vi.fn()
    const mutation = buildMutation({ mutate } as Partial<MutationResult>)

    render(
      <InlineNarrationEditor
        is_active={true}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    const textarea = screen.getByRole('textbox')
    await user.clear(textarea)
    await user.type(textarea, '중복 방지 테스트')
    // Simulate Enter immediately followed by blur (potential double-save path).
    await user.keyboard('{Enter}')
    textarea.blur()

    // The is_saving_ref guard must prevent a second mutation.mutate call.
    expect(mutate).toHaveBeenCalledTimes(1)
  })
})

// ── Ctrl+Z revert ─────────────────────────────────────────────────────────

describe('InlineNarrationEditor — Ctrl+Z revert', () => {
  it('Ctrl+Z restores last persisted narration into textarea', async () => {
    const user = userEvent.setup()
    const mutation = buildMutation()

    render(
      <InlineNarrationEditor
        is_active={true}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    const textarea = screen.getByRole('textbox') as HTMLTextAreaElement
    await user.clear(textarea)
    await user.type(textarea, '임시 수정')
    expect(textarea.value).toContain('임시 수정')

    await user.keyboard('{Control>}z{/Control}')
    await waitFor(() => {
      expect(textarea.value).toBe(base_scene.narration)
    })
  })
})

// ── Error recovery ────────────────────────────────────────────────────────

describe('InlineNarrationEditor — error recovery', () => {
  it('shows error message and stays in error state after failed save', async () => {
    const user = userEvent.setup()

    let onErrorCb: ((err: ApiClientError) => void) | undefined
    const mutate = vi.fn((_vars: unknown, opts: { onError: (err: ApiClientError) => void }) => {
      onErrorCb = opts.onError
    })
    const mutation = buildMutation({ mutate } as Partial<MutationResult>)

    render(
      <InlineNarrationEditor
        is_active={true}
        mutation={mutation}
        on_activate={vi.fn()}
        on_deactivate={vi.fn()}
        scene={base_scene}
      />,
    )

    const textarea = screen.getByRole('textbox')
    await user.clear(textarea)
    await user.type(textarea, '실패할 내용')
    await user.keyboard('{Enter}')

    expect(mutate).toHaveBeenCalled()

    const err = Object.assign(new Error('저장 오류'), { status: 409, name: 'ApiClientError' })
    onErrorCb!(err as ApiClientError)

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    // After error, textarea reverts to last persisted value.
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe(base_scene.narration)
  })
})
