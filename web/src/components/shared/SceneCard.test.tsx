import '@testing-library/jest-dom'
import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SceneCard } from './SceneCard'
import { renderWithProviders } from '../../test/renderWithProviders'
import type { ReviewItem } from '../../contracts/runContracts'

const item: ReviewItem = {
  clip_path: null,
  content_flags: [],
  critic_breakdown: null,
  critic_score: 86,
  high_leverage: true,
  high_leverage_reason: 'Opening hook scene',
  high_leverage_reason_code: 'hook_scene',
  narration: 'A tense opening corridor shot.',
  previous_version: null,
  regen_attempts: 0,
  review_status: 'waiting_for_review',
  retry_exhausted: false,
  scene_index: 0,
  shots: [
    {
      duration_s: 3.2,
      image_path: '/images/scene-0.png',
      transition: 'cut',
      visual_descriptor: 'opening corridor',
    },
  ],
  tts_duration_ms: null,
  tts_path: null,
}

describe('SceneCard', () => {
  it('renders selection state, high-leverage badge, and score badge', () => {
    renderWithProviders(
      <SceneCard item={item} on_select={vi.fn()} selected />,
    )

    expect(screen.getByRole('option')).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('High-Leverage')).toBeInTheDocument()
    expect(screen.getByText('86')).toBeInTheDocument()
    expect(screen.getByText(/tense opening corridor/i)).toBeInTheDocument()
  })
})
