import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { StatusBar } from './StatusBar'

const running_run = {
  cost_usd: 2.5,
  created_at: '2026-04-19T00:00:00Z',
  duration_ms: 125000,
  human_override: false,
  id: 'scp-049-run-2',
  retry_count: 0,
  scp_id: '049',
  stage: 'write' as const,
  status: 'running' as const,
  token_in: 1200,
  token_out: 340,
  updated_at: '2026-04-19T01:00:00Z',
}

describe('StatusBar', () => {
  it('renders compact live telemetry for an active run', () => {
    render(<StatusBar run={running_run} />)

    const bar = screen.getByTestId('status-bar')
    expect(bar).toHaveAttribute('data-visible', 'true')
    expect(screen.getByText('Script writing')).toBeInTheDocument()
    expect(screen.getByText('2:05')).toBeInTheDocument()
    expect(screen.getByText('$2.50')).toBeInTheDocument()
  })

  it('reveals run id and cost detail on hover and focus', async () => {
    const user = userEvent.setup()
    render(<StatusBar run={running_run} />)

    const bar = screen.getByTestId('status-bar')
    expect(screen.getByText('scp-049-run-2').parentElement).toHaveAttribute(
      'aria-hidden',
      'true',
    )

    await user.hover(bar)

    expect(bar).toHaveAttribute('data-expanded', 'true')
    expect(screen.getByText('In 1,200 / Out 340')).toBeInTheDocument()

    await user.unhover(bar)
    await user.tab()

    expect(bar).toHaveAttribute('data-expanded', 'true')
  })

  it('collapses to zero-height when there is no live run', () => {
    render(
      <StatusBar
        run={{
          ...running_run,
          status: 'completed',
        }}
      />,
    )

    expect(screen.getByTestId('status-bar')).toHaveAttribute('data-visible', 'false')
  })
})
