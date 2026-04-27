import '@testing-library/jest-dom'
import { afterEach } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { renderWithProviders } from '../../test/renderWithProviders'
import { SettingsShell } from './SettingsShell'

function installFetchMock(overrides?: {
  budget_status?: 'safe' | 'near_cap' | 'exceeded'
  put_error?: Record<string, string>
}) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url
    if (url.endsWith('/api/settings') && (!init?.method || init.method === 'GET')) {
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            config: {
              writer_model: 'deepseek-chat',
              critic_model: 'gemini-3.1-flash-lite-preview',
              image_model: 'qwen-max-vl',
              tts_model: 'qwen3-tts',
              tts_voice: 'longhua',
              tts_audio_format: 'wav',
              writer_provider: 'deepseek',
              critic_provider: 'gemini',
              image_provider: 'dashscope',
              tts_provider: 'dashscope',
              dashscope_region: 'cn-beijing',
              cost_cap_research: 0.5,
              cost_cap_write: 0.5,
              cost_cap_image: 2,
              cost_cap_tts: 1,
              cost_cap_assemble: 0.1,
              cost_cap_per_run: 5,
              dry_run: false,
            },
            env: {
              DASHSCOPE_API_KEY: { configured: true },
              DEEPSEEK_API_KEY: { configured: false },
              GEMINI_API_KEY: { configured: true },
            },
            budget: {
              source: {
                kind: 'active_run',
                label: 'Active run spend',
                run_id: 'scp-049-run-2',
              },
              current_spend_usd:
                overrides?.budget_status === 'exceeded'
                  ? 5.4
                  : overrides?.budget_status === 'near_cap'
                    ? 4.2
                    : 2.1,
              soft_cap_usd: 4,
              hard_cap_usd: 5,
              progress_ratio:
                overrides?.budget_status === 'exceeded'
                  ? 1.08
                  : overrides?.budget_status === 'near_cap'
                    ? 0.84
                    : 0.42,
              status: overrides?.budget_status ?? 'safe',
            },
            application: {
              effective_version: 1,
            },
          },
        }),
        {
          headers: { 'Content-Type': 'application/json', ETag: '"1"' },
          status: 200,
        },
      )
    }

    if (url.endsWith('/api/settings') && init?.method === 'PUT') {
      if (overrides?.put_error) {
        return new Response(
          JSON.stringify({
            version: 1,
            error: {
              code: 'VALIDATION_ERROR',
              message: 'validation failed',
              details: overrides.put_error,
            },
          }),
          { headers: { 'Content-Type': 'application/json' }, status: 400 },
        )
      }

      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            config: {
              writer_model: 'deepseek-chat-v2',
              critic_model: 'gemini-3.1-flash-lite-preview',
              image_model: 'qwen-max-vl',
              tts_model: 'qwen3-tts',
              tts_voice: 'longhua',
              tts_audio_format: 'wav',
              writer_provider: 'deepseek',
              critic_provider: 'gemini',
              image_provider: 'dashscope',
              tts_provider: 'dashscope',
              dashscope_region: 'cn-beijing',
              cost_cap_research: 0.5,
              cost_cap_write: 0.5,
              cost_cap_image: 2,
              cost_cap_tts: 1,
              cost_cap_assemble: 0.1,
              cost_cap_per_run: 5,
              dry_run: false,
            },
            env: {
              DASHSCOPE_API_KEY: { configured: true },
              DEEPSEEK_API_KEY: { configured: false },
              GEMINI_API_KEY: { configured: true },
            },
            budget: {
              source: { kind: 'active_run', label: 'Active run spend', run_id: 'scp-049-run-2' },
              current_spend_usd: 2.1,
              soft_cap_usd: 4,
              hard_cap_usd: 5,
              progress_ratio: 0.42,
              status: 'safe',
            },
            application: {
              effective_version: 3,
            },
          },
        }),
        { headers: { 'Content-Type': 'application/json' }, status: 200 },
      )
    }

    if (url.includes('/api/decisions')) {
      return new Response(
        JSON.stringify({
          version: 1,
          data: {
            items: [
              {
                id: 1,
                run_id: 'run-1',
                scp_id: '049',
                scene_id: '0',
                decision_type: 'approve',
                note: 'approved',
                reason_from_snapshot: null,
                superseded_by: null,
                created_at: '2026-04-19T01:00:00Z',
              },
            ],
            next_cursor: null,
          },
        }),
        { headers: { 'Content-Type': 'application/json' }, status: 200 },
      )
    }

    return new Response('not found', { status: 404 })
  })
}

describe('SettingsShell', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the settings workspace and timeline together', async () => {
    installFetchMock()

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <SettingsShell />
      </KeyboardShortcutsProvider>,
      { initialEntries: ['/settings'] },
    )

    expect(
      await screen.findByRole('heading', { name: 'Settings' }),
    ).toBeInTheDocument()
    expect(
      await screen.findByRole('heading', { name: 'Models and cost guardrails' }),
    ).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Timeline' })).toBeInTheDocument()
  })

  it('shows inline client validation before submit', async () => {
    installFetchMock()
    const user = userEvent.setup()

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <SettingsShell />
      </KeyboardShortcutsProvider>,
      { initialEntries: ['/settings'] },
    )

    const writer_provider = await screen.findByDisplayValue('deepseek')
    await user.clear(writer_provider)
    await user.click(screen.getByRole('button', { name: 'Save settings' }))

    expect(await screen.findByText('Required')).toBeInTheDocument()
    expect(screen.getByText(/fix the highlighted fields/i)).toBeInTheDocument()
  })

  it('renders exceeded budget state', async () => {
    installFetchMock({ budget_status: 'exceeded' })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <SettingsShell />
      </KeyboardShortcutsProvider>,
      { initialEntries: ['/settings'] },
    )

    expect(await screen.findByText('Exceeded')).toBeInTheDocument()
    expect(screen.getByText(/108% of hard cap used/i)).toBeInTheDocument()
  })

  it('renders corruption recovery UI when config.yaml is unreadable', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url
      if (url.endsWith('/api/settings')) {
        return new Response(
          JSON.stringify({
            version: 1,
            error: {
              code: 'SETTINGS_CORRUPTED',
              message: 'config.yaml is unreadable',
            },
          }),
          { headers: { 'Content-Type': 'application/json' }, status: 422 },
        )
      }
      return new Response('not found', { status: 404 })
    })

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <SettingsShell />
      </KeyboardShortcutsProvider>,
      { initialEntries: ['/settings'] },
    )

    expect(
      await screen.findByText(/config\.yaml is unreadable/i),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Reset to defaults' }),
    ).toBeInTheDocument()
  })

  it('keeps the form open and shows server field errors inline', async () => {
    installFetchMock({
      put_error: {
        'env.GEMINI_API_KEY': 'API key cannot be blank when explicitly cleared',
      },
    })
    const user = userEvent.setup()

    renderWithProviders(
      <KeyboardShortcutsProvider>
        <SettingsShell />
      </KeyboardShortcutsProvider>,
      { initialEntries: ['/settings'] },
    )

    await user.type(
      await screen.findByLabelText('Gemini API key'),
      '   ',
    )
    await user.click(screen.getByRole('button', { name: 'Save settings' }))

    expect(
      await screen.findByText(/api key cannot be blank when explicitly cleared/i),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeInTheDocument()
  })
})
