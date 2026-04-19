import { render, screen, within } from '@testing-library/react'
import '@testing-library/jest-dom'
import { describe, expect, it } from 'vitest'
import { ActionBar } from './ActionBar'
import { ShortcutHintAction } from './ShortcutHintAction'

describe('ActionBar', () => {
  it('renders a visible key label and action text', () => {
    render(<ShortcutHintAction shortcut="Enter" label="Approve" />)

    expect(screen.getByRole('button', { name: /\[Enter\] Approve/i })).toBeVisible()
    expect(screen.getByText('[Enter]')).toBeVisible()
    expect(screen.getByText('Approve')).toBeVisible()
  })

  it('renders action hints in a stable ordered grouping', () => {
    render(
      <ActionBar
        actions={[
          { label: 'Approve', shortcut: 'Enter' },
          { label: 'Reject', shortcut: 'Esc' },
          { label: 'Edit', shortcut: 'Tab' },
          { label: 'Skip', shortcut: 'S' },
          { label: 'Undo', shortcut: 'Ctrl+Z' },
        ]}
      />,
    )

    const toolbar = screen.getByRole('toolbar', { name: 'Keyboard actions' })
    const keys = within(toolbar)
      .getAllByTestId('shortcut-hint-key')
      .map((element) => element.textContent)

    expect(keys).toEqual(['[Enter]', '[Esc]', '[Tab]', '[S]', '[Ctrl+Z]'])
    expect(within(toolbar).getByRole('button', { name: /\[Esc\] Reject/i })).toBeVisible()
  })
})
