import { screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppShell } from '../shared/AppShell'
import { ProductionShell } from './ProductionShell'
import { renderWithProviders } from '../../test/renderWithProviders'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { useUIStore } from '../../stores/useUIStore'

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
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the dashboard inside the shell and filters run inventory search', async () => {
    const user = userEvent.setup()

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : input.url

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
    expect((await screen.findAllByText('scp-049-run-2')).length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: 'Tuning' })).toBeInTheDocument()
    expect(screen.getByTestId('status-bar')).toHaveAttribute('data-visible', 'true')
    expect(screen.getByRole('button', { name: /scp-049/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /scp-173/i })).toBeInTheDocument()

    await user.type(screen.getByPlaceholderText('Search runs'), '173')

    await waitFor(() => {
      expect(screen.queryByRole('button', { name: /scp-049/i })).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: /scp-173/i })).toBeInTheDocument()
  }, 10_000)

  it('does not render a continuity banner on the first production visit for a run', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : input.url

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
      const url = typeof input === 'string' ? input : input.url

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
      const url = typeof input === 'string' ? input : input.url

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
      const url = typeof input === 'string' ? input : input.url

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
})
