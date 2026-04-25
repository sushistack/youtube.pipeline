import '@testing-library/jest-dom'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { DetailPanel } from './DetailPanel'
import { renderWithProviders } from '../../test/renderWithProviders'
import type { ReviewItem } from '../../contracts/runContracts'

const baseItem: ReviewItem = {
  clip_path: null,
  content_flags: [],
  critic_breakdown: {
    aggregate_score: 82,
    emotional_variation: 64,
    fact_accuracy: 88,
    hook_strength: 91,
    immersion: 45,
  },
  critic_score: 82,
  high_leverage: true,
  high_leverage_reason: 'First appearance of SCP-049',
  high_leverage_reason_code: 'first_appearance',
  narration: 'Current version narration with stronger hook.',
  previous_version: {
    narration: 'Earlier narration.',
    shots: [
      {
        duration_s: 3,
        image_path: '/images/before.png',
        transition: 'fade',
        visual_descriptor: 'before',
      },
    ],
  },
  regen_attempts: 0,
  review_status: 'waiting_for_review',
  retry_exhausted: false,
  scene_index: 2,
  shots: [
    {
      duration_s: 4,
      image_path: '/images/after.png',
      transition: 'cut',
      visual_descriptor: 'after',
    },
  ],
  tts_duration_ms: 5400,
  tts_path: '/audio/scene-2.wav',
}

describe('DetailPanel', () => {
  it('renders the 6-metric grid with mapped critic fields and why-high-leverage annotation', () => {
    const { container } = renderWithProviders(
      <KeyboardShortcutsProvider>
        <DetailPanel item={baseItem} />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.getByText(/why high-leverage/i)).toHaveTextContent('First appearance of SCP-049')
    expect(screen.getByLabelText(/narration audio/i)).toBeInTheDocument()

    const grid = container.querySelector('.detail-panel__metrics-grid')!
    const cardFor = (key: string) =>
      grid.querySelector(`[data-metric="${key}"]`)! as HTMLElement

    expect(cardFor('visual')).toHaveTextContent('Visual')
    expect(cardFor('narration')).toHaveTextContent('Narration')
    expect(cardFor('coherence')).toHaveTextContent('Coherence')
    expect(cardFor('pacing')).toHaveTextContent('Pacing')
    expect(cardFor('scp_accuracy')).toHaveTextContent('SCP Accuracy')
    expect(cardFor('audio')).toHaveTextContent('Audio')

    expect(
      cardFor('narration').querySelector('.detail-panel__metric-score'),
    ).toHaveTextContent('91')
    expect(
      cardFor('scp_accuracy').querySelector('.detail-panel__metric-score'),
    ).toHaveTextContent('88')
    expect(
      cardFor('coherence').querySelector('.detail-panel__metric-score'),
    ).toHaveTextContent('45')

    expect(
      cardFor('visual').querySelector('.detail-panel__metric-score'),
    ).toHaveTextContent('—')
    expect(cardFor('visual')).toHaveAttribute('title', 'metric not yet emitted by critic')
    expect(
      cardFor('audio').querySelector('.detail-panel__metric-score'),
    ).toHaveTextContent('—')
    expect(cardFor('audio')).toHaveAttribute('title', 'metric not yet emitted by critic')
  })

  it('renders all 6 metrics as `—` placeholders when no critic breakdown is provided', () => {
    const { container } = renderWithProviders(
      <KeyboardShortcutsProvider>
        <DetailPanel item={{ ...baseItem, critic_breakdown: null }} />
      </KeyboardShortcutsProvider>,
    )

    const grid = container.querySelector('.detail-panel__metrics-grid')!
    const metric_keys = ['visual', 'narration', 'coherence', 'pacing', 'scp_accuracy', 'audio']
    for (const key of metric_keys) {
      const card = grid.querySelector(`[data-metric="${key}"]`)!
      expect(card.querySelector('.detail-panel__metric-score')).toHaveTextContent('—')
    }
  })

  it('shows version toggle and switches narration when previous version exists', async () => {
    const user = userEvent.setup()
    renderWithProviders(
      <KeyboardShortcutsProvider>
        <DetailPanel item={baseItem} />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.getByRole('button', { name: 'Current' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Previous' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Previous' }))

    expect(screen.getByText('Earlier narration.')).toBeInTheDocument()
    expect(screen.getByLabelText(/before and after diff/i)).toBeInTheDocument()
  })

  it('omits diff controls when there is no previous version', () => {
    renderWithProviders(
      <KeyboardShortcutsProvider>
        <DetailPanel
          item={{
            ...baseItem,
            previous_version: null,
          }}
        />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.queryByRole('button', { name: 'Previous' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/before and after diff/i)).not.toBeInTheDocument()
  })

  it('omits diff section for a standard (non-high-leverage) scene without prior version', () => {
    renderWithProviders(
      <KeyboardShortcutsProvider>
        <DetailPanel
          item={{
            ...baseItem,
            high_leverage: false,
            high_leverage_reason: null,
            high_leverage_reason_code: null,
            previous_version: null,
          }}
        />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.queryByLabelText(/before and after diff/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Previous' })).not.toBeInTheDocument()
    expect(screen.queryByText(/why high-leverage/i)).not.toBeInTheDocument()
  })

  it('resets version to current when the selected scene changes', async () => {
    const user = userEvent.setup()
    const { rerender } = renderWithProviders(
      <KeyboardShortcutsProvider>
        <DetailPanel key={baseItem.scene_index} item={baseItem} />
      </KeyboardShortcutsProvider>,
    )

    await user.click(screen.getByRole('button', { name: 'Previous' }))
    expect(screen.getByRole('button', { name: 'Previous' })).toHaveAttribute('data-active', 'true')

    rerender(
      <KeyboardShortcutsProvider>
        <DetailPanel key="scene-7" item={{ ...baseItem, scene_index: 7 }} />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.getByRole('button', { name: 'Current' })).toHaveAttribute('data-active', 'true')
    expect(screen.getByRole('button', { name: 'Previous' })).toHaveAttribute('data-active', 'false')
  })

  it('renders an unavailable audio state when the scene has no narration asset', () => {
    renderWithProviders(
      <KeyboardShortcutsProvider>
        <DetailPanel
          item={{
            ...baseItem,
            tts_duration_ms: null,
            tts_path: null,
          }}
        />
      </KeyboardShortcutsProvider>,
    )

    expect(screen.getByText(/audio unavailable for this scene/i)).toBeInTheDocument()
  })
})
