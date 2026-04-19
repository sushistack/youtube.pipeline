import '@testing-library/jest-dom'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { RejectComposer } from './RejectComposer'

describe('RejectComposer', () => {
  it('renders as inline region (never as a modal dialog)', () => {
    render(
      <RejectComposer
        scene_index={0}
        is_submitting={false}
        on_submit={vi.fn()}
        on_cancel={vi.fn()}
      />,
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByLabelText(/reject composer for scene 1/i)).toBeInTheDocument()
  })

  it('auto-focuses the reason textarea on mount', () => {
    render(
      <RejectComposer
        scene_index={2}
        is_submitting={false}
        on_submit={vi.fn()}
        on_cancel={vi.fn()}
      />,
    )
    expect(screen.getByLabelText(/rejection reason/i)).toHaveFocus()
  })

  it('blocks submit when the reason is empty and shows inline validation', async () => {
    const user = userEvent.setup()
    const on_submit = vi.fn()
    render(
      <RejectComposer
        scene_index={0}
        is_submitting={false}
        on_submit={on_submit}
        on_cancel={vi.fn()}
      />,
    )
    await user.click(screen.getByRole('button', { name: /confirm reject/i }))
    expect(on_submit).not.toHaveBeenCalled()
    expect(screen.getByText(/rejection reason is required/i)).toBeInTheDocument()
  })

  it('submits the trimmed reason when confirm is pressed with non-empty text', async () => {
    const user = userEvent.setup()
    const on_submit = vi.fn()
    render(
      <RejectComposer
        scene_index={0}
        is_submitting={false}
        on_submit={on_submit}
        on_cancel={vi.fn()}
      />,
    )
    await user.type(screen.getByLabelText(/rejection reason/i), '  pacing off  ')
    await user.click(screen.getByRole('button', { name: /confirm reject/i }))
    expect(on_submit).toHaveBeenCalledWith('pacing off')
  })

  it('cancels via Esc only when the textarea is empty', async () => {
    const user = userEvent.setup()
    const on_cancel = vi.fn()
    render(
      <RejectComposer
        scene_index={0}
        is_submitting={false}
        on_submit={vi.fn()}
        on_cancel={on_cancel}
      />,
    )
    await user.keyboard('{Escape}')
    expect(on_cancel).toHaveBeenCalledTimes(1)

    on_cancel.mockReset()
    await user.type(screen.getByLabelText(/rejection reason/i), 'draft')
    await user.keyboard('{Escape}')
    expect(on_cancel).not.toHaveBeenCalled()
  })

  it('renders the FR53 prior rejection warning when supplied', () => {
    render(
      <RejectComposer
        scene_index={1}
        is_submitting={false}
        prior_rejection={{
          created_at: '2026-03-12T09:30:00Z',
          reason: 'scene felt rushed',
          run_id: 'prior-run-a',
          scene_index: 1,
          scp_id: '049',
        }}
        on_submit={vi.fn()}
        on_cancel={vi.fn()}
      />,
    )
    expect(screen.getByText(/we've seen this scene fail before/i)).toBeInTheDocument()
    expect(screen.getByText(/scene felt rushed/i)).toBeInTheDocument()
    expect(screen.getByText(/prior-run-a/)).toBeInTheDocument()
  })
})
