import { render, screen } from '@testing-library/react'
import '@testing-library/jest-dom'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { ShortcutHandlerContext } from '../lib/keyboardShortcuts'
import {
  KeyboardShortcutsProvider,
  useKeyboardShortcuts,
} from './useKeyboardShortcuts'

const DIGIT_SHORTCUT_KEYS = [
  'digit-0',
  'digit-1',
  'digit-2',
  'digit-3',
  'digit-4',
  'digit-5',
  'digit-6',
  'digit-7',
  'digit-8',
  'digit-9',
] as const

type ShortcutHandler = (context: ShortcutHandlerContext) => void

function renderWithProvider(children: ReactNode) {
  return render(<KeyboardShortcutsProvider>{children}</KeyboardShortcutsProvider>)
}

function ShortcutHarness({
  enabled = true,
  global_handlers,
  with_context = false,
}: {
  enabled?: boolean
  global_handlers: Record<string, ShortcutHandler>
  with_context?: boolean
}) {
  useKeyboardShortcuts(
    [
      {
        action: 'primary',
        handler: (context) => global_handlers.enter(context),
        key: 'enter',
        prevent_default: true,
      },
      {
        action: 'secondary',
        handler: (context) => global_handlers.escape(context),
        key: 'escape',
        prevent_default: true,
      },
      {
        action: 'edit',
        handler: (context) => global_handlers.tab(context),
        key: 'tab',
      },
      {
        action: 'skip',
        handler: (context) => global_handlers.s(context),
        key: 's',
        prevent_default: true,
      },
      {
        action: 'navigate-next',
        handler: (context) => global_handlers.j(context),
        key: 'j',
        prevent_default: true,
      },
      {
        action: 'navigate-prev',
        handler: (context) => global_handlers.k(context),
        key: 'k',
        prevent_default: true,
      },
      {
        action: 'undo',
        handler: (context) => global_handlers.ctrl_z(context),
        key: 'ctrl+z',
        prevent_default: true,
      },
      {
        action: 'submit',
        handler: (context) => global_handlers.shift_enter(context),
        key: 'shift+enter',
        prevent_default: true,
      },
      ...DIGIT_SHORTCUT_KEYS.map((key, index) => ({
        action: 'select',
        handler: (context: ShortcutHandlerContext) =>
          global_handlers[`digit_${index}`](context),
        key,
      })),
    ],
    { enabled },
  )

  return (
    <div>
      <button type="button">First action</button>
      <button type="button">Second action</button>
      <input aria-label="Command input" />
      <textarea aria-label="Command notes" />
      <div aria-label="Rich notes" contentEditable suppressContentEditableWarning />
      {with_context ? <ContextHarness handler={global_handlers.context_enter} /> : null}
    </div>
  )
}

function ContextHarness({ handler }: { handler: ShortcutHandler }) {
  useKeyboardShortcuts([
    {
      action: 'primary',
      handler: (context: ShortcutHandlerContext) => handler(context),
      key: 'enter',
      prevent_default: true,
      scope: 'context',
    },
  ])

  return <div aria-label="Context owner">Context owner</div>
}

function ToggleHarness({
  global_handlers,
}: {
  global_handlers: Record<string, ShortcutHandler>
}) {
  const [enabled, set_enabled] = useState(true)

  return (
    <>
      <button type="button" onClick={() => set_enabled(false)}>
        Disable shortcuts
      </button>
      <ShortcutHarness enabled={enabled} global_handlers={global_handlers} />
    </>
  )
}

function createHandlers() {
  const make = () => vi.fn<(context: ShortcutHandlerContext) => void>()
  return {
    context_enter: make(),
    ctrl_z: make(),
    digit_0: make(),
    digit_1: make(),
    digit_2: make(),
    digit_3: make(),
    digit_4: make(),
    digit_5: make(),
    digit_6: make(),
    digit_7: make(),
    digit_8: make(),
    digit_9: make(),
    enter: make(),
    escape: make(),
    j: make(),
    k: make(),
    s: make(),
    shift_enter: make(),
    tab: make(),
  }
}

