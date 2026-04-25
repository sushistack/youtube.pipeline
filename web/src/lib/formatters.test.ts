import { describe, expect, it, vi } from 'vitest'
import {
  buildStageDagTopology,
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

describe('buildStageDagTopology', () => {
  it('emits 15 nodes spanning the full pipeline', () => {
    const dag = buildStageDagTopology('pending', 'pending')
    expect(dag.nodes.map((n) => n.id)).toEqual([
      'pending',
      'research',
      'structure',
      'write',
      'visual_break',
      'review',
      'critic',
      'scenario_review',
      'character_pick',
      'image',
      'tts',
      'batch_review',
      'assemble',
      'metadata_ack',
      'complete',
    ])
  })

  it('emits 6 swim-lanes matching the thin stepper grouping', () => {
    const dag = buildStageDagTopology('write', 'running')
    expect(dag.lanes.map((l) => l.id)).toEqual([
      'pending',
      'scenario',
      'character',
      'assets',
      'assemble',
      'complete',
    ])
    const scenario_lane = dag.lanes.find((l) => l.id === 'scenario')
    expect(scenario_lane?.state).toBe('active')
    expect(dag.lanes.find((l) => l.id === 'pending')?.state).toBe('completed')
    expect(dag.lanes.find((l) => l.id === 'character')?.state).toBe('upcoming')
  })

  it('marks the scenario lane failed when an inner stage fails', () => {
    const dag = buildStageDagTopology('write', 'failed')
    expect(dag.lanes.find((l) => l.id === 'scenario')?.state).toBe('failed')
  })

  it('assigns each node to its parent lane with relative coordinates', () => {
    const dag = buildStageDagTopology('research', 'running')
    const research = dag.nodes.find((n) => n.id === 'research')
    expect(research?.parent).toBe('scenario')
    expect(research?.rel_x).toBeGreaterThan(0)
    expect(research?.rel_y).toBeGreaterThan(0)
  })

  it('places image+tts in the left column with batch_review to the right (avoids edge collisions)', () => {
    const dag = buildStageDagTopology('image', 'running')
    const image = dag.nodes.find((n) => n.id === 'image')
    const tts = dag.nodes.find((n) => n.id === 'tts')
    const batch = dag.nodes.find((n) => n.id === 'batch_review')
    expect(image?.rel_x).toBe(tts?.rel_x)
    expect((tts?.rel_y ?? 0)).toBeGreaterThan(image?.rel_y ?? 0)
    expect((batch?.rel_x ?? 0)).toBeGreaterThan(image?.rel_x ?? 0)
    expect((batch?.rel_y ?? 0)).toBeGreaterThan(image?.rel_y ?? 0)
    expect((batch?.rel_y ?? 0)).toBeLessThan(tts?.rel_y ?? 0)
  })

  it('forks character_pick into image+tts and merges them at batch_review', () => {
    const dag = buildStageDagTopology('character_pick', 'waiting')
    const fork_edges = dag.edges.filter((e) => e.source === 'character_pick')
    expect(fork_edges.map((e) => e.target).sort()).toEqual(['image', 'tts'])
    const merge_edges = dag.edges.filter((e) => e.target === 'batch_review')
    expect(merge_edges.map((e) => e.source).sort()).toEqual(['image', 'tts'])
  })

  it('renders both image and tts as active when stage=image (parallel branch invariant)', () => {
    const dag = buildStageDagTopology('image', 'running')
    const by_id = Object.fromEntries(dag.nodes.map((n) => [n.id, n.state]))
    expect(by_id.image).toBe('active')
    expect(by_id.tts).toBe('active')
    expect(by_id.character_pick).toBe('completed')
    expect(by_id.batch_review).toBe('upcoming')
  })

  it('keeps both image and tts active when stage=tts', () => {
    const dag = buildStageDagTopology('tts', 'running')
    const by_id = Object.fromEntries(dag.nodes.map((n) => [n.id, n.state]))
    expect(by_id.image).toBe('active')
    expect(by_id.tts).toBe('active')
  })

  it('attaches the decisions counter on batch_review when stage=batch_review', () => {
    const dag = buildStageDagTopology('batch_review', 'waiting', {
      approved_count: 8,
      rejected_count: 2,
      pending_count: 22,
    })
    const batch = dag.nodes.find((n) => n.id === 'batch_review')
    expect(batch?.counter).toEqual({ done: 10, total: 32, suffix: 'reviewed' })
  })

  it('omits the counter when totals are zero (no 0/0)', () => {
    const dag = buildStageDagTopology('batch_review', 'waiting', {
      approved_count: 0,
      rejected_count: 0,
      pending_count: 0,
    })
    const batch = dag.nodes.find((n) => n.id === 'batch_review')
    expect(batch?.counter).toBeUndefined()
  })

  it('marks the active node failed and propagates upstream completed / downstream upcoming', () => {
    const dag = buildStageDagTopology('write', 'failed')
    const by_id = Object.fromEntries(dag.nodes.map((n) => [n.id, n.state]))
    expect(by_id.write).toBe('failed')
    expect(by_id.structure).toBe('completed')
    expect(by_id.research).toBe('completed')
    expect(by_id.visual_break).toBe('upcoming')
    expect(by_id.image).toBe('upcoming')
  })

  it('derives edge state — completed-to-completed is completed; entering an active node is active', () => {
    const dag = buildStageDagTopology('critic', 'running')
    const completed_edge = dag.edges.find(
      (e) => e.source === 'research' && e.target === 'structure',
    )
    expect(completed_edge?.state).toBe('completed')
    const active_edge = dag.edges.find(
      (e) => e.source === 'review' && e.target === 'critic',
    )
    expect(active_edge?.state).toBe('active')
  })
})
