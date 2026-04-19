import { createContext } from 'react'

export interface OpenNewRunOptions {
  restore_focus_to?: HTMLElement | null
}

export interface NewRunCoordinatorValue {
  is_open: boolean
  open_new_run_panel: (options?: OpenNewRunOptions) => void
  close_new_run_panel: () => void
  restore_focus: () => void
}

export const NewRunCoordinatorContext =
  createContext<NewRunCoordinatorValue | null>(null)