describe('useKeyboardShortcuts', () => {
  it('registers the required combinations and dispatches each handler once', async () => {
    const user = userEvent.setup()
    const handlers = createHandlers()
    renderWithProvider(<ShortcutHarness global_handlers={handlers} />)

    await user.keyboard('{Enter}{Escape}{Tab}s')
    await user.keyboard('j')
    await user.keyboard('k')
    await user.keyboard('{Control>}z{/Control}')
    await user.keyboard('{Shift>}{Enter}{/Shift}')
    await user.keyboard('1234567890')

    expect(handlers.enter).toHaveBeenCalledTimes(1)
    expect(handlers.escape).toHaveBeenCalledTimes(1)
    expect(handlers.tab).toHaveBeenCalledTimes(1)
    expect(handlers.s).toHaveBeenCalledTimes(1)
    expect(handlers.j).toHaveBeenCalledTimes(1)
    expect(handlers.k).toHaveBeenCalledTimes(1)
    expect(handlers.ctrl_z).toHaveBeenCalledTimes(1)
    expect(handlers.shift_enter).toHaveBeenCalledTimes(1)
    expect(handlers.digit_1).toHaveBeenCalledTimes(1)
    expect(handlers.digit_9).toHaveBeenCalledTimes(1)
    expect(handlers.digit_0).toHaveBeenCalledTimes(1)
    expect(handlers.digit_3).toHaveBeenCalledWith(
      expect.objectContaining({ digit: 3, key: 'digit-3' }),
    )
  })

  it('cleans up listeners on disable and unmount', async () => {
    const user = userEvent.setup()
    const handlers = createHandlers()
    const view = renderWithProvider(<ToggleHarness global_handlers={handlers} />)

    await user.keyboard('{Enter}')
    expect(handlers.enter).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: 'Disable shortcuts' }))
    await user.keyboard('{Enter}')
    expect(handlers.enter).toHaveBeenCalledTimes(1)

    view.unmount()
    await user.keyboard('{Enter}')
    expect(handlers.enter).toHaveBeenCalledTimes(1)
  })

  it('does not cross-trigger enter and shift+enter', async () => {
    const user = userEvent.setup()
    const handlers = createHandlers()
    renderWithProvider(<ShortcutHarness global_handlers={handlers} />)

    await user.keyboard('{Enter}')
    expect(handlers.enter).toHaveBeenCalledTimes(1)
    expect(handlers.shift_enter).not.toHaveBeenCalled()

    await user.keyboard('{Shift>}{Enter}{/Shift}')
    expect(handlers.enter).toHaveBeenCalledTimes(1)
    expect(handlers.shift_enter).toHaveBeenCalledTimes(1)
  })

  it('suppresses global shortcuts inside editable controls', async () => {
    const user = userEvent.setup()
    const handlers = createHandlers()
    renderWithProvider(<ShortcutHarness global_handlers={handlers} />)

    const input = screen.getByRole('textbox', { name: 'Command input' })
    await user.click(input)
    expect(input).toHaveFocus()
    await user.keyboard('j')
    expect(input).toHaveValue('j')
    expect(handlers.j).not.toHaveBeenCalled()

    const textarea = screen.getByRole('textbox', { name: 'Command notes' })
    await user.click(textarea)
    expect(textarea).toHaveFocus()
    await user.keyboard('k')
    expect(textarea).toHaveValue('k')
    expect(handlers.k).not.toHaveBeenCalled()

    expect(textarea).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(handlers.enter).not.toHaveBeenCalled()

    const rich_notes = screen.getByLabelText('Rich notes')
    await user.click(rich_notes)
    expect(rich_notes).toHaveFocus()
    await user.keyboard('s')
    expect(handlers.s).not.toHaveBeenCalled()
  })

  it('prefers contextual handlers over global handlers for the same key', async () => {
    const user = userEvent.setup()
    const handlers = createHandlers()
    renderWithProvider(
      <ShortcutHarness global_handlers={handlers} with_context />,
    )

    await user.keyboard('{Enter}')

    expect(handlers.context_enter).toHaveBeenCalledTimes(1)
    expect(handlers.enter).not.toHaveBeenCalled()
  })

  it('keeps ordinary tab focus navigation working while shortcuts are mounted', async () => {
    const user = userEvent.setup()
    const handlers = createHandlers()
    renderWithProvider(<ShortcutHarness global_handlers={handlers} />)

    await user.tab()
    expect(screen.getByRole('button', { name: 'First action' })).toHaveFocus()

    await user.tab()
    expect(screen.getByRole('button', { name: 'Second action' })).toHaveFocus()
    expect(handlers.tab).toHaveBeenCalledTimes(2)
  })
})
