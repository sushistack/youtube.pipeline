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

type MockES = {
  close: ReturnType<typeof vi.fn>
  onmessage: ((e: { data: string }) => void) | null
  onerror: (() => void) | null
  done_handler: (() => void) | null
  addEventListener: ReturnType<typeof vi.fn>
}

describe('useRunStatus', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('streams status via SSE and closes on terminal state', async () => {
    let mock_es: MockES | null = null

    vi.stubGlobal(
      'EventSource',
      vi.fn().mockImplementation(function () {
        mock_es = {
          close: vi.fn(),
          onmessage: null,
          onerror: null,
          done_handler: null,
          addEventListener: vi.fn((event: string, fn: () => void) => {
            if (event === 'done') mock_es!.done_handler = fn
          }),
        }
        return mock_es
      }),
    )

    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(running_status), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderWithProviders(<HookHarness />)

    await waitFor(() => expect(mock_es).not.toBeNull())

    mock_es!.onmessage!({ data: JSON.stringify(running_status) })
    expect(await screen.findByText('running')).toBeInTheDocument()

    mock_es!.onmessage!({ data: JSON.stringify(completed_status) })
    await waitFor(() => expect(screen.getByText('completed')).toBeInTheDocument())

    mock_es!.done_handler?.()
    expect(mock_es!.close).toHaveBeenCalled()
  })
})
