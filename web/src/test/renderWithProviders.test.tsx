import { screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import { Route, Routes } from 'react-router'
import { describe, expect, it } from 'vitest'
import { ProductionShell } from '../components/shells/ProductionShell'
import { AppShell } from '../components/shared/AppShell'
import { KeyboardShortcutsProvider } from '../hooks/useKeyboardShortcuts'
import { renderWithProviders } from './renderWithProviders'

describe('renderWithProviders', () => {
  it('renders a routed shell through MemoryRouter and a fresh QueryClient', async () => {
    const firstRender = renderWithProviders(
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
    expect(firstRender.queryClient.getQueryCache().getAll()).toHaveLength(0)

    firstRender.unmount()

    const secondRender = renderWithProviders(<div>fresh client</div>)
    expect(secondRender.queryClient).not.toBe(firstRender.queryClient)
  })
})

