import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  characterGroupResponseSchema,
  createRunRequestSchema,
  createRunResponseSchema,
  descriptorPrefillResponseSchema,
  reviewItemListResponseSchema,
  runDetailResponseSchema,
  runListResponseSchema,
  runResumeResponseSchema,
  runStatusResponseSchema,
  timelineListResponseSchema,
  sceneDecisionRequestSchema,
  sceneDecisionResponseSchema,
  sceneEditResponseSchema,
  sceneListResponseSchema,
} from './runContracts'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

function loadContractFixture(name: string) {
  const fixturePath = path.resolve(__dirname, '../../../testdata/contracts', name)
  return JSON.parse(readFileSync(fixturePath, 'utf8'))
}

describe('run contract fixtures', () => {
  it('parses the checked-in detail/list/resume/status API fixtures with Zod', () => {
    expect(() =>
      runDetailResponseSchema.parse(loadContractFixture('run.detail.response.json')),
    ).not.toThrow()

    expect(() =>
      runListResponseSchema.parse(loadContractFixture('run.list.response.json')),
    ).not.toThrow()

    expect(() =>
      runResumeResponseSchema.parse(loadContractFixture('run.resume.response.json')),
    ).not.toThrow()

    expect(() =>
      runStatusResponseSchema.parse(loadContractFixture('run.status.response.json')),
    ).not.toThrow()

    expect(() =>
      runDetailResponseSchema.parse(loadContractFixture('run.failure.response.json')),
    ).not.toThrow()
  })

  it('parses the scene list and edit response fixtures', () => {
    expect(() =>
      sceneListResponseSchema.parse(loadContractFixture('run.scenes.response.json')),
    ).not.toThrow()

    expect(() =>
      sceneEditResponseSchema.parse(loadContractFixture('run.scene.edit.response.json')),
    ).not.toThrow()
  })

  it('parses the batch review item fixture', () => {
    expect(() =>
      reviewItemListResponseSchema.parse(
        loadContractFixture('run.review-items.response.json'),
      ),
    ).not.toThrow()
  })

  it('parses approve, reject, and skip decision contracts', () => {
    expect(() =>
      sceneDecisionRequestSchema.parse({
        scene_index: 3,
        decision_type: 'approve',
      }),
    ).not.toThrow()

    expect(() =>
      sceneDecisionRequestSchema.parse({
        scene_index: 3,
        decision_type: 'reject',
        note: null,
      }),
    ).not.toThrow()

    expect(() =>
      sceneDecisionRequestSchema.parse({
        scene_index: 3,
        decision_type: 'skip_and_remember',
        context_snapshot: {
          action_source: 'batch_review',
          content_flags: ['Safeguard Triggered: Minors'],
          critic_score: 84,
          critic_sub: { hook_strength: 91 },
          review_status_before: 'waiting_for_review',
          scene_index: 3,
        },
      }),
    ).not.toThrow()

    expect(() =>
      sceneDecisionResponseSchema.parse({
        version: 1,
        data: {
          scene_index: 3,
          decision_type: 'approve',
          next_scene_index: 4,
        },
      }),
    ).not.toThrow()
  })

  it('parses the decisions timeline contract with nullable timeline fields', () => {
    expect(() =>
      timelineListResponseSchema.parse({
        version: 1,
        data: {
          items: [
            {
              id: 10,
              run_id: 'run-1',
              scp_id: '049',
              scene_id: '3',
              decision_type: 'reject',
              note: null,
              reason_from_snapshot: 'needs a clearer rationale',
              superseded_by: 11,
              created_at: '2026-04-19T01:02:03Z',
            },
            {
              id: 11,
              run_id: 'run-1',
              scp_id: '049',
              scene_id: null,
              decision_type: 'undo',
              note: 'undo of decision 10',
              reason_from_snapshot: null,
              superseded_by: null,
              created_at: '2026-04-19T01:03:03Z',
            },
          ],
          next_cursor: {
            before_created_at: '2026-04-19T01:02:03Z',
            before_id: 10,
          },
        },
      }),
    ).not.toThrow()
  })

  it('parses the character candidate group fixture', () => {
    expect(() =>
      characterGroupResponseSchema.parse(
        loadContractFixture('run.character.candidates.response.json'),
      ),
    ).not.toThrow()
  })

  it('parses the descriptor prefill fixture (with and without prior)', () => {
    expect(() =>
      descriptorPrefillResponseSchema.parse(
        loadContractFixture('run.character.descriptor.response.json'),
      ),
    ).not.toThrow()

    const noPrior = {
      version: 1,
      data: { auto: 'appearance: x', prior: null },
    }
    expect(() => descriptorPrefillResponseSchema.parse(noPrior)).not.toThrow()
  })

  it('rejects descriptor fixtures that drop the prior field', () => {
    const invalid = { version: 1, data: { auto: 'x' } }
    expect(() => descriptorPrefillResponseSchema.parse(invalid)).toThrow(/prior/i)
  })

  it('fails fast when a scene fixture scene_index is not a number', () => {
    const invalid = {
      version: 1,
      data: { items: [{ scene_index: 'zero', narration: 'text' }], total: 1 },
    }
    expect(() => sceneListResponseSchema.parse(invalid)).toThrow(/scene_index/i)
  })

  it('fails fast when a checked-in fixture shape drifts', () => {
    const invalidFixture = structuredClone(loadContractFixture('run.detail.response.json'))
    invalidFixture.data.retry_count = 'two'

    expect(() => runDetailResponseSchema.parse(invalidFixture)).toThrow(/retry_count/i)
  })

  it('parses the create-run request and success envelope', () => {
    expect(() =>
      createRunRequestSchema.parse({
        scp_id: '173-J',
      }),
    ).not.toThrow()

    expect(() =>
      createRunResponseSchema.parse({
        version: 1,
        data: {
          cost_usd: 0,
          created_at: '2026-04-19T00:00:00Z',
          duration_ms: 0,
          human_override: false,
          id: 'scp-049-run-1',
          retry_count: 0,
          scp_id: '049',
          stage: 'pending',
          status: 'pending',
          token_in: 0,
          token_out: 0,
          updated_at: '2026-04-19T00:00:00Z',
        },
        error: null,
      }),
    ).not.toThrow()
  })
})
