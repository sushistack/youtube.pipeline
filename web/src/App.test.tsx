import { act, cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { useUIStore } from './stores/useUIStore'

type MediaListener = (event: MediaQueryListEvent) => void

const viewport_state = {
  width: 1440,
  listeners: new Set<MediaListener>(),
}

function computeMatches(width: number) {
  return width < 1280
}

function installMatchMedia(initial_width: number) {
  viewport_state.width = initial_width
  viewport_state.listeners.clear()

  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: initial_width,
    writable: true,
  })

  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      get matches() {
        return query === '(width < 1280px)' ? computeMatches(viewport_state.width) : false
      },
      media: query,
      onchange: null,
      addEventListener: (_event: string, listener: MediaListener) => {
        viewport_state.listeners.add(listener)
      },
      removeEventListener: (_event: string, listener: MediaListener) => {
        viewport_state.listeners.delete(listener)
      },
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
}

function setViewportWidth(width: number) {
  const prev_matches = computeMatches(viewport_state.width)
  viewport_state.width = width
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
    writable: true,
  })
  const next_matches = computeMatches(width)
  if (prev_matches !== next_matches) {
    viewport_state.listeners.forEach((listener) =>
      listener({ matches: next_matches, media: '(width < 1280px)' } as MediaQueryListEvent),
    )
  }
}

describe('App', () => {
  beforeEach(() => {
    localStorage.clear()
    useUIStore.setState({ sidebar_collapsed: false })
    window.history.pushState({}, '', '/')
    installMatchMedia(1440)
  })

  afterEach(() => {
    cleanup()
  })

  it('renders the shared shell on the default route and redirects to production', async () => {
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-sidebar', 'shell')
    expect(window.location.pathname).toBe('/production')
  })

  it('renders route-specific shell content for each workflow route', async () => {
    window.history.pushState({}, '', '/tuning')
    render(<App />)
    expect(await screen.findByRole('heading', { name: 'Tuning' })).toBeInTheDocument()

    cleanup()
    window.history.pushState({}, '', '/settings')
    render(<App />)
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument()
  })

  it('redirects unknown routes to /production', async () => {
    window.history.pushState({}, '', '/does-not-exist')
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    expect(window.location.pathname).toBe('/production')
  })

  it('navigates client-side and marks the active nav item accessibly', async () => {
    const user = userEvent.setup()
    render(<App />)

    const shell = await screen.findByTestId('app-shell')
    const tuning_link = screen.getByRole('link', { name: 'Tuning' })
    expect(screen.getByRole('link', { name: 'Production' })).toHaveAttribute(
      'aria-current',
      'page',
    )

    await user.click(tuning_link)

    expect(await screen.findByRole('heading', { name: 'Tuning' })).toBeInTheDocument()
    expect(window.location.pathname).toBe('/tuning')
    expect(tuning_link).toHaveAttribute('aria-current', 'page')
    expect(shell).toBeInTheDocument()
  })

  it('updates the shell collapsed contract when the operator toggles the sidebar', async () => {
    const user = userEvent.setup()
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-collapsed', 'false')

    const toggle = screen.getByRole('button', { name: 'Collapse sidebar' })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')

    await user.click(toggle)

    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-collapsed', 'true')
    const expand = screen.getByRole('button', { name: 'Expand sidebar' })
    expect(expand).toHaveAttribute('aria-expanded', 'false')
  })

  it('forces the collapsed presentation on narrow viewports', async () => {
    installMatchMedia(1100)
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Production' })).toBeInTheDocument()
    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-forced-collapsed', 'true')
    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-collapsed', 'true')
    expect(screen.getByRole('link', { name: 'Production' })).toHaveAttribute(
      'title',
      'Production',
    )
  })

  it('keeps the desktop preference visible at exactly 1280px (boundary)', async () => {
    installMatchMedia(1280)
    render(<App />)

    expect(await screen.findByTestId('app-shell')).toHaveAttribute(
      'data-forced-collapsed',
      'false',
    )
    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-collapsed', 'false')
  })

  it('forces the collapsed presentation at exactly 1279px (boundary)', async () => {
    installMatchMedia(1279)
    render(<App />)

    expect(await screen.findByTestId('app-shell')).toHaveAttribute(
      'data-forced-collapsed',
      'true',
    )
    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-collapsed', 'true')
  })

  it('reacts live to matchMedia change events after mount', async () => {
    render(<App />)

    const shell = await screen.findByTestId('app-shell')
    expect(shell).toHaveAttribute('data-forced-collapsed', 'false')

    act(() => {
      setViewportWidth(1100)
    })

    expect(shell).toHaveAttribute('data-forced-collapsed', 'true')

    act(() => {
      setViewportWidth(1440)
    })

    expect(shell).toHaveAttribute('data-forced-collapsed', 'false')
  })

  it('disables the toggle on narrow viewports so the persisted desktop preference survives', async () => {
    const user = userEvent.setup()
    installMatchMedia(1100)
    render(<App />)

    expect(await screen.findByTestId('app-shell')).toHaveAttribute('data-collapsed', 'true')
    const forced_button = screen.getByRole('button', {
      name: 'Viewport is forcing the collapsed shell',
    })
    expect(forced_button).toBeDisabled()

    await user.click(forced_button).catch(() => undefined)

    const persisted = localStorage.getItem('youtube-pipeline-ui')
    expect(persisted === null || JSON.parse(persisted).state.sidebar_collapsed === false).toBe(
      true,
    )

    act(() => {
      setViewportWidth(1440)
    })

    expect(screen.getByTestId('app-shell')).toHaveAttribute('data-collapsed', 'false')
  })
})
