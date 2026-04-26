import { screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RunSummary } from '../../contracts/runContracts'
import { CharacterPick } from './CharacterPick'
import { renderWithProviders } from '../../test/renderWithProviders'

function makeRun(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    cost_usd: 0,
    created_at: '2026-04-18T00:00:00Z',
    critic_score: null,
    duration_ms: 0,
    human_override: false,
    id: 'scp-049-run-1',
    retry_count: 0,
    retry_reason: null,
    scp_id: '049',
    stage: 'character_pick',
    status: 'waiting',
    token_in: 0,
    token_out: 0,
    updated_at: '2026-04-18T00:00:00Z',
    ...overrides,
  }
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    status,
  })
}

function makeCandidatesResponse(count = 10) {
  const candidates = []
  for (let i = 1; i <= count; i += 1) {
    candidates.push({
      id: `scp-049#${i}`,
      image_url: `https://example.com/scp-049/${i}.jpg`,
      page_url: `https://example.com/scp-049/${i}`,
      preview_url: `https://example.com/scp-049/${i}-thumb.jpg`,
    })
  }
  return jsonResponse({
    version: 1,
    data: { query: 'SCP-049', query_key: 'scp-049', candidates },
  })
}

function makeDescriptorResponse(auto: string, prior: string | null) {
  return jsonResponse({ version: 1, data: { auto, prior } })
}

function makePickResponse(run: RunSummary) {
  return jsonResponse({ version: 1, data: run })
}

interface FetchCall {
  url: string
  init?: RequestInit
}

