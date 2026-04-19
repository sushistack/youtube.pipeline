import {
  type PropsWithChildren,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  NewRunCoordinatorContext,
  type NewRunCoordinatorValue,
} from './newRunCoordinatorContext'

export function NewRunCoordinatorProvider({
  children,
}: PropsWithChildren) {
  const [is_open, set_is_open] = useState(false)
  const restore_focus_ref = useRef<HTMLElement | null>(null)

  const value = useMemo<NewRunCoordinatorValue>(
    () => ({
      close_new_run_panel() {
        set_is_open(false)
      },
      is_open,
      open_new_run_panel(options) {
        if (is_open) return
        restore_focus_ref.current = options?.restore_focus_to ?? null
        set_is_open(true)
      },
      restore_focus() {
        restore_focus_ref.current?.focus()
      },
    }),
    [is_open],
  )

  return (
    <NewRunCoordinatorContext.Provider value={value}>
      {children}
    </NewRunCoordinatorContext.Provider>
  )
}
