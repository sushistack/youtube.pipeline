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

  describe('expanded variant (graph canvas)', () => {
    it('mounts the graph view with the pipeline DAG aria-region', () => {
      render(<StageStepper stage="critic" status="running" variant="expanded" />)

      expect(screen.getByRole('img', { name: /pipeline dag/i })).toBeInTheDocument()
    })

    it('exposes scenario sub-stage states via per-node aria-labels', () => {
      render(<StageStepper stage="critic" status="running" variant="expanded" />)

      expect(screen.getByLabelText('Critic pass: active')).toBeInTheDocument()
      expect(screen.getByLabelText('Research: completed')).toBeInTheDocument()
      expect(screen.getByLabelText('Script writing: completed')).toBeInTheDocument()
      expect(screen.getByLabelText('Scenario review: upcoming')).toBeInTheDocument()
    })

    it('renders image and tts as parallel active branches when stage=image', () => {
      render(<StageStepper stage="image" status="running" variant="expanded" />)

      expect(screen.getByLabelText('Image generation: active')).toBeInTheDocument()
      expect(screen.getByLabelText('Voice render: active')).toBeInTheDocument()
      expect(screen.getByLabelText('Asset review: upcoming')).toBeInTheDocument()
    })

    it('keeps both branches active when stage=tts (cannot distinguish from polled signal)', () => {
      render(<StageStepper stage="tts" status="running" variant="expanded" />)

      expect(screen.getByLabelText('Image generation: active')).toBeInTheDocument()
      expect(screen.getByLabelText('Voice render: active')).toBeInTheDocument()
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

    it('marks the active node failed when run status is failed', () => {
      render(<StageStepper stage="write" status="failed" variant="expanded" />)

      expect(screen.getByLabelText('Script writing: failed')).toBeInTheDocument()
      expect(screen.getByLabelText('Structure: completed')).toBeInTheDocument()
      expect(screen.getByLabelText('Shot planning: upcoming')).toBeInTheDocument()
    })
  })
})
