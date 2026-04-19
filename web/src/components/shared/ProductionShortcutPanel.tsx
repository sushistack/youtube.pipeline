import { useState } from 'react'
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { ActionBar } from './ActionBar'

const ACTION_BAR_ACTIONS = [
  { label: 'Approve', shortcut: 'Enter' },
  { label: 'Reject', shortcut: 'Esc' },
  { label: 'Edit', shortcut: 'Tab' },
  { label: 'Skip', shortcut: 'S' },
  { label: 'Undo', shortcut: 'Ctrl+Z' },
] as const

const SHOT_LABELS = ['Shot 1', 'Shot 2', 'Shot 3'] as const
const SHOT_SHORTCUT_KEYS = ['digit-1', 'digit-2', 'digit-3'] as const

export function ProductionShortcutPanel() {
  const [last_action, set_last_action] = useState('Waiting for keyboard command')
  const [selected_index, set_selected_index] = useState(0)

  useKeyboardShortcuts([
    {
      action: 'primary',
      handler: () => {
        set_last_action(`Approved ${SHOT_LABELS[selected_index]}`)
      },
      key: 'enter',
      prevent_default: true,
    },
    {
      action: 'secondary',
      handler: () => {
        set_last_action(`Rejected ${SHOT_LABELS[selected_index]}`)
      },
      key: 'escape',
      prevent_default: true,
    },
    {
      action: 'edit',
      handler: () => {
        set_last_action('Focus moved to the edit surface')
      },
      key: 'tab',
    },
    {
      action: 'skip',
      handler: () => {
        set_last_action(`Skipped ${SHOT_LABELS[selected_index]}`)
      },
      key: 's',
      prevent_default: true,
    },
    {
      action: 'undo',
      handler: () => {
        set_last_action('Undo requested')
      },
      key: 'ctrl+z',
      prevent_default: true,
    },
    {
      action: 'navigate-next',
      handler: () => {
        set_selected_index((current) => (current + 1) % SHOT_LABELS.length)
      },
      key: 'j',
      prevent_default: true,
    },
    {
      action: 'navigate-prev',
      handler: () => {
        set_selected_index((current) =>
          current === 0 ? SHOT_LABELS.length - 1 : current - 1,
        )
      },
      key: 'k',
      prevent_default: true,
    },
    ...SHOT_LABELS.map((label, index) => ({
      action: 'select',
      handler: () => {
        set_selected_index(index)
        set_last_action(`Selected ${label}`)
      },
      key: SHOT_SHORTCUT_KEYS[index],
      scope: 'context' as const,
    })),
  ])

  return (
    <section
      className="shortcut-demo"
      aria-labelledby="production-shortcut-panel-title"
    >
      <div className="shortcut-demo__header">
        <p className="route-shell__eyebrow">Operator shortcuts</p>
        <h2
          id="production-shortcut-panel-title"
          className="shortcut-demo__title"
        >
          Keyboard-first command rail
        </h2>
        <p className="shortcut-demo__body">
          Global actions stay visible, while scoped digit selection can swap the
          active shot without reloading the route shell.
        </p>
      </div>

      <ActionBar actions={ACTION_BAR_ACTIONS.map((action) => ({ ...action }))} />

      <div className="shortcut-demo__selection" aria-label="Shot selection">
        {SHOT_LABELS.map((label, index) => (
          <button
            key={label}
            type="button"
            className="shortcut-demo__chip"
            data-active={String(index === selected_index)}
            onClick={() => {
              set_selected_index(index)
              set_last_action(`Selected ${label}`)
            }}
          >
            <span className="shortcut-demo__chip-key">[{index + 1}]</span>
            <span>{label}</span>
          </button>
        ))}
      </div>

      <p className="shortcut-demo__status" aria-live="polite">
        {last_action}
      </p>
    </section>
  )
}
