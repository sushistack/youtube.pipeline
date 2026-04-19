import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  runDetailResponseSchema,
  runListResponseSchema,
  runResumeResponseSchema,
} from './runContracts'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

function loadContractFixture(name: string) {
  const fixturePath = path.resolve(__dirname, '../../../testdata/contracts', name)
  return JSON.parse(readFileSync(fixturePath, 'utf8'))
}

describe('run contract fixtures', () => {
  it('parses the checked-in detail/list/resume API fixtures with Zod', () => {
    expect(() =>
      runDetailResponseSchema.parse(loadContractFixture('run.detail.response.json')),
    ).not.toThrow()

    expect(() =>
      runListResponseSchema.parse(loadContractFixture('run.list.response.json')),
    ).not.toThrow()

    expect(() =>
      runResumeResponseSchema.parse(loadContractFixture('run.resume.response.json')),
    ).not.toThrow()
  })

  it('fails fast when a checked-in fixture shape drifts', () => {
    const invalidFixture = structuredClone(loadContractFixture('run.detail.response.json'))
    invalidFixture.data.retry_count = 'two'

    expect(() => runDetailResponseSchema.parse(invalidFixture)).toThrow(/retry_count/i)
  })
})
