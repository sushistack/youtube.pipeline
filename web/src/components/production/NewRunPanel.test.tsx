import React from 'react'
import { screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi, afterEach } from 'vitest'
import * as apiClient from '../../lib/apiClient'
import { NewRunPanel } from './NewRunPanel'
import { renderWithProviders } from '../../test/renderWithProviders'

function renderPanelHarness() {
  function Harness() {
    const [is_open, set_is_open] = React.useState(false)
    const [created_run_id, set_created_run_id] = React.useState<string | null>(null)
    const trigger_ref = React.useRef<HTMLButtonElement | null>(null)

    return (
      <div>
        <button
          ref={trigger_ref}
          type="button"
          onClick={() => {
            set_is_open(true)
          }}
        >
          Open panel
        </button>
        {created_run_id ? <p>Created {created_run_id}</p> : null}
        {is_open ? (
          <NewRunPanel
            on_cancel={() => {
              set_is_open(false)
              trigger_ref.current?.focus()
            }}
            on_success={async (run) => {
              set_created_run_id(run.id)
              set_is_open(false)
              trigger_ref.current?.focus()
            }}
          />
        ) : null}
      </div>
    )
  }

  return renderWithProviders(<Harness />)
}

const successful_run = {
  cost_usd: 0,
  created_at: '2026-04-19T00:00:00Z',
  duration_ms: 0,
  human_override: false,
  id: 'scp-049-run-1',
  retry_count: 0,
  scp_id: '049',
  stage: 'pending' as const,
  status: 'pending' as const,
  token_in: 0,
  token_out: 0,
  updated_at: '2026-04-19T00:00:00Z',
}

describe('NewRunPanel', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders as an alertdialog, autofocuses the input, traps Tab, and closes on Escape', async () => {
    const user = userEvent.setup()

    renderPanelHarness()

    await user.click(screen.getByRole('button', { name: 'Open panel' }))

    const dialog = screen.getByRole('alertdialog', {
      name: 'Create a new pipeline run',
    })
    const input = screen.getByRole('textbox', { name: 'SCP ID' })
    const create_button = screen.getByRole('button', { name: 'Create' })
    const cancel_button = screen.getByRole('button', { name: 'Cancel' })

    expect(dialog).toBeInTheDocument()
    await waitFor(() => {
      expect(input).toHaveFocus()
    })

    await user.type(input, '049')

    await user.tab()
    expect(create_button).toHaveFocus()
    await user.tab()
    expect(cancel_button).toHaveFocus()
    await user.tab()
    expect(input).toHaveFocus()

    // reverse cycle (Shift+Tab)
    await user.keyboard('{Shift>}{Tab}{/Shift}')
    expect(cancel_button).toHaveFocus()
    await user.keyboard('{Shift>}{Tab}{/Shift}')
    expect(create_button).toHaveFocus()
    await user.keyboard('{Shift>}{Tab}{/Shift}')
    expect(input).toHaveFocus()

    await user.keyboard('{Escape}')

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'Open panel' })).toHaveFocus()
  })

  it('validates SCP IDs client-side and trims before submit', async () => {
    const user = userEvent.setup()
    const create_run_spy = vi
      .spyOn(apiClient, 'createRun')
      .mockResolvedValue(successful_run)

    renderPanelHarness()

    await user.click(screen.getByRole('button', { name: 'Open panel' }))
    const input = screen.getByRole('textbox', { name: 'SCP ID' })
    const create_button = screen.getByRole('button', { name: 'Create' })

    expect(create_button).toBeDisabled()

    await user.type(input, 'abc def')
    expect(screen.getByRole('alert')).toHaveTextContent(
      'SCP ID must be alphanumeric, hyphen, or underscore',
    )
    expect(create_button).toBeDisabled()

    await user.clear(input)
    await user.type(input, '  049  ')

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(create_button).toBeEnabled()

    await user.keyboard('{Enter}')

    await waitFor(() => {
      expect(create_run_spy).toHaveBeenCalledWith('049')
    })
    expect(await screen.findByText('Created scp-049-run-1')).toBeInTheDocument()
  })

  it('maps validation, network, and generic server errors inline and allows retry', async () => {
    const user = userEvent.setup()
    const create_run_spy = vi.spyOn(apiClient, 'createRun')

    create_run_spy.mockRejectedValueOnce(
      new apiClient.ApiClientError('missing scp_id', 400, 'VALIDATION_ERROR'),
    )
    create_run_spy.mockRejectedValueOnce(new TypeError('fetch failed'))
    create_run_spy.mockRejectedValueOnce(
      new apiClient.ApiClientError('boom', 500, 'INTERNAL'),
    )
    create_run_spy.mockResolvedValueOnce(successful_run)

    renderPanelHarness()

    await user.click(screen.getByRole('button', { name: 'Open panel' }))
    const input = screen.getByRole('textbox', { name: 'SCP ID' })

    await user.type(input, '049')
    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The server rejected that SCP ID: missing scp_id. Check the format and try again.',
    )
    expect(screen.getByDisplayValue('049')).toBeInTheDocument()

    await user.type(input, 'x')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      "Couldn't reach the server. Check that `pipeline serve` is running, then retry.",
    )

    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Server error (500). The run was not created. Retry, or check the server logs.',
    )

    await user.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Created scp-049-run-1')).toBeInTheDocument()
  })
})
