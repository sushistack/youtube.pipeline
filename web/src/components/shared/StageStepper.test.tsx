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
})
