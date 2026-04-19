import { fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ContinuityBanner } from './ContinuityBanner'

describe('ContinuityBanner', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('auto-dismisses after five seconds', () => {
    vi.useFakeTimers()
    const on_dismiss = vi.fn()

    render(
      <ContinuityBanner
        message="Scene 4 moved from pending to approved"
        on_dismiss={on_dismiss}
      />,
    )

    vi.advanceTimersByTime(4999)
    expect(on_dismiss).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    expect(on_dismiss).toHaveBeenCalledTimes(1)
  })

  it('dismisses immediately on interaction and cancels the later timer callback', async () => {
    vi.useFakeTimers()
    const on_dismiss = vi.fn()

    render(
      <div>
        <button type="button">Outside action</button>
        <ContinuityBanner
          message="Scene 4 moved from pending to approved"
          on_dismiss={on_dismiss}
        />
      </div>,
    )

    fireEvent.keyDown(window, { key: 'Enter' })
    expect(on_dismiss).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(5000)
    expect(on_dismiss).toHaveBeenCalledTimes(1)
  })

  it('ignores keydown dispatched from an editable input target', () => {
    vi.useFakeTimers()
    const on_dismiss = vi.fn()

    render(
      <div>
        <input data-testid="search" />
        <ContinuityBanner
          message="Scene 4 moved from pending to approved"
          on_dismiss={on_dismiss}
        />
      </div>,
    )

    const input = screen.getByTestId('search') as HTMLInputElement
    input.focus()
    fireEvent.keyDown(input, { key: 'a' })

    expect(on_dismiss).not.toHaveBeenCalled()
  })

  it('ignores IME composition keydown events', () => {
    vi.useFakeTimers()
    const on_dismiss = vi.fn()

    render(
      <ContinuityBanner
        message="Scene 4 moved from pending to approved"
        on_dismiss={on_dismiss}
      />,
    )

    fireEvent.keyDown(window, { key: 'Process', isComposing: true })

    expect(on_dismiss).not.toHaveBeenCalled()
  })

  it('renders the expected title and message', () => {
    render(
      <ContinuityBanner
        message="Scene 4 moved from pending to approved"
        on_dismiss={vi.fn()}
      />,
    )

    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.getByText('What changed since last session')).toBeInTheDocument()
    expect(screen.getByText('Scene 4 moved from pending to approved')).toBeInTheDocument()
  })
})
