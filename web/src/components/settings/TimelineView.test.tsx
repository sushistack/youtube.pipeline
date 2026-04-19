import '@testing-library/jest-dom'
import { act, fireEvent, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { renderWithProviders } from '../../test/renderWithProviders'
import { TimelineView } from './TimelineView'

const timeline_payload = {
  version: 1,
  data: {
    items: [
      {
        id: 3,
        run_id: 'run-2',
        scp_id: '173',
        scene_id: null,
        decision_type: 'undo',
        note: 'undo of decision 2',
        reason_from_snapshot: null,
        superseded_by: null,
        created_at: '2026-04-19T01:02:03Z',
      },
      {
        id: 2,
        run_id: 'run-1',
        scp_id: '049',
        scene_id: '4',
        decision_type: 'reject',
        note: null,
        reason_from_snapshot: 'Needs a clearer hook beat',
        superseded_by: 3,
        created_at: '2026-04-19T01:01:03Z',
      },
      {
        id: 1,
        run_id: 'run-1',
        scp_id: '049',
        scene_id: '3',
        decision_type: 'approve',
        note: 'Operator approved after spot-check',
        reason_from_snapshot: null,
        superseded_by: null,
        created_at: '2026-04-19T01:00:03Z',
      },
    ],
    next_cursor: null,
  },
}

function renderTimeline() {
  return renderWithProviders(
    <KeyboardShortcutsProvider>
      <TimelineView />
    </KeyboardShortcutsProvider>,
  )
}

async function findTimelineListbox() {
  return screen.findByRole('listbox', { name: /decisions history timeline/i })
}

async function findSelectedTimelineRow() {
  const listbox = await findTimelineListbox()
  return within(listbox).getByRole('option', { selected: true })
}

describe('TimelineView', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
      configurable: true,
      value: vi.fn(),
      writable: true,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders required row fields and marks superseded rows as undone', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(timeline_payload), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderTimeline()

    expect(await findTimelineListbox()).toBeInTheDocument()
    expect(screen.getByText('173')).toBeInTheDocument()
    expect(await findSelectedTimelineRow()).toHaveTextContent('undo')
    expect(screen.getByText('Scene 4')).toBeInTheDocument()
    expect(screen.getByText('Needs a clearer hook beat')).toBeInTheDocument()
    expect(screen.getByText('Undone')).toBeInTheDocument()
    expect(await findSelectedTimelineRow()).toHaveAttribute('aria-selected', 'true')
  })

  it('applies reason search only after exactly 100ms and matches case-insensitively', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(timeline_payload), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderTimeline()
    expect(await screen.findByText('Operator approved after spot-check')).toBeInTheDocument()
    vi.useFakeTimers()

    const search = screen.getByRole('searchbox', { name: /reason search/i })
    fireEvent.change(search, { target: { value: 'CLEARER' } })

    act(() => {
      vi.advanceTimersByTime(99)
    })
    expect(screen.getByText('Operator approved after spot-check')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(1)
    })
    await act(async () => {
      await Promise.resolve()
    })
    expect(screen.queryByText('Operator approved after spot-check')).not.toBeInTheDocument()
    expect(screen.getByText('Needs a clearer hook beat')).toBeInTheDocument()
  })

  it('filters by decision type and clear restores the full page without extra fetches', async () => {
    const user = userEvent.setup()
    const fetch_mock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(timeline_payload), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderTimeline()
    expect(await screen.findByText('Operator approved after spot-check')).toBeInTheDocument()

    await user.selectOptions(
      screen.getByRole('combobox', { name: /decision type filter/i }),
      'reject',
    )

    expect(screen.getByText('Needs a clearer hook beat')).toBeInTheDocument()
    expect(screen.queryByText('Operator approved after spot-check')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /clear filters/i }))

    expect(await screen.findByText('Operator approved after spot-check')).toBeInTheDocument()
    expect(fetch_mock).toHaveBeenCalledTimes(1)
  })

  it('navigates with J/K inside the filtered list, stays bounded, and resets selection when filters exclude it', async () => {
    const user = userEvent.setup()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(timeline_payload), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderTimeline()
    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'undo of decision 2',
    )

    await user.keyboard('j')
    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'Needs a clearer hook beat',
    )

    await user.keyboard('j')
    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'Operator approved after spot-check',
    )

    await user.keyboard('j')
    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'Operator approved after spot-check',
    )

    await user.selectOptions(
      screen.getByRole('combobox', { name: /decision type filter/i }),
      'reject',
    )
    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'Needs a clearer hook beat',
    )

    await user.keyboard('k')
    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'Needs a clearer hook beat',
    )
  })

  it('suppresses shortcuts while the reason search input is focused', async () => {
    const user = userEvent.setup()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(timeline_payload), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderTimeline()
    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'undo of decision 2',
    )

    const search = screen.getByRole('searchbox', { name: /reason search/i })
    await user.click(search)
    await user.keyboard('j')

    expect(await findSelectedTimelineRow()).toHaveTextContent(
      'undo of decision 2',
    )
  })

  it('renders loading, empty, and recoverable error states', async () => {
    let resolve_fetch: ((value: Response) => void) | null = null
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      () =>
        new Promise((resolve) => {
          resolve_fetch = resolve
        }),
    )

    renderTimeline()
    expect(screen.getByText(/loading decisions history/i)).toHaveAttribute(
      'aria-busy',
      'true',
    )

    act(() => {
      resolve_fetch?.(
        new Response(
          JSON.stringify({
            version: 1,
            data: { items: [], next_cursor: null },
          }),
          {
            headers: { 'Content-Type': 'application/json' },
            status: 200,
          },
        ),
      )
    })

    expect(await screen.findByText(/no decisions yet/i)).toBeInTheDocument()

    let attempts = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
      attempts += 1
      if (attempts === 1) {
        return new Response(
          JSON.stringify({
            version: 1,
            error: { code: 'INTERNAL', message: 'boom' },
          }),
          {
            headers: { 'Content-Type': 'application/json' },
            status: 500,
          },
        )
      }
      return new Response(JSON.stringify(timeline_payload), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      })
    })

    renderTimeline()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      /failed to load decisions history/i,
    )

    await userEvent.setup().click(screen.getByRole('button', { name: /retry/i }))
    expect(await screen.findByText('Operator approved after spot-check')).toBeInTheDocument()
  })

  it('does not register an extra raw window keydown listener', async () => {
    const add_event_listener = vi.spyOn(window, 'addEventListener')
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(timeline_payload), {
        headers: { 'Content-Type': 'application/json' },
        status: 200,
      }),
    )

    renderTimeline()
    await screen.findByText('Operator approved after spot-check')

    const keydown_calls = add_event_listener.mock.calls.filter(
      (call) => call[0] === 'keydown',
    )
    expect(keydown_calls).toHaveLength(1)
  })
})
