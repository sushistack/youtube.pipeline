import { describe, expect, it, vi } from 'vitest'
import {
  buildStageNodes,
  compareRunsForInventory,
  formatContinuityMessage,
  formatFreshness,
  getCriticTone,
  mapStageToNode,
} from './formatters'

describe('formatters', () => {
  it('maps representative backend stages into the six UX nodes', () => {
    expect(mapStageToNode('pending')).toBe('pending')
    expect(mapStageToNode('visual_break')).toBe('scenario')
    expect(mapStageToNode('character_pick')).toBe('character')
    expect(mapStageToNode('tts')).toBe('assets')
    expect(mapStageToNode('metadata_ack')).toBe('assemble')
    expect(mapStageToNode('complete')).toBe('complete')
  })

  it('marks the active node as failed when the run fails', () => {
    const nodes = buildStageNodes('batch_review', 'failed')

    expect(nodes.map((node) => node.state)).toEqual([
      'completed',
      'completed',
      'completed',
      'failed',
      'upcoming',
      'upcoming',
    ])
  })

  it('sorts live runs ahead of completed inventory items', () => {
    const running = {
      cost_usd: 2.5,
      created_at: '2026-04-19T00:00:00Z',
      duration_ms: 120000,
      human_override: false,
      id: 'scp-049-run-2',
      retry_count: 0,
      scp_id: '049',
      stage: 'write' as const,
      status: 'running' as const,
      token_in: 0,
      token_out: 0,
      updated_at: '2026-04-19T01:00:00Z',
    }
    const completed = {
      ...running,
      id: 'scp-049-run-1',
      status: 'completed' as const,
      updated_at: '2026-04-19T02:00:00Z',
    }

    expect([completed, running].sort(compareRunsForInventory)[0].id).toBe(
      'scp-049-run-2',
    )
  })

  it('applies the critic-score tone thresholds exactly', () => {
    expect(getCriticTone(80)).toBe('high')
    expect(getCriticTone(79)).toBe('medium')
    expect(getCriticTone(50)).toBe('medium')
    expect(getCriticTone(49)).toBe('low')
  })

  it('renders a stable freshness cue from updated_at', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-04-19T03:00:00Z'))

    expect(formatFreshness('2026-04-19T02:45:00Z')).toBe('Updated 15m ago')

    vi.useRealTimers()
  })

  it('formats continuity changes from backend diff data with a remainder count', () => {
    expect(
      formatContinuityMessage({
        changes_since_last_interaction: [
          {
            after: 'approved',
            before: 'pending',
            kind: 'scene_status_flipped',
            scene_id: '4',
          },
          {
            kind: 'scene_added',
            scene_id: '6',
          },
          {
            kind: 'scene_removed',
            scene_id: '8',
          },
        ],
      } as const),
    ).toBe('Scene 4 moved from pending to approved (+2 more updates)')
  })

  it('falls back to the backend summary when no diff array is present', () => {
    expect(
      formatContinuityMessage({
        summary: 'Scenario review is waiting for your input',
      }),
    ).toBe('Scenario review is waiting for your input')
  })

  it('returns null when the backend summary is empty or whitespace', () => {
    expect(formatContinuityMessage({ summary: '' })).toBeNull()
    expect(formatContinuityMessage({ summary: '   ' })).toBeNull()
    expect(formatContinuityMessage({})).toBeNull()
  })

  it('avoids "moved from X to X" when scene_status_flipped has equal before/after', () => {
    expect(
      formatContinuityMessage({
        changes_since_last_interaction: [
          {
            after: 'pending',
            before: 'pending',
            kind: 'scene_status_flipped',
            scene_id: '4',
          },
        ],
      } as const),
    ).toBe('Scene 4 is now pending')
  })
})
