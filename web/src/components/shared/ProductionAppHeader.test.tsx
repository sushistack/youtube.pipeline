import '@testing-library/jest-dom'
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import userEvent from '@testing-library/user-event'
import { ProductionAppHeader } from './ProductionAppHeader'
import { useUIStore } from '../../stores/useUIStore'

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
  beforeEach(() => {
    useUIStore.getState().set_stage_stepper_expanded(false)
  })

  afterEach(() => {
    useUIStore.getState().set_stage_stepper_expanded(false)
  })

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

  it('renders an expand-pipeline toggle when a run is selected', () => {
    render(<ProductionAppHeader run={base_run} />)

    const toggle = screen.getByRole('button', { name: /expand pipeline view/i })
    expect(toggle).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-pressed', 'false')
  })

  it('does not render the toggle when no run is selected', () => {
    render(<ProductionAppHeader run={null} />)

    expect(
      screen.queryByRole('button', { name: /pipeline view/i }),
    ).not.toBeInTheDocument()
  })

  it('toggles stage_stepper_expanded in the UI store and switches stepper variant', async () => {
    const user = userEvent.setup()
    render(<ProductionAppHeader run={base_run} />)

    expect(useUIStore.getState().stage_stepper_expanded).toBe(false)
    expect(
      screen.queryByRole('list', { name: /scenario sub-stages/i }),
    ).not.toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: /expand pipeline view/i }),
    )

    expect(useUIStore.getState().stage_stepper_expanded).toBe(true)
    expect(
      screen.getByRole('list', { name: /scenario sub-stages/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /collapse pipeline view/i }),
    ).toHaveAttribute('aria-pressed', 'true')
  })

  it('flows decisions_summary into the expanded stepper for the batch_review counter', () => {
    useUIStore.getState().set_stage_stepper_expanded(true)
    render(
      <ProductionAppHeader
        run={base_run}
        status_payload={{
          run: base_run,
          decisions_summary: {
            approved_count: 8,
            rejected_count: 2,
            pending_count: 22,
          },
        }}
      />,
    )

    expect(screen.getByText('10/32 reviewed')).toBeInTheDocument()
  })
})
