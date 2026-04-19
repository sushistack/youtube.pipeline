import type { ButtonHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export interface ShortcutHintActionProps
  extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string
  shortcut: string
}

export function ShortcutHintAction({
  className,
  label,
  shortcut,
  type = 'button',
  ...props
}: ShortcutHintActionProps) {
  return (
    <button
      {...props}
      type={type}
      className={cn('shortcut-hint-action', className)}
    >
      <span className="shortcut-hint-action__key" data-testid="shortcut-hint-key">
        [{shortcut}]
      </span>
      <span className="shortcut-hint-action__label">{label}</span>
    </button>
  )
}
