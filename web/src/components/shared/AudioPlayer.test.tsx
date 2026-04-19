import '@testing-library/jest-dom'
import { fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { renderWithProviders } from '../../test/renderWithProviders'
import { AudioPlayer } from './AudioPlayer'

describe('AudioPlayer', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  })

  it('renders play/pause controls and seekbar for a valid source', async () => {
    const user = userEvent.setup()

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <AudioPlayer duration_ms={4200} scene_key="scene-0" src="/audio/scene-0.wav" />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument()
    expect(screen.getByLabelText(/audio seekbar/i)).toBeInTheDocument()
    expect(screen.getByText('0:00')).toBeInTheDocument()
    expect(screen.getByText('0:04')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Play' }))
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled()
  })

  it('toggles playback on space and prevents the default browser action', () => {
    renderWithProviders(
      <KeyboardShortcutsProvider>
        <AudioPlayer duration_ms={4200} scene_key="scene-0" src="/audio/scene-0.wav" />
      </KeyboardShortcutsProvider>,
    )

    const event = new KeyboardEvent('keydown', {
      key: ' ',
      bubbles: true,
      cancelable: true,
    })
    window.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled()
  })

  it('absorbs space and prevents default even when the scene has no audio', () => {
    renderWithProviders(
      <KeyboardShortcutsProvider>
        <AudioPlayer scene_key="scene-0" src={null} />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.getByText(/audio unavailable/i)).toBeInTheDocument()

    const event = new KeyboardEvent('keydown', {
      key: ' ',
      bubbles: true,
      cancelable: true,
    })
    window.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(HTMLMediaElement.prototype.play).not.toHaveBeenCalled()
  })

  it('resets playback to 0:00 and pauses when the scene changes', () => {
    const view = renderWithProviders(
      <KeyboardShortcutsProvider>
        <AudioPlayer key="scene-0" duration_ms={4200} scene_key="scene-0" src="/audio/scene-0.wav" />
      </KeyboardShortcutsProvider>,
    )

    const audio = view.container.querySelector('audio')
    if (!(audio instanceof HTMLAudioElement)) {
      throw new Error('expected audio element')
    }

    Object.defineProperty(audio, 'currentTime', {
      configurable: true,
      value: 2.7,
      writable: true,
    })
    fireEvent(audio, new Event('timeupdate'))

    expect(screen.getByText('0:02')).toBeInTheDocument()

    view.rerender(
      <KeyboardShortcutsProvider>
        <AudioPlayer key="scene-1" duration_ms={4200} scene_key="scene-1" src="/audio/scene-1.wav" />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument()
    expect(screen.getByText('0:00')).toBeInTheDocument()
  })
})
