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
  it('renders the required card anatomy', () => {
    render(
      <RunCard run={base_run} selected={false} on_select={vi.fn()} />,
    )

    expect(screen.getByText('SCP-173')).toBeInTheDocument()
    expect(screen.getByText('Run 4')).toBeInTheDocument()
    expect(screen.getByText('Critic pass is waiting for review')).toBeInTheDocument()
    expect(screen.getByText('Waiting')).toBeInTheDocument()
    expect(screen.getByText('$1.80')).toBeInTheDocument()
    expect(screen.getByText('Critic')).toBeInTheDocument()
    expect(screen.getByText('84')).toBeInTheDocument()
  })

  it('applies the critic threshold tone and selection callback', async () => {
    const user = userEvent.setup()
    const on_select = vi.fn()

    render(<RunCard run={base_run} selected={true} on_select={on_select} />)

    expect(screen.getByText('84').closest('.run-card__critic')).toHaveAttribute(
      'data-tone',
      'high',
    )

    await user.click(screen.getByRole('button', { name: /scp-173/i }))

    expect(on_select).toHaveBeenCalledWith('scp-173-run-4')
  })
})
