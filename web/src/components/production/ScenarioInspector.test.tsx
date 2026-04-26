import { fireEvent, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import { describe, expect, it, vi } from 'vitest'
import { ScenarioInspector } from './ScenarioInspector'
import { renderWithProviders } from '../../test/renderWithProviders'

function buildScenesResponse(items: { scene_index: number; narration: string }[]) {
  return new Response(
    JSON.stringify({
      data: { items, total: items.length },
      version: 1,
    }),
    {
      headers: { 'Content-Type': 'application/json' },
      status: 200,
    },
  )
}

describe('ScenarioInspector', () => {
  it('renders scene list with scene labels and narration text', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      buildScenesResponse([
        { scene_index: 0, narration: 'SCP-049는 흑사병 의사입니다.' },
        { scene_index: 1, narration: '그의 손길이 닿으면 모든 것이 멈춥니다.' },
      ]),
    )

    renderWithProviders(<ScenarioInspector run_id="scp-049-run-1" />)

    expect(await screen.findByText('Scene 1')).toBeInTheDocument()
    expect(await screen.findByText('SCP-049는 흑사병 의사입니다.')).toBeInTheDocument()
    expect(screen.getByText('Scene 2')).toBeInTheDocument()
    expect(screen.getByText('그의 손길이 닿으면 모든 것이 멈춥니다.')).toBeInTheDocument()
  })

  it('shows narration in scene_index order from API payload', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      buildScenesResponse([
        { scene_index: 0, narration: '첫 장면' },
        { scene_index: 1, narration: '두 번째 장면' },
        { scene_index: 2, narration: '세 번째 장면' },
      ]),
    )

    renderWithProviders(<ScenarioInspector run_id="run-test" />)

    const labels = await screen.findAllByText(/^Scene \d+$/)
    expect(labels).toHaveLength(3)
    expect(labels[0]).toHaveTextContent('Scene 1')
    expect(labels[2]).toHaveTextContent('Scene 3')
  })

  it('shows loading state before data arrives', () => {
    vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise(() => {}))
    renderWithProviders(<ScenarioInspector run_id="run-loading" />)
    expect(screen.getByText(/loading scenes/i)).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Network error'))
    renderWithProviders(<ScenarioInspector run_id="run-err" />)

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent(/failed to load scenes/i)
  })

  it('shows empty state when no scenes are returned', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      buildScenesResponse([]),
    )
    renderWithProviders(<ScenarioInspector run_id="run-empty" />)

    expect(await screen.findByText(/no narration scenes found/i)).toBeInTheDocument()
  })

  it('renders approve button when scenes load and POSTs on click', async () => {
    const fetch_spy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/scenario/approve')) {
        return new Response(
          JSON.stringify({
            data: {
              id: 'scp-049-run-1',
              scp_id: '049',
              stage: 'character_pick',
              status: 'waiting',
              retry_count: 0,
              created_at: '2026-04-26T00:00:00Z',
              updated_at: '2026-04-26T00:00:00Z',
            },
            version: 1,
          }),
          { headers: { 'Content-Type': 'application/json' }, status: 200 },
        )
      }
      return buildScenesResponse([{ scene_index: 0, narration: '첫 장면' }])
    })

    renderWithProviders(<ScenarioInspector run_id="scp-049-run-1" />)

    const button = await screen.findByRole('button', { name: /approve scenario/i })
    expect(button).toBeEnabled()

    fireEvent.click(button)

    await waitFor(() => {
      const approve_calls = fetch_spy.mock.calls.filter((call) => {
        const u = typeof call[0] === 'string' ? call[0] : (call[0] as Request).url
        return u.includes('/scenario/approve')
      })
      expect(approve_calls).toHaveLength(1)
    })
  })

  it('keeps approve button enabled and shows error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : (input as Request).url
      if (url.includes('/scenario/approve')) {
        return new Response(
          JSON.stringify({ error: { code: 'CONFLICT', message: 'wrong stage' } }),
          { headers: { 'Content-Type': 'application/json' }, status: 409 },
        )
      }
      return buildScenesResponse([{ scene_index: 0, narration: '첫 장면' }])
    })

    renderWithProviders(<ScenarioInspector run_id="scp-049-run-1" />)

    const button = await screen.findByRole('button', { name: /approve scenario/i })
    fireEvent.click(button)

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/wrong stage|approve/i)
    })
    // After error the button must still be clickable for a retry attempt.
    expect(screen.getByRole('button', { name: /approve scenario/i })).toBeEnabled()
  })
})
