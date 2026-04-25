import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import { describe, expect, it } from 'vitest'
import { StageStepper } from './StageStepper'

describe('StageStepper', () => {
  it('renders the full six-node contract with labels', () => {
    render(<StageStepper stage="image" status="running" variant="full" />)

    expect(screen.getAllByRole('listitem')).toHaveLength(6)
    expect(screen.getByText('Pending')).toBeInTheDocument()
    expect(screen.getByText('Scenario')).toBeInTheDocument()
    expect(screen.getByText('Character')).toBeInTheDocument()
    expect(screen.getByText('Assets')).toBeInTheDocument()
    expect(screen.getByText('Assemble')).toBeInTheDocument()
    expect(screen.getByText('Complete')).toBeInTheDocument()
    expect(screen.getByLabelText('Assets: active')).toBeInTheDocument()
  })

  it('keeps compact mode accessible while hiding visible labels', () => {
    render(<StageStepper stage="character_pick" status="waiting" variant="compact" />)

    expect(screen.getAllByRole('listitem')).toHaveLength(6)
    expect(screen.getByLabelText('Character: active')).toBeInTheDocument()
  })

  describe('expanded variant', () => {
    it('reveals scenario sub-stages in engine-verified order with correct states', () => {
      render(<StageStepper stage="critic" status="running" variant="expanded" />)

      const scenario_rail = screen.getByRole('list', { name: /scenario sub-stages/i })
      expect(scenario_rail).toBeInTheDocument()
      const sub_items = scenario_rail.querySelectorAll('[data-stage]')
      expect(Array.from(sub_items).map((node) => node.getAttribute('data-stage'))).toEqual([
        'research',
        'structure',
        'write',
        'visual_break',
        'review',
        'critic',
        'scenario_review',
      ])
      expect(screen.getByLabelText('Critic pass: active')).toBeInTheDocument()
      expect(screen.getByLabelText('Research: completed')).toBeInTheDocument()
      expect(screen.getByLabelText('Scenario review: upcoming')).toBeInTheDocument()
    })

    it('renders sequential image → tts → batch_review under assets, no parallel rails', () => {
      render(<StageStepper stage="image" status="running" variant="expanded" />)

      const assets_rail = screen.getByRole('list', { name: /assets sub-stages/i })
      const sub_items = assets_rail.querySelectorAll('[data-stage]')
      expect(Array.from(sub_items).map((node) => node.getAttribute('data-stage'))).toEqual([
        'image',
        'tts',
        'batch_review',
      ])
      expect(screen.getByLabelText('Image generation: active')).toBeInTheDocument()
      expect(screen.getByLabelText('Voice render: upcoming')).toBeInTheDocument()
    })

    it('renders the decisions counter on batch_review when summary is provided', () => {
      render(
        <StageStepper
          stage="batch_review"
          status="waiting"
          variant="expanded"
          decisions_summary={{
            approved_count: 8,
            rejected_count: 2,
            pending_count: 22,
          }}
        />,
      )

      expect(screen.getByText('10/32 reviewed')).toBeInTheDocument()
    })

    it('omits the counter when decisions_summary is missing', () => {
      render(<StageStepper stage="batch_review" status="waiting" variant="expanded" />)

      expect(screen.queryByText(/reviewed/)).not.toBeInTheDocument()
    })

    it('marks the active sub-node failed when run status is failed', () => {
      render(<StageStepper stage="write" status="failed" variant="expanded" />)

      expect(screen.getByLabelText('Script writing: failed')).toBeInTheDocument()
      expect(screen.getByLabelText('Structure: completed')).toBeInTheDocument()
      expect(screen.getByLabelText('Shot planning: upcoming')).toBeInTheDocument()
    })
  })
})
