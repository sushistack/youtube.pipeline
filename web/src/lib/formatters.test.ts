import { describe, expect, it, vi } from 'vitest'
import {
  ASSEMBLE_SUB_STAGES,
  ASSETS_SUB_STAGES,
  buildStageGraph,
  buildStageNodes,
  compareRunsForInventory,
  formatContinuityMessage,
  formatFreshness,
  getCriticTone,
  mapStageToNode,
  SCENARIO_SUB_STAGES,
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

describe('buildStageGraph', () => {
  it('exposes the engine-verified sub-stage order', () => {
    expect(SCENARIO_SUB_STAGES).toEqual([
      'research',
      'structure',
      'write',
      'visual_break',
      'review',
      'critic',
      'scenario_review',
    ])
    expect(ASSETS_SUB_STAGES).toEqual(['image', 'tts', 'batch_review'])
    expect(ASSEMBLE_SUB_STAGES).toEqual(['assemble', 'metadata_ack'])
  })

  it('marks scenario sub-states correctly when run is on critic', () => {
    const graph = buildStageGraph('critic', 'running')
    const scenario = graph.sub_nodes.scenario ?? []
    const states = Object.fromEntries(
      scenario.map((node) => [node.stage, node.state]),
    )
    expect(states).toEqual({
      research: 'completed',
      structure: 'completed',
      write: 'completed',
      visual_break: 'completed',
      review: 'completed',
      critic: 'active',
      scenario_review: 'upcoming',
    })
  })

  it('treats earlier parent groups as completed and later as upcoming', () => {
    const graph = buildStageGraph('image', 'running')
    expect(
      graph.sub_nodes.scenario?.every((node) => node.state === 'completed'),
    ).toBe(true)
    const assets = graph.sub_nodes.assets ?? []
    expect(assets.find((node) => node.stage === 'image')?.state).toBe('active')
    expect(assets.find((node) => node.stage === 'tts')?.state).toBe('upcoming')
    expect(
      graph.sub_nodes.assemble?.every((node) => node.state === 'upcoming'),
    ).toBe(true)
  })

  it('marks the active sub-node failed when status=failed', () => {
    const graph = buildStageGraph('write', 'failed')
    const scenario = graph.sub_nodes.scenario ?? []
    expect(scenario.find((node) => node.stage === 'write')?.state).toBe('failed')
    expect(scenario.find((node) => node.stage === 'structure')?.state).toBe(
      'completed',
    )
    expect(scenario.find((node) => node.stage === 'visual_break')?.state).toBe(
      'upcoming',
    )
  })

  it('attaches a counter to batch_review when decisions_summary is present', () => {
    const graph = buildStageGraph('batch_review', 'waiting', {
      approved_count: 8,
      rejected_count: 2,
      pending_count: 22,
    })
    const batch = graph.sub_nodes.assets?.find(
      (node) => node.stage === 'batch_review',
    )
    expect(batch?.state).toBe('active')
    expect(batch?.counter).toEqual({ done: 10, total: 32, suffix: 'reviewed' })
  })

  it('omits the counter when decisions_summary is absent', () => {
    const graph = buildStageGraph('batch_review', 'waiting')
    const batch = graph.sub_nodes.assets?.find(
      (node) => node.stage === 'batch_review',
    )
    expect(batch?.counter).toBeUndefined()
  })

  it('omits the counter when decisions_summary totals are all zero (no 0/0)', () => {
    const graph = buildStageGraph('batch_review', 'waiting', {
      approved_count: 0,
      rejected_count: 0,
      pending_count: 0,
    })
    const batch = graph.sub_nodes.assets?.find(
      (node) => node.stage === 'batch_review',
    )
    expect(batch?.counter).toBeUndefined()
  })

  it('marks all sub-nodes completed when run status is completed', () => {
    const graph = buildStageGraph('complete', 'completed')
    expect(
      graph.sub_nodes.scenario?.every((node) => node.state === 'completed'),
    ).toBe(true)
    expect(
      graph.sub_nodes.assets?.every((node) => node.state === 'completed'),
    ).toBe(true)
    expect(
      graph.sub_nodes.assemble?.every((node) => node.state === 'completed'),
    ).toBe(true)
  })
})
