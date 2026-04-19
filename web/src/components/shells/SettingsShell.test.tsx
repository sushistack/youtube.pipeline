import '@testing-library/jest-dom'
import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { renderWithProviders } from '../../test/renderWithProviders'
import { SettingsShell } from './SettingsShell'

describe('SettingsShell', () => {
  it('renders the intro copy and timeline section together', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          version: 1,
          data: {
            items: [
              {
                id: 1,
                run_id: 'run-1',
                scp_id: '049',
                scene_id: '0',
                decision_type: 'approve',
                note: 'approved',
                reason_from_snapshot: null,
                superseded_by: null,
                created_at: '2026-04-19T01:00:00Z',
              },
            ],
            next_cursor: null,
          },
        }),
        {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        },
      ),
    )

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <SettingsShell />
      </KeyboardShortcutsProvider>,
    )

    expect(
      await screen.findByRole('heading', { name: 'Settings' }),
    ).toBeInTheDocument()
    expect(
      await screen.findByRole('heading', { name: 'Timeline' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/manage application preferences/i)).toBeInTheDocument()
  })
})
