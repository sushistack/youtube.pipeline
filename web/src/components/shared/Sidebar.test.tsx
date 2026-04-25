import React from 'react'
import { screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useSearchParams } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { renderWithProviders } from '../../test/renderWithProviders'
import { NewRunCoordinatorProvider } from '../production/NewRunContext'
import { Sidebar } from './Sidebar'

function SearchParamProbe() {
  const [search_params] = useSearchParams()
  return <output>selected-run:{search_params.get('run') ?? 'none'}</output>
}

function renderSidebar(initial_entry: string, props?: Partial<React.ComponentProps<typeof Sidebar>>) {
  return renderWithProviders(
    <KeyboardShortcutsProvider>
      <NewRunCoordinatorProvider>
        <Routes>
          <Route
            path="*"
            element={
              <>
                <Sidebar
                  collapsed={props?.collapsed ?? false}
                  forced_collapsed={props?.forced_collapsed ?? false}
                  on_toggle={props?.on_toggle ?? vi.fn()}
                />
                <SearchParamProbe />
              </>
            }
          />
        </Routes>
      </NewRunCoordinatorProvider>
    </KeyboardShortcutsProvider>,
    {
      initialEntries: [initial_entry],
    },
  )
}

function requestUrl(input: string | URL | Request) {
  if (typeof input === 'string') {
    return input
  }
  if (input instanceof URL) {
    return input.href
  }
  return input.url
}

function runListResponse(items: Array<Record<string, unknown>>) {
  return {
    data: {
      items,
      total: items.length,
    },
    version: 1,
  }
}

const initial_runs = [
  {
    cost_usd: 0.45,
    created_at: '2026-04-19T00:00:00Z',
    duration_ms: 32000,
    human_override: false,
    id: 'scp-173-run-1',
    retry_count: 0,
    scp_id: '173',
    stage: 'pending',
    status: 'pending',
    token_in: 0,
    token_out: 0,
    updated_at: '2026-04-19T00:05:00Z',
  },
]

const created_run = {
  cost_usd: 0,
  created_at: '2026-04-19T00:06:00Z',
  duration_ms: 0,
  human_override: false,
  id: 'scp-049-run-1',
  retry_count: 0,
  scp_id: '049',
  stage: 'pending',
  status: 'pending',
  token_in: 0,
  token_out: 0,
  updated_at: '2026-04-19T00:06:00Z',
}

describe('Sidebar', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the single Recent-runs section with compact dot+title rows on production route', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(runListResponse(initial_runs)), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderSidebar('/production')

    expect(
      await screen.findByRole('heading', { name: 'Recent runs' }),
    ).toBeInTheDocument()
    expect(screen.queryByPlaceholderText('Search runs')).not.toBeInTheDocument()
    expect(screen.queryByText(/active runs/i)).not.toBeInTheDocument()

    const run_button = await screen.findByRole('button', { name: /scp-173 run #1/i })
    expect(run_button.querySelector('.run-card__dot')).toHaveAttribute(
      'data-tone',
      'subtle',
    )
    expect(run_button.querySelector('.run-card__title')).toHaveTextContent(
      'SCP-173 Run #1',
    )
  })

  it('renders SVG icons (not letter abbreviations) for the workflow nav links', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(runListResponse(initial_runs)), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderSidebar('/production')

    for (const label of ['Production', 'Tuning', 'Settings']) {
      const link = await screen.findByRole('link', { name: label })
      // lucide-react renders inline <svg>; assert the SVG presence and the
      // absence of the legacy two-letter abbreviation that this redesign replaced.
      expect(link.querySelector('svg')).toBeInTheDocument()
      const abbreviation = label.slice(0, 2).toUpperCase()
      expect(link).not.toHaveTextContent(new RegExp(`^${abbreviation}\\s*${label}$`))
    }
  })

  it('renders the New Run button on production only, with expanded and collapsed affordances', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(runListResponse(initial_runs)), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    const first_render = renderSidebar('/production')

    expect(
      await screen.findByRole('button', { name: 'Create a new pipeline run' }),
    ).toBeInTheDocument()
    expect(screen.getByText('New Run')).toBeInTheDocument()

    first_render.unmount()

    const second_render = renderSidebar('/settings')
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: 'Create a new pipeline run' }),
      ).not.toBeInTheDocument()
    })

    second_render.unmount()

    const tuning_render = renderSidebar('/tuning')
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: 'Create a new pipeline run' }),
      ).not.toBeInTheDocument()
    })

    tuning_render.unmount()

    renderSidebar('/production', { collapsed: true })
    expect(
      await screen.findByRole('button', { name: 'Create a new pipeline run' }),
    ).toHaveAttribute('title', 'Create a new pipeline run')
  })

  it('does not register a Ctrl+N / Cmd+N keyboard shortcut (browser conflict)', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(runListResponse(initial_runs)), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderSidebar('/production')
    await screen.findByRole('button', { name: 'Create a new pipeline run' })

    const ctrl_shortcut = new KeyboardEvent('keydown', {
      bubbles: true,
      cancelable: true,
      ctrlKey: true,
      key: 'n',
    })
    window.dispatchEvent(ctrl_shortcut)

    expect(ctrl_shortcut.defaultPrevented).toBe(false)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()

    const meta_shortcut = new KeyboardEvent('keydown', {
      bubbles: true,
      cancelable: true,
      key: 'n',
      metaKey: true,
    })
    window.dispatchEvent(meta_shortcut)

    expect(meta_shortcut.defaultPrevented).toBe(false)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('creates a run, invalidates inventory, selects it, and restores focus to the trigger', async () => {
    const user = userEvent.setup()
    let runs = [...initial_runs]
    const fetch_spy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async (input, init) => {
        const url = requestUrl(input)

        if (url.endsWith('/api/runs') && init?.method === 'POST') {
          expect(init.body).toBe(JSON.stringify({ scp_id: '049' }))
          runs = [created_run, ...runs]
          return new Response(
            JSON.stringify({
              data: created_run,
              error: null,
              version: 1,
            }),
            {
              headers: { 'Content-Type': 'application/json' },
              status: 201,
            },
          )
        }

        if (url.endsWith('/api/runs')) {
          return new Response(JSON.stringify(runListResponse(runs)), {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          })
        }

        throw new Error(`Unexpected fetch in test: ${url}`)
      })

    renderSidebar('/production')

    const trigger = await screen.findByRole('button', {
      name: 'Create a new pipeline run',
    })
    await user.click(trigger)
    await user.type(screen.getByRole('textbox', { name: 'SCP ID' }), '049')
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => {
      expect(fetch_spy.mock.calls.length).toBeGreaterThanOrEqual(3)
    })
    expect(await screen.findByText(/selected-run:scp-049-run-1/)).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /scp-049/i })).toBeInTheDocument()

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })
    expect(trigger).toHaveFocus()
  })
})
