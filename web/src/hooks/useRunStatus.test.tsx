import { screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useRunStatus } from './useRunStatus'
import { renderWithProviders } from '../test/renderWithProviders'

const running_status = {
  data: {
    run: {
      cost_usd: 2.5,
      created_at: '2026-04-19T00:00:00Z',
      duration_ms: 125000,
      human_override: false,
      id: 'scp-049-run-2',
      retry_count: 0,
      scp_id: '049',
      stage: 'write',
      status: 'running',
      token_in: 1200,
      token_out: 340,
      updated_at: '2026-04-19T01:00:00Z',
    },
  },
  version: 1,
}

const completed_status = {
  data: {
    run: {
      ...running_status.data.run,
      stage: 'complete',
      status: 'completed',
      updated_at: '2026-04-19T01:05:00Z',
    },
  },
  version: 1,
}

function HookHarness() {
  const query = useRunStatus('scp-049-run-2')

  return (
    <div>
      <span>{query.data?.run.status ?? 'loading'}</span>
      <span>{String(query.isFetching)}</span>
    </div>
  )
}

describe('useRunStatus', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('polls every 5 seconds and stops once the run is no longer live', async () => {
    let status_calls = 0

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : input.url
      if (url.endsWith('/api/runs/scp-049-run-2/status')) {
        status_calls += 1
        const payload = status_calls === 1 ? running_status : completed_status
        return new Response(JSON.stringify(payload), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    renderWithProviders(<HookHarness />)

    expect(await screen.findByText('running')).toBeInTheDocument()
    expect(status_calls).toBe(1)

    await new Promise((resolve) => {
      setTimeout(resolve, 5200)
    })

    await waitFor(() => {
      expect(screen.getByText('completed')).toBeInTheDocument()
      expect(status_calls).toBe(2)
    })

    await new Promise((resolve) => {
      setTimeout(resolve, 5200)
    })

    expect(status_calls).toBe(2)
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  }, 15_000)
})
