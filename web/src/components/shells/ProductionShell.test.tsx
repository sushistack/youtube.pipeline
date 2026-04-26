import { screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { Route, Routes, useSearchParams } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppShell } from '../shared/AppShell'
import { ProductionShell } from './ProductionShell'
import { renderWithProviders } from '../../test/renderWithProviders'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { useUIStore } from '../../stores/useUIStore'

function SelectedRunProbe() {
  const [search_params] = useSearchParams()
  return <output data-testid="selected-run-probe">{search_params.get('run') ?? 'none'}</output>
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

const run_list_response = {
  data: {
    items: [
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
      {
        cost_usd: 2.5,
        created_at: '2026-04-19T00:00:00Z',
        critic_score: 88,
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
    ],
    total: 2,
  },
  version: 1,
}

const run_status_response = {
  data: {
    changes_since_last_interaction: [
      {
        after: 'approved',
        before: 'pending',
        kind: 'scene_status_flipped',
        scene_id: '4',
      },
    ],
    decisions_summary: {
      approved_count: 3,
      pending_count: 2,
      rejected_count: 0,
    },
    run: run_list_response.data.items[1],
    summary: 'Script writing is actively processing',
  },
  version: 1,
}

describe('ProductionShell integration', () => {
  beforeEach(() => {
    localStorage.clear()
    useUIStore.setState({
      onboarding_dismissed: true,
      production_last_seen: {},
      sidebar_collapsed: false,
    })
    // useRunStatus opens an SSE EventSource on mount; jsdom does not provide
    // EventSource, so stub a no-op constructor so the hook's effect is inert
    // for these tests (they exercise REST-driven flows, not the SSE stream).
    vi.stubGlobal(
      'EventSource',
      vi.fn().mockImplementation(function FakeEventSource(
        this: { close: () => void; onmessage: null; onerror: null; addEventListener: () => void },
      ) {
        this.close = vi.fn()
        this.onmessage = null
        this.onerror = null
        this.addEventListener = vi.fn()
      }),
    )
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('renders the Master-Detail shell with app header, sidebar Recent runs, and live status bar', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = requestUrl(input)

      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify(run_list_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-049-run-2/status')) {
        return new Response(JSON.stringify(run_status_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    expect(
      await screen.findByRole('heading', { name: 'SCP-049 Run #2' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Tuning' })).toBeInTheDocument()
    expect(screen.getByTestId('status-bar')).toHaveAttribute('data-visible', 'true')
    expect(
      screen.getAllByRole('list', { name: /pipeline progress/i }).length,
    ).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /scp-049 run #2/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /scp-173 run #1/i })).toBeInTheDocument()
    expect(screen.queryByPlaceholderText('Search runs')).not.toBeInTheDocument()
    expect(screen.queryByText(/six-node pipeline map/i)).not.toBeInTheDocument()
    expect(screen.queryByText('Decision state')).not.toBeInTheDocument()
  }, 10_000)

  it('does not render a continuity banner on the first production visit for a run', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = requestUrl(input)

      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify(run_list_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-049-run-2/status')) {
        return new Response(JSON.stringify(run_status_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByText('What changed since last session')).not.toBeInTheDocument()
    })
  })

  it('renders a continuity banner when the persisted snapshot is stale and backend change data exists', async () => {
    useUIStore.setState({
      onboarding_dismissed: true,
      production_last_seen: {
        'scp-049-run-2': {
          run_id: 'scp-049-run-2',
          stage: 'write',
          status: 'running',
          updated_at: '2026-04-19T00:30:00Z',
        },
      },
      sidebar_collapsed: false,
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = requestUrl(input)

      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify(run_list_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-049-run-2/status')) {
        return new Response(JSON.stringify(run_status_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByText('What changed since last session')).toBeInTheDocument()
    expect(screen.getByText('Scene 4 moved from pending to approved')).toBeInTheDocument()
  })

  it('does not render the continuity banner when the persisted snapshot matches the live run', async () => {
    useUIStore.setState({
      onboarding_dismissed: true,
      production_last_seen: {
        'scp-049-run-2': {
          run_id: 'scp-049-run-2',
          stage: 'write',
          status: 'running',
          updated_at: '2026-04-19T01:00:00Z',
        },
      },
      sidebar_collapsed: false,
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = requestUrl(input)

      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify(run_list_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-049-run-2/status')) {
        return new Response(JSON.stringify(run_status_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByText('What changed since last session')).not.toBeInTheDocument()
    })
  })

  it('persists the current snapshot after dismissing the banner so it does not reappear on remount', async () => {
    const user = userEvent.setup()

    useUIStore.setState({
      onboarding_dismissed: true,
      production_last_seen: {
        'scp-049-run-2': {
          run_id: 'scp-049-run-2',
          stage: 'write',
          status: 'running',
          updated_at: '2026-04-19T00:30:00Z',
        },
      },
      sidebar_collapsed: false,
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = requestUrl(input)

      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify(run_list_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-049-run-2/status')) {
        return new Response(JSON.stringify(run_status_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    const view = renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByText('What changed since last session')).toBeInTheDocument()

    await user.keyboard('{Enter}')

    await waitFor(() => {
      expect(screen.queryByText('What changed since last session')).not.toBeInTheDocument()
    })
    expect(useUIStore.getState().production_last_seen['scp-049-run-2']).toEqual({
      run_id: 'scp-049-run-2',
      stage: 'write',
      status: 'running',
      updated_at: '2026-04-19T01:00:00Z',
    })

    view.unmount()

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByText('What changed since last session')).not.toBeInTheDocument()
    })
  })

  it('renders the batch review surface when the selected run is paused at batch_review', async () => {
    const batchReviewRun = {
      ...run_list_response.data.items[1],
      stage: 'batch_review',
      status: 'waiting',
    }
    const batchStatusResponse = {
      ...run_status_response,
      data: {
        ...run_status_response.data,
        run: batchReviewRun,
      },
    }
    const reviewItemsResponse = {
      data: {
        items: [
          {
            clip_path: null,
            critic_breakdown: null,
            critic_score: 84,
            high_leverage: true,
            high_leverage_reason: 'Opening hook scene',
            high_leverage_reason_code: 'hook_scene',
            narration: 'Scene 0 review copy',
            previous_version: null,
            review_status: 'waiting_for_review',
            scene_index: 0,
            shots: [
              {
                image_path: '/images/scene-0.png',
                duration_s: 4,
                transition: 'cut',
                visual_descriptor: 'scene zero',
              },
            ],
          },
        ],
        total: 1,
      },
      version: 1,
    }

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = requestUrl(input)

      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify({
          ...run_list_response,
          data: {
            ...run_list_response.data,
            items: [run_list_response.data.items[0], batchReviewRun],
          },
        }), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-049-run-2/status')) {
        return new Response(JSON.stringify(batchStatusResponse), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-049-run-2/review-items')) {
        return new Response(JSON.stringify(reviewItemsResponse), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByLabelText(/batch review layout/i)).toBeInTheDocument()
    expect(screen.getAllByText('Scene 0 review copy')).toHaveLength(2)
  })

  it('renders the pending run guidance card and starts the run via Start run button', async () => {
    const advance_calls: string[] = []

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = requestUrl(input)

      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify(run_list_response), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (url.endsWith('/api/runs/scp-173-run-1/status')) {
        return new Response(JSON.stringify({
          data: {
            run: run_list_response.data.items[0],
            summary: 'Queued and waiting to start',
          },
          version: 1,
        }), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }

      if (
        url.endsWith('/api/runs/scp-173-run-1/advance') &&
        init?.method === 'POST'
      ) {
        advance_calls.push(url)
        return new Response(
          JSON.stringify({
            data: {
              ...run_list_response.data.items[0],
              stage: 'scenario_review',
              status: 'waiting',
            },
            version: 1,
          }),
          { headers: { 'Content-Type': 'application/json' }, status: 200 },
        )
      }

      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    const user = userEvent.setup()

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production?run=scp-173-run-1'],
      },
    )

    const pending_guidance = await screen.findByLabelText('Pending run guidance')
    expect(pending_guidance).toBeInTheDocument()
    expect(
      within(pending_guidance).getByRole('heading', { name: 'SCP-173 Run #1' }),
    ).toBeInTheDocument()
    expect(within(pending_guidance).getByText('scp-173-run-1')).toBeInTheDocument()
    expect(
      screen.getByText(/Run created\. It has not started yet\./i),
    ).toBeInTheDocument()

    expect(
      screen.queryByRole('button', { name: 'Copy command' }),
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Start run' }))

    await waitFor(() => {
      expect(advance_calls).toHaveLength(1)
    })
  })

  it('renders the empty-state New Run CTA when no runs exist', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          data: { items: [], total: 0 },
          version: 1,
        }),
        {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        },
      ),
    )

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route path="production" element={<ProductionShell />} />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      {
        initialEntries: ['/production'],
      },
    )

    expect(await screen.findByText('No runs yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New Run' })).toBeInTheDocument()
  })

  // AC1/AC2 regression: a newly created run must remain selected across the
  // dialog close + invalidateQueries refetch window. The Sidebar writes the
  // explicit `?run=<new id>` and the ProductionShell fallback must not race
  // it. The fix combines two pieces:
  //   1) `createRunResponseSchema` no longer requires `error: null` so the
  //      success path actually invokes `Sidebar.handleNewRunSuccess` (the Go
  //      envelope omits the error key on success via `json:"error,omitempty"`).
  //   2) ProductionShell's fallback effect runs at most once per mount so it
  //      cannot overwrite the post-create URL during a later refetch.
  it('preserves the post-create URL across the inventory refetch window without the fallback overwriting it', async () => {
    const user = userEvent.setup()
    const created_run = {
      cost_usd: 0,
      created_at: '2026-04-19T00:06:00Z',
      duration_ms: 0,
      human_override: false,
      id: 'scp-049-run-1',
      retry_count: 0,
      scp_id: '049',
      stage: 'pending' as const,
      status: 'pending' as const,
      token_in: 0,
      token_out: 0,
      updated_at: '2026-04-19T00:06:00Z',
    }
    let runs: Array<typeof created_run> = []

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = requestUrl(input)
      if (url.endsWith('/api/runs') && init?.method === 'POST') {
        runs = [created_run]
        // Mirror the Go server envelope: encoding/json omits the error key on
        // success because of `json:"error,omitempty"` on a *apiError pointer.
        return new Response(JSON.stringify({ data: created_run, version: 1 }), {
          headers: { 'Content-Type': 'application/json' },
          status: 201,
        })
      }
      if (url.endsWith('/api/runs')) {
        return new Response(JSON.stringify({
          data: { items: runs, total: runs.length },
          version: 1,
        }), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }
      if (url.endsWith('/api/runs/scp-049-run-1/status')) {
        return new Response(JSON.stringify({
          data: { run: created_run, summary: 'Created' },
          version: 1,
        }), {
          headers: { 'Content-Type': 'application/json' },
          status: 200,
        })
      }
      throw new Error(`Unexpected fetch in test: ${url}`)
    })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route
              path="production"
              element={
                <>
                  <ProductionShell />
                  <SelectedRunProbe />
                </>
              }
            />
          </Route>
        </Routes>
      </KeyboardShortcutsProvider>,
      { initialEntries: ['/production'] },
    )

    // Bootstrap state: empty inventory, fallback effect cannot seed anything.
    expect(await screen.findByText('No runs yet')).toBeInTheDocument()
    expect(screen.getByTestId('selected-run-probe')).toHaveTextContent('none')

    await user.click(screen.getByRole('button', { name: 'Create a new pipeline run' }))
    await user.type(screen.getByRole('textbox', { name: 'SCP ID' }), '049')
    await user.click(screen.getByRole('button', { name: 'Create' }))

    // Sidebar's set_search_params must have committed `?run=scp-049-run-1`
    // and the ProductionShell fallback must not have replaced it.
    await waitFor(() => {
      expect(screen.getByTestId('selected-run-probe')).toHaveTextContent(
        'scp-049-run-1',
      )
    })

    // The dialog closed and the pending guidance card resolves to the new run
    // (proves `current_run` matched the explicit selection, not a fallback).
    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })
    const guidance = await screen.findByLabelText('Pending run guidance')
    expect(
      within(guidance).getByRole('heading', { name: 'SCP-049 Run #1' }),
    ).toBeInTheDocument()
    expect(within(guidance).getByText('scp-049-run-1')).toBeInTheDocument()

    // URL must remain stable through the refetch — the inventory now lists the
    // new run, but the fallback is bootstrap-only and cannot re-seed.
    expect(await screen.findByRole('button', { name: /scp-049/i })).toBeInTheDocument()
    expect(screen.getByTestId('selected-run-probe')).toHaveTextContent(
      'scp-049-run-1',
    )
  })

  // SCL-5: master scene-list pane must render at every post-Phase-A stage
  // when segments exist, not only at batch_review/waiting.
  describe('SCL-5: scene list visible at every post-Phase-A stage', () => {
    function imageStageRun() {
      return {
        ...run_list_response.data.items[1],
        stage: 'image' as const,
        status: 'running' as const,
      }
    }

    function buildScenesResponse(count: number) {
      return {
        data: {
          items: Array.from({ length: count }, (_, i) => ({
            clip_path: null,
            content_flags: [],
            critic_breakdown: {
              hook_strength: 80 + i,
              fact_accuracy: 75,
              emotional_variation: 60,
              immersion: 70,
            },
            critic_score: 80 + i,
            high_leverage: false,
            narration: `Scene ${i} narration text`,
            regen_attempts: 0,
            retry_exhausted: false,
            review_status: 'waiting_for_review',
            scene_index: i,
            shots: [
              {
                image_path: `/images/scene-${i}.png`,
                duration_s: 4,
                transition: 'cut',
                visual_descriptor: `scene ${i}`,
              },
            ],
          })),
          total: count,
        },
        version: 1,
      }
    }

    function mockFetchForImageStage(scenes_count: number) {
      const run = imageStageRun()
      const status_response = {
        ...run_status_response,
        data: { ...run_status_response.data, run },
      }
      return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = requestUrl(input)
        if (url.endsWith('/api/runs')) {
          return new Response(
            JSON.stringify({
              ...run_list_response,
              data: { ...run_list_response.data, items: [run_list_response.data.items[0], run] },
            }),
            { headers: { 'Content-Type': 'application/json' }, status: 200 },
          )
        }
        if (url.endsWith('/api/runs/scp-049-run-2/status')) {
          return new Response(JSON.stringify(status_response), {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          })
        }
        if (url.endsWith('/api/runs/scp-049-run-2/scenes')) {
          return new Response(JSON.stringify(buildScenesResponse(scenes_count)), {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          })
        }
        throw new Error(`Unexpected fetch in test: ${url}`)
      })
    }

    it('renders the scene list and read-only DetailPanel at image/running with populated scenes', async () => {
      mockFetchForImageStage(3)

      renderWithProviders(
        <KeyboardShortcutsProvider>
          <Routes>
            <Route path="/" element={<AppShell />}>
              <Route path="production" element={<ProductionShell />} />
            </Route>
          </Routes>
        </KeyboardShortcutsProvider>,
        { initialEntries: ['/production?run=scp-049-run-2'] },
      )

      const master = await screen.findByRole('listbox', { name: /scenes/i })
      const cards = within(master).getAllByRole('option')
      expect(cards).toHaveLength(3)
      expect(cards[0]).toHaveAttribute('aria-selected', 'true')
      // Detail pane shows the first scene's read-only DetailPanel.
      expect(await screen.findByLabelText(/scene 1 detail/i)).toBeInTheDocument()
    })

    it('falls back to the stage-in-progress placeholder when no scenes exist yet', async () => {
      mockFetchForImageStage(0)

      renderWithProviders(
        <KeyboardShortcutsProvider>
          <Routes>
            <Route path="/" element={<AppShell />}>
              <Route path="production" element={<ProductionShell />} />
            </Route>
          </Routes>
        </KeyboardShortcutsProvider>,
        { initialEntries: ['/production?run=scp-049-run-2'] },
      )

      expect(await screen.findByLabelText('Stage in progress')).toBeInTheDocument()
      expect(screen.queryByRole('listbox', { name: /scenes/i })).not.toBeInTheDocument()
    })

    it('shows the scene list alongside ScenarioInspector at scenario_review/waiting', async () => {
      const scenarioRun = {
        ...run_list_response.data.items[1],
        stage: 'scenario_review' as const,
        status: 'waiting' as const,
      }
      const status_response = {
        ...run_status_response,
        data: { ...run_status_response.data, run: scenarioRun },
      }
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = requestUrl(input)
        if (url.endsWith('/api/runs')) {
          return new Response(
            JSON.stringify({
              ...run_list_response,
              data: {
                ...run_list_response.data,
                items: [run_list_response.data.items[0], scenarioRun],
              },
            }),
            { headers: { 'Content-Type': 'application/json' }, status: 200 },
          )
        }
        if (url.endsWith('/api/runs/scp-049-run-2/status')) {
          return new Response(JSON.stringify(status_response), {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          })
        }
        if (url.endsWith('/api/runs/scp-049-run-2/scenes')) {
          return new Response(JSON.stringify(buildScenesResponse(2)), {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          })
        }
        if (url.endsWith('/api/runs/scp-049-run-2/scenario/approve')) {
          return new Response(JSON.stringify({ data: { ok: true }, version: 1 }), {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          })
        }
        throw new Error(`Unexpected fetch in test: ${url}`)
      })

      renderWithProviders(
        <KeyboardShortcutsProvider>
          <Routes>
            <Route path="/" element={<AppShell />}>
              <Route path="production" element={<ProductionShell />} />
            </Route>
          </Routes>
        </KeyboardShortcutsProvider>,
        { initialEntries: ['/production?run=scp-049-run-2'] },
      )

      const master = await screen.findByRole('listbox', { name: /scenes/i })
      expect(within(master).getAllByRole('option')).toHaveLength(2)
      // Right pane: ScenarioInspector continues to render unchanged at this stage.
      expect(
        await screen.findByText(/Narration inspector — 2 scenes/i),
      ).toBeInTheDocument()
    })
  })
})
