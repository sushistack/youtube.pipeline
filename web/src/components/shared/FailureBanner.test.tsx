import { screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { QueryClient } from '@tanstack/react-query'
import type { ComponentProps } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RunSummary } from '../../contracts/runContracts'
import { KeyboardShortcutsProvider } from '../../hooks/useKeyboardShortcuts'
import { resumeRun } from '../../lib/apiClient'
import { queryKeys } from '../../lib/queryKeys'
import { renderWithProviders } from '../../test/renderWithProviders'
import { FailureBanner } from './FailureBanner'

vi.mock('../../lib/apiClient', async () => {
  const actual = await vi.importActual<typeof import('../../lib/apiClient')>(
    '../../lib/apiClient',
  )

  return {
    ...actual,
    resumeRun: vi.fn(),
  }
})

const mocked_resume_run = vi.mocked(resumeRun)

const failed_run: RunSummary = {
  cost_usd: 3.75,
  created_at: '2026-04-19T00:00:00Z',
  duration_ms: 182000,
  human_override: false,
  id: 'scp-173-run-9',
  retry_count: 1,
  retry_reason: 'rate_limit',
  scp_id: '173',
  stage: 'image',
  status: 'failed',
  token_in: 2200,
  token_out: 610,
  updated_at: '2026-04-19T00:07:00Z',
}

function render_failure_banner(
  run: RunSummary = failed_run,
  props?: Partial<ComponentProps<typeof FailureBanner>>,
) {
  const query_client = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  })

  const invalidate_spy = vi.spyOn(query_client, 'invalidateQueries')
  const on_dismiss = vi.fn()

  const result = renderWithProviders(
    <KeyboardShortcutsProvider>
      <FailureBanner on_dismiss={on_dismiss} run={run} {...props} />
    </KeyboardShortcutsProvider>,
    { queryClient: query_client },
  )

  return {
    ...result,
    invalidate_spy,
    on_dismiss,
    query_client,
  }
}

describe('FailureBanner', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('renders failed-run recovery copy', () => {
    render_failure_banner()

    expect(screen.getByRole('alert')).toHaveClass('failure-banner--retryable')
    expect(screen.getByText(/Pipeline failed/i)).toBeInTheDocument()
    expect(screen.getByText('$3.75')).toBeInTheDocument()
  })

  it('does not render when the run is not failed', () => {
    render_failure_banner({
      ...failed_run,
      status: 'running',
    })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('applies fatal styling when retry_reason is null', () => {
    render_failure_banner({
      ...failed_run,
      retry_reason: null,
    })

    expect(screen.getByRole('alert')).toHaveClass('failure-banner--fatal')
    expect(screen.getByText(/Stage failed/i)).toBeInTheDocument()
  })

  it('resumes on click, disables while pending, and invalidates run queries on success', async () => {
    const user = userEvent.setup()
    let resolve_resume: ((value: RunSummary) => void) | undefined
    mocked_resume_run.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolve_resume = resolve
        }),
    )

    const { invalidate_spy, on_dismiss } = render_failure_banner()
    const resume_button = screen.getByRole('button', { name: /\[enter\]\s*resume/i })

    await user.click(resume_button)

    expect(mocked_resume_run).toHaveBeenCalledWith('scp-173-run-9')
    expect(resume_button).toBeDisabled()

    resolve_resume?.({
      ...failed_run,
      status: 'running',
    })

    await waitFor(() => {
      expect(invalidate_spy).toHaveBeenCalledWith({
        queryKey: queryKeys.runs.list(),
      })
    })
    expect(invalidate_spy).toHaveBeenCalledWith({
      queryKey: queryKeys.runs.status('scp-173-run-9'),
    })
    // onSuccess는 더 이상 on_dismiss를 호출하지 않는다 — query invalidation으로
    // 셸이 status를 다시 받아 banner 조건이 false가 되며 자연스럽게 사라진다.
    // dismissed_run_id에 sticky하게 박지 않는 게 cancel→restart→cancel cycle에서
    // banner가 다시 뜨도록 보장하는 핵심.
    expect(on_dismiss).not.toHaveBeenCalled()
  })

  it('fires resume on Enter when the banner is mounted', async () => {
    const user = userEvent.setup()
    mocked_resume_run.mockResolvedValue({
      ...failed_run,
      status: 'running',
    })

    render_failure_banner()

    await user.keyboard('{Enter}')

    await waitFor(() => {
      expect(mocked_resume_run).toHaveBeenCalledWith('scp-173-run-9')
    })
  })

  it('dismisses on Escape without making an API call', async () => {
    const user = userEvent.setup()
    const { on_dismiss } = render_failure_banner()

    await user.keyboard('{Escape}')

    expect(on_dismiss).toHaveBeenCalledTimes(1)
    expect(mocked_resume_run).not.toHaveBeenCalled()
  })

  it('surfaces the resume error inline and re-enables the button on failure', async () => {
    const user = userEvent.setup()
    mocked_resume_run.mockRejectedValue(new Error('API request failed (500)'))

    render_failure_banner()

    const resume_button = screen.getByRole('button', { name: /\[enter\]\s*resume/i })
    await user.click(resume_button)

    await waitFor(() => {
      expect(
        screen.getByText(/Resume failed: API request failed \(500\)/i),
      ).toBeInTheDocument()
    })
    expect(resume_button).not.toBeDisabled()
  })

  it('dismisses from the close button without making an API call', async () => {
    const user = userEvent.setup()
    const { on_dismiss } = render_failure_banner()

    await user.click(screen.getByRole('button', { name: /dismiss failure banner/i }))

    expect(on_dismiss).toHaveBeenCalledTimes(1)
    expect(mocked_resume_run).not.toHaveBeenCalled()
  })
})
