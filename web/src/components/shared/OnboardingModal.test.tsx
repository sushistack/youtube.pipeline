import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { OnboardingModal } from './OnboardingModal'

describe('OnboardingModal', () => {
  it('renders the workflow mode labels and accessible dialog controls', () => {
    render(<OnboardingModal on_dismiss={vi.fn()} />)

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Know where to work next' })).toBeInTheDocument()
    expect(screen.getByText('Production')).toBeInTheDocument()
    expect(screen.getByText('Tuning')).toBeInTheDocument()
    expect(screen.getByText('Settings')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue to workspace' })).toBeInTheDocument()
  })

  it('dismisses on Escape', async () => {
    const user = userEvent.setup()
    const on_dismiss = vi.fn()

    render(<OnboardingModal on_dismiss={on_dismiss} />)

    await user.keyboard('{Escape}')

    expect(on_dismiss).toHaveBeenCalledTimes(1)
  })
})
