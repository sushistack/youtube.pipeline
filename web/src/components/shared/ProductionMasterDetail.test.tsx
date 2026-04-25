import '@testing-library/jest-dom'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ProductionMasterDetail } from './ProductionMasterDetail'

describe('ProductionMasterDetail', () => {
  it('renders both master and detail slots when master is provided', () => {
    render(
      <ProductionMasterDetail
        master={<div data-testid="master-content">scenes</div>}
        detail={<div data-testid="detail-content">detail</div>}
      />,
    )

    expect(screen.getByTestId('master-content')).toBeInTheDocument()
    expect(screen.getByTestId('detail-content')).toBeInTheDocument()
    expect(screen.getByLabelText('Scenes')).toBeInTheDocument()
    expect(screen.getByLabelText('Detail')).toBeInTheDocument()
  })

  it('renders the empty master placeholder when no master content is supplied', () => {
    render(
      <ProductionMasterDetail
        detail={<div data-testid="detail-content">detail</div>}
      />,
    )

    expect(
      screen.getByText(/scenes will appear once phase a finishes/i),
    ).toBeInTheDocument()
    expect(screen.getByTestId('detail-content')).toBeInTheDocument()
  })

  it('honors a custom empty-master message and master label', () => {
    render(
      <ProductionMasterDetail
        master_label="Queue"
        master_empty_message="Nothing queued yet."
        detail={<div>detail</div>}
      />,
    )

    expect(screen.getByLabelText('Queue')).toBeInTheDocument()
    expect(screen.getByText('Nothing queued yet.')).toBeInTheDocument()
  })
})
