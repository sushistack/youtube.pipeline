import '@testing-library/jest-dom'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ProductionAppHeader } from './ProductionAppHeader'

const base_run = {
  cost_usd: 1.8,
  created_at: '2026-04-19T00:00:00Z',
  duration_ms: 91000,
  human_override: false,
  id: 'scp-049-run-12',
  retry_count: 0,
  scp_id: '049',
  stage: 'batch_review' as const,
  status: 'waiting' as const,
  token_in: 1800,
  token_out: 420,
  updated_at: '2026-04-19T01:00:00Z',
}

describe('ProductionAppHeader', () => {
  it('renders run identity in `SCP-XXX Run #N` format and the 6-stage stepper', () => {
    render(<ProductionAppHeader run={base_run} />)

    expect(screen.getByText('SCP-049 Run #12')).toBeInTheDocument()
    expect(
      screen.getByRole('list', { name: /pipeline progress/i }),
    ).toBeInTheDocument()
  })

  it('renders an empty-state heading when no run is selected', () => {
    render(<ProductionAppHeader run={null} />)

    expect(screen.getByText('No run selected')).toBeInTheDocument()
    expect(
      screen.queryByRole('list', { name: /pipeline progress/i }),
    ).not.toBeInTheDocument()
  })

  it('falls back to the raw run id when the sequence cannot be parsed', () => {
    render(
      <ProductionAppHeader
        run={{ ...base_run, id: 'legacy-id-without-suffix' }}
      />,
    )

    expect(screen.getByText('legacy-id-without-suffix')).toBeInTheDocument()
  })
})
