import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { RunCard } from './RunCard'

const base_run = {
  cost_usd: 1.8,
  created_at: '2026-04-19T00:00:00Z',
  critic_score: 84,
  duration_ms: 91000,
  human_override: false,
  id: 'scp-173-run-4',
  retry_count: 0,
  scp_id: '173',
  stage: 'critic' as const,
  status: 'waiting' as const,
  token_in: 1800,
  token_out: 420,
  updated_at: '2026-04-19T01:00:00Z',
}

describe('RunCard', () => {
  it('renders the compact one-line layout with dot tone matching the run status', () => {
    render(<RunCard run={base_run} selected={false} on_select={vi.fn()} />)

    expect(screen.getByText('SCP-173 Run #4')).toBeInTheDocument()
    const card = screen.getByRole('button', { name: /scp-173 run #4/i })
    expect(card).toHaveAttribute('data-tone', 'muted')
    expect(card).toHaveAttribute('data-selected', 'false')
  })

  it('marks selection state on the card and triggers on_select on click', async () => {
    const user = userEvent.setup()
    const on_select = vi.fn()

    render(<RunCard run={base_run} selected on_select={on_select} />)

    const card = screen.getByRole('button', { name: /scp-173 run #4/i })
    expect(card).toHaveAttribute('data-selected', 'true')

    await user.click(card)
    expect(on_select).toHaveBeenCalledWith('scp-173-run-4')
  })

  it('falls back to scp-only label when the run id has no parseable sequence', () => {
    render(
      <RunCard
        run={{ ...base_run, id: 'legacy-id' }}
        selected={false}
        on_select={vi.fn()}
      />,
    )

    expect(screen.getByText('SCP-173')).toBeInTheDocument()
  })
})
