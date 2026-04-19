import { ShortcutHintAction, type ShortcutHintActionProps } from './ShortcutHintAction'

interface ActionBarProps {
  actions?: ShortcutHintActionProps[]
}

export function ActionBar({ actions = [] }: ActionBarProps) {
  return (
    <div className="action-bar" role="toolbar" aria-label="Keyboard actions">
      {actions.map((action) => (
        <ShortcutHintAction
          key={`${action.shortcut}-${action.label}`}
          {...action}
        />
      ))}
    </div>
  )
}
