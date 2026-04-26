import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { VisionDescriptorEditor } from './VisionDescriptorEditor'

describe('VisionDescriptorEditor', () => {
  it('renders the prefill value in read mode', () => {
    render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="initial descriptor"
      />,
    )
    expect(screen.getByText('initial descriptor')).toBeInTheDocument()
  })

  it('activates the textarea when Tab is pressed in read mode', async () => {
    const user = userEvent.setup()
    render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="initial"
      />,
    )
    const read_mode = screen.getByRole('button', { name: /vision descriptor draft/i })
    read_mode.focus()
    await user.keyboard('{Tab}')
    expect(screen.getByLabelText(/vision descriptor draft/i)).toHaveFocus()
  })

  it('calls onConfirm when Enter is pressed in read mode', async () => {
    const user = userEvent.setup()
    const on_confirm = vi.fn()
    render(
      <VisionDescriptorEditor
        onConfirm={on_confirm}
        onDescriptorChange={vi.fn()}
        prefill="initial"
      />,
    )
    const read_mode = screen.getByRole('button', { name: /vision descriptor draft/i })
    read_mode.focus()
    await user.keyboard('{Enter}')
    expect(on_confirm).toHaveBeenCalledTimes(1)
  })

  it('reverts to prefill when Ctrl+Z is pressed in the textarea', async () => {
    const user = userEvent.setup()
    render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="revert-target"
      />,
    )
    await user.tab()
    await user.keyboard('{Tab}')
    const textarea = screen.getByLabelText(/vision descriptor draft/i) as HTMLTextAreaElement
    await user.clear(textarea)
    await user.type(textarea, 'edited value')
    expect(textarea.value).toBe('edited value')
    await user.keyboard('{Control>}z{/Control}')
    expect(textarea.value).toBe('revert-target')
  })

  it('calls onDescriptorChange with trimmed draft on blur', async () => {
    const user = userEvent.setup()
    const on_change = vi.fn()
    render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={on_change}
        prefill="initial"
      />,
    )
    const read_mode = screen.getByRole('button', { name: /vision descriptor draft/i })
    read_mode.focus()
    await user.keyboard('{Tab}')
    const textarea = screen.getByLabelText(/vision descriptor draft/i) as HTMLTextAreaElement
    await user.clear(textarea)
    await user.type(textarea, '  new draft  ')
    textarea.blur()
    expect(on_change).toHaveBeenCalledWith('new draft')
  })

  it('syncs reverted value to onDescriptorChange on Ctrl+Z (does not wait for blur)', async () => {
    const user = userEvent.setup()
    const on_change = vi.fn()
    render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={on_change}
        prefill="revert-target"
      />,
    )
    const read_mode = screen.getByRole('button', { name: /vision descriptor draft/i })
    read_mode.focus()
    await user.keyboard('{Tab}')
    const textarea = screen.getByLabelText(/vision descriptor draft/i) as HTMLTextAreaElement
    await user.clear(textarea)
    await user.type(textarea, 'edited value')
    on_change.mockClear()
    await user.keyboard('{Control>}z{/Control}')
    // Ctrl+Z must notify the parent immediately with the reverted value so
    // a subsequent confirm cannot accidentally submit the pre-revert edit.
    expect(on_change).toHaveBeenCalledWith('revert-target')
  })

  it('does not reset draft when prefill prop changes mid-edit', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="initial"
      />,
    )
    const read_mode = screen.getByRole('button', { name: /vision descriptor draft/i })
    read_mode.focus()
    await user.keyboard('{Tab}')
    const textarea = screen.getByLabelText(/vision descriptor draft/i) as HTMLTextAreaElement
    await user.clear(textarea)
    await user.type(textarea, 'operator edits')
    // Simulate a background refetch delivering a new prefill prop while the
    // operator is editing. The in-progress draft must survive.
    rerender(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="background-refetch-value"
      />,
    )
    expect(textarea.value).toBe('operator edits')
  })

  it('Edit descriptor toggles textarea visibility and switches button label', async () => {
    const user = userEvent.setup()
    render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="readable"
      />,
    )
    expect(screen.queryByLabelText(/vision descriptor draft$/i)).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /edit descriptor/i }))
    expect(screen.getByLabelText(/vision descriptor draft$/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /save edit/i })).toBeInTheDocument()
  })

  it('Save edit syncs draft to parent and returns to read mode', async () => {
    const user = userEvent.setup()
    const on_change = vi.fn()
    render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={on_change}
        prefill="initial"
      />,
    )
    await user.click(screen.getByRole('button', { name: /edit descriptor/i }))
    const textarea = screen.getByLabelText(/vision descriptor draft$/i) as HTMLTextAreaElement
    await user.clear(textarea)
    await user.type(textarea, '  edited via button  ')
    on_change.mockClear()
    await user.click(screen.getByRole('button', { name: /save edit/i }))
    expect(on_change).toHaveBeenCalledWith('edited via button')
    // Read-mode is restored — the role=button div with aria-label "Vision Descriptor draft. Press Tab..." is back.
    expect(
      screen.getByRole('button', { name: /vision descriptor draft\. press tab/i }),
    ).toBeInTheDocument()
  })

  it('Confirm & continue calls onConfirm', async () => {
    const user = userEvent.setup()
    const on_confirm = vi.fn()
    render(
      <VisionDescriptorEditor
        onConfirm={on_confirm}
        onDescriptorChange={vi.fn()}
        prefill="initial"
      />,
    )
    await user.click(screen.getByRole('button', { name: /confirm & continue/i }))
    expect(on_confirm).toHaveBeenCalledTimes(1)
  })

  it('Confirm & continue is disabled and shows Saving label while submitting', () => {
    render(
      <VisionDescriptorEditor
        is_submitting
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="initial"
      />,
    )
    const primary = screen.getByRole('button', { name: /saving/i })
    expect(primary).toBeDisabled()
  })

  it('resets draft and revert target when prefill prop changes', async () => {
    const { rerender } = render(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="first"
      />,
    )
    expect(screen.getByText('first')).toBeInTheDocument()
    rerender(
      <VisionDescriptorEditor
        onConfirm={vi.fn()}
        onDescriptorChange={vi.fn()}
        prefill="second"
      />,
    )
    expect(screen.getByText('second')).toBeInTheDocument()
  })
})
