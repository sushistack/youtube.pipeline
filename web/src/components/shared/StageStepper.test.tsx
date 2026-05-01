import { fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import { describe, expect, it, vi } from 'vitest'
import { StageStepper } from './StageStepper'

describe('StageStepper', () => {
  it('renders the four-node work-phase contract with labels', () => {
    render(<StageStepper stage="image" status="running" variant="full" />)

    expect(screen.getAllByRole('listitem')).toHaveLength(4)
    expect(screen.queryByText('Pending')).not.toBeInTheDocument()
    expect(screen.queryByText('Complete')).not.toBeInTheDocument()
    expect(screen.getByText('Story')).toBeInTheDocument()
    expect(screen.getByText('Cast')).toBeInTheDocument()
    expect(screen.getByText('Media')).toBeInTheDocument()
    expect(screen.getByText('Cut')).toBeInTheDocument()
    expect(screen.getByLabelText('Media: active')).toBeInTheDocument()
  })

  it('keeps compact mode accessible while hiding visible labels', () => {
    render(<StageStepper stage="character_pick" status="waiting" variant="compact" />)

    expect(screen.getAllByRole('listitem')).toHaveLength(4)
    expect(screen.getByLabelText('Cast: active')).toBeInTheDocument()
  })

  describe('expanded variant (graph canvas)', () => {
    it('mounts the graph view with the pipeline DAG aria-region', () => {
      render(<StageStepper stage="critic" status="running" variant="expanded" />)

      expect(screen.getByRole('img', { name: /pipeline dag/i })).toBeInTheDocument()
    })

    it('hides the pending and complete lifecycle lanes from the DAG', () => {
      render(<StageStepper stage="image" status="running" variant="expanded" />)

      // Pending/Complete are run lifecycle states, not work phases.
      expect(screen.queryByText('Pending')).not.toBeInTheDocument()
      expect(screen.queryByText('Queued')).not.toBeInTheDocument()
      expect(screen.queryByLabelText(/^Complete: /)).not.toBeInTheDocument()
      // Story/Cast/Media/Cut lane labels remain.
      expect(screen.getByText('Story')).toBeInTheDocument()
      expect(screen.getByText('Cast')).toBeInTheDocument()
      expect(screen.getByText('Media')).toBeInTheDocument()
      expect(screen.getByText('Cut')).toBeInTheDocument()
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

  describe('rewind affordance', () => {
    it('renders completed nodes as buttons when on_rewind_request is provided', () => {
      const handler = vi.fn()
      render(
        <StageStepper
          stage="assemble"
          status="running"
          variant="full"
          on_rewind_request={handler}
        />,
      )
      // Story / Cast / Media are completed → buttons.
      expect(screen.getByRole('button', { name: 'Rewind to Story' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Rewind to Cast' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Rewind to Media' })).toBeInTheDocument()
      // Cut is active → not a button.
      expect(screen.queryByRole('button', { name: 'Rewind to Cut' })).not.toBeInTheDocument()
    })

    it('does not render buttons when handler is omitted', () => {
      render(<StageStepper stage="assemble" status="running" variant="full" />)
      expect(screen.queryByRole('button', { name: /Rewind/i })).not.toBeInTheDocument()
    })

    it('fires the handler with the stepper key on click', () => {
      const handler = vi.fn()
      render(
        <StageStepper
          stage="assemble"
          status="running"
          variant="full"
          on_rewind_request={handler}
        />,
      )
      fireEvent.click(screen.getByRole('button', { name: 'Rewind to Cast' }))
      expect(handler).toHaveBeenCalledWith('character')
    })

    it('disables the targeted button when rewind_pending_node matches', () => {
      const handler = vi.fn()
      render(
        <StageStepper
          stage="assemble"
          status="running"
          variant="full"
          on_rewind_request={handler}
          rewind_pending_node="assets"
        />,
      )
      const media_btn = screen.getByRole('button', { name: 'Rewind to Media' })
      expect(media_btn).toBeDisabled()
      // Other nodes still active.
      expect(screen.getByRole('button', { name: 'Rewind to Cast' })).not.toBeDisabled()
    })

    it('keeps compact variant non-clickable', () => {
      const handler = vi.fn()
      render(
        <StageStepper
          stage="assemble"
          status="running"
          variant="compact"
          on_rewind_request={handler}
        />,
      )
      expect(screen.queryByRole('button', { name: /Rewind/i })).not.toBeInTheDocument()
    })
  })
})