function spyFetch(responder: (url: string, init?: RequestInit) => Response | Promise<Response>) {
  const calls: FetchCall[] = []
  const spy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = typeof input === 'string' ? input : (input as Request).url
    calls.push({ url, init: init ?? undefined })
    return responder(url, init)
  })
  return { calls, spy }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('CharacterPick', () => {
  it('auto-loads candidates when character_query_key is already set', async () => {
    const { calls } = spyFetch((url) => {
      if (url.includes('/characters/descriptor')) {
        return makeDescriptorResponse('auto-desc', null)
      }
      if (url.includes('/characters')) {
        return makeCandidatesResponse(10)
      }
      throw new Error(`unexpected url: ${url}`)
    })

    const run = makeRun({ character_query_key: 'scp-049' })
    renderWithProviders(<CharacterPick run={run} />)

    const grid = await screen.findByTestId('character-grid')
    expect(within(grid).getAllByRole('option')).toHaveLength(10)
    // The first call must be the cache-restore GET with no query param.
    const character_calls = calls.filter((c) => c.url.includes('/characters'))
    expect(character_calls[0].url).not.toMatch(/query=/)
  })

  it('submits a search when no character_query_key is present', async () => {
    const { calls } = spyFetch((url) => {
      if (url.includes('/characters?query=')) {
        return makeCandidatesResponse(10)
      }
      throw new Error(`unexpected url: ${url}`)
    })

    const run = makeRun({ character_query_key: null })
    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)

    const input = screen.getByLabelText(/search query/i)
    await user.type(input, 'SCP-049')
    await user.click(screen.getByRole('button', { name: /search/i }))

    const grid = await screen.findByTestId('character-grid')
    expect(within(grid).getAllByRole('option')).toHaveLength(10)
    expect(calls.some((c) => c.url.includes('query=SCP-049'))).toBe(true)
  })

  it('selects candidates via digit keys 1–9 and 0', async () => {
    spyFetch((url) => {
      if (url.includes('/characters/descriptor')) {
        return makeDescriptorResponse('auto-desc', null)
      }
      return makeCandidatesResponse(10)
    })

    const run = makeRun({ character_query_key: 'scp-049' })
    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)

    const grid = await screen.findByTestId('character-grid')
    grid.focus()
    await user.keyboard('3')
    expect(screen.getByTestId('character-grid-cell-3')).toHaveAttribute('aria-selected', 'true')

    await user.keyboard('0')
    expect(screen.getByTestId('character-grid-cell-0')).toHaveAttribute('aria-selected', 'true')
  })

  it('Esc in grid returns to the search phase', async () => {
    spyFetch(() => makeCandidatesResponse(10))

    const run = makeRun({ character_query_key: 'scp-049' })
    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)
    const grid = await screen.findByTestId('character-grid')
    grid.focus()
    await user.keyboard('{Escape}')
    expect(screen.getByLabelText(/search query/i)).toBeInTheDocument()
  })

  it('Enter after selection advances to descriptor phase and fetches prefill', async () => {
    const { calls } = spyFetch((url) => {
      if (url.includes('/characters/descriptor')) {
        return makeDescriptorResponse('auto-desc-value', 'prior-desc-value')
      }
      return makeCandidatesResponse(10)
    })

    const run = makeRun({ character_query_key: 'scp-049' })
    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)

    const grid = await screen.findByTestId('character-grid')
    grid.focus()
    await user.keyboard('2')
    await user.keyboard('{Enter}')

    await waitFor(() => {
      expect(screen.getByText('prior-desc-value')).toBeInTheDocument()
    })
    expect(calls.some((c) => c.url.includes('/characters/descriptor'))).toBe(true)
  })

  it('falls back to search phase when cached candidate fetch returns 404', async () => {
    spyFetch((url) => {
      if (url.includes('/characters')) {
        return jsonResponse(
          { version: 1, error: { code: 'NOT_FOUND', message: 'no cached group' } },
          404,
        )
      }
      throw new Error(`unexpected url: ${url}`)
    })

    const run = makeRun({ character_query_key: 'scp-049' })
    renderWithProviders(<CharacterPick run={run} />)

    // The component mounts with phase='grid' because character_query_key is
    // present, but the cache-restore fetch 404s. The UI must auto-recover
    // back to the search input rather than stranding at grid.
    await waitFor(() => {
      expect(screen.getByLabelText(/search query/i)).toBeInTheDocument()
    })
  })

  it('Esc in grid clears the selection before returning to search', async () => {
    spyFetch((url) => {
      if (url.includes('/characters/descriptor')) {
        return makeDescriptorResponse('auto-desc', null)
      }
      return makeCandidatesResponse(10)
    })

    const run = makeRun({ character_query_key: 'scp-049' })
    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)
    const grid = await screen.findByTestId('character-grid')
    grid.focus()
    // Pick candidate 3 then Esc back to search; a later re-entry to grid
    // must not retain the stale selection.
    await user.keyboard('3')
    expect(screen.getByTestId('character-grid-cell-3')).toHaveAttribute('aria-selected', 'true')
    await user.keyboard('{Escape}')

    const input = await screen.findByLabelText(/search query/i)
    expect(input).toBeInTheDocument()

    // Submit again to re-enter grid; no cell should be pre-selected.
    await user.type(input, 'SCP-049')
    await user.click(screen.getByRole('button', { name: /search/i }))
    const gridAgain = await screen.findByTestId('character-grid')
    const selected = within(gridAgain)
      .getAllByRole('option')
      .filter((c) => c.getAttribute('aria-selected') === 'true')
    expect(selected).toHaveLength(0)
  })

  it('prefills the search input from the run character_query_key', async () => {
    spyFetch((url) => {
      if (url.includes('/characters?query=')) {
        return makeCandidatesResponse(10)
      }
      throw new Error(`unexpected url: ${url}`)
    })

    // Start at search phase (no character_query_key) then switch: actually
    // simplest — pass character_query_key=null and no query. But the ask is
    // that when a key exists, a search submit from the recovered phase uses
    // the prior value without retyping. Assert via controlled input value.
    const run = makeRun({ character_query_key: null })
    const { unmount } = renderWithProviders(<CharacterPick run={run} />)
    const input = screen.getByLabelText(/search query/i) as HTMLInputElement
    expect(input.value).toBe('')
    unmount()

    const runWithKey = makeRun({ character_query_key: 'scp-049-prior' })
    // character_query_key → grid phase; simulate 404 to bounce back to search.
    spyFetch((url) => {
      if (url.includes('/characters?query=')) {
        return makeCandidatesResponse(10)
      }
      if (url.includes('/characters')) {
        return jsonResponse(
          { version: 1, error: { code: 'NOT_FOUND', message: 'evicted' } },
          404,
        )
      }
      throw new Error(`unexpected url: ${url}`)
    })
    renderWithProviders(<CharacterPick run={runWithKey} />)
    const prefilledInput = (await screen.findByLabelText(/search query/i)) as HTMLInputElement
    expect(prefilledInput.value).toBe('scp-049-prior')
  })

  it('renders Confirm selection disabled until candidate clicked, then advances to descriptor phase on click', async () => {
    spyFetch((url) => {
      if (url.includes('/characters/descriptor')) {
        return makeDescriptorResponse('auto-desc-button', null)
      }
      return makeCandidatesResponse(10)
    })

    const run = makeRun({ character_query_key: 'scp-049' })
    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)

    await screen.findByTestId('character-grid')
    const confirm_btn = screen.getByRole('button', { name: /confirm selection/i })
    expect(confirm_btn).toBeDisabled()

    await user.click(screen.getByTestId('character-grid-cell-2'))
    expect(confirm_btn).toBeEnabled()

    await user.click(confirm_btn)
    await waitFor(() => {
      expect(screen.getByText('auto-desc-button')).toBeInTheDocument()
    })
  })

  it('renders Search again that returns to search phase and clears selection', async () => {
    spyFetch((url) => {
      if (url.includes('/characters/descriptor')) {
        return makeDescriptorResponse('auto-desc', null)
      }
      return makeCandidatesResponse(10)
    })

    const run = makeRun({ character_query_key: 'scp-049' })
    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)

    await screen.findByTestId('character-grid')
    await user.click(screen.getByTestId('character-grid-cell-3'))
    expect(screen.getByTestId('character-grid-cell-3')).toHaveAttribute(
      'aria-selected',
      'true',
    )

    await user.click(screen.getByRole('button', { name: /search again/i }))
    const input = await screen.findByLabelText(/search query/i)
    expect(input).toBeInTheDocument()

    await user.type(input, 'SCP-049')
    await user.click(screen.getByRole('button', { name: /^search$/i }))
    const grid_again = await screen.findByTestId('character-grid')
    const selected = within(grid_again)
      .getAllByRole('option')
      .filter((c) => c.getAttribute('aria-selected') === 'true')
    expect(selected).toHaveLength(0)
  })

  it('confirm triggers pickCharacterWithDescriptor mutation', async () => {
    const run = makeRun({ character_query_key: 'scp-049' })
    const { calls } = spyFetch((url, init) => {
      if (url.includes('/characters/descriptor')) {
        return makeDescriptorResponse('auto-desc-value', null)
      }
      if (url.includes('/characters/pick')) {
        expect(init?.method).toBe('POST')
        return makePickResponse({ ...run, stage: 'image', status: 'running' })
      }
      return makeCandidatesResponse(10)
    })

    const user = userEvent.setup()
    renderWithProviders(<CharacterPick run={run} />)
    const grid = await screen.findByTestId('character-grid')
    grid.focus()
    await user.keyboard('1')
    await user.keyboard('{Enter}')

    const read_mode = await screen.findByRole('button', {
      name: /vision descriptor draft/i,
    })
    read_mode.focus()
    await user.keyboard('{Enter}')

    await waitFor(() => {
      expect(calls.some((c) => c.url.includes('/characters/pick'))).toBe(true)
    })
    const pick_call = calls.find((c) => c.url.includes('/characters/pick'))
    expect(pick_call?.init?.body).toContain('auto-desc-value')
    expect(pick_call?.init?.body).toContain('scp-049#1')
  })
})
