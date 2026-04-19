import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  characterGroupResponseSchema,
  descriptorPrefillResponseSchema,
  runDetailResponseSchema,
  runListResponseSchema,
  runResumeResponseSchema,
  runStatusResponseSchema,
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
})
