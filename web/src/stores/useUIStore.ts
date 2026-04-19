import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import type { RunStage, RunStatus } from '../lib/formatters'

export const UI_STORE_PERSIST_KEY = 'youtube-pipeline-ui'

export interface ProductionLastSeenSnapshot {
  run_id: string
  updated_at: string
  stage: RunStage
  status: RunStatus
}

interface UIState {
  onboarding_dismissed: boolean
  dismiss_onboarding: () => void
  production_last_seen: Record<string, ProductionLastSeenSnapshot>
  set_production_last_seen: (snapshot: ProductionLastSeenSnapshot) => void
  sidebar_collapsed: boolean
  toggle_sidebar: () => void
  set_sidebar_collapsed: (next: boolean) => void
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      onboarding_dismissed: false,
      dismiss_onboarding: () =>
        set({
          onboarding_dismissed: true,
        }),
      production_last_seen: {},
      set_production_last_seen: (snapshot) =>
        set((state) => ({
          production_last_seen: {
            ...state.production_last_seen,
            [snapshot.run_id]: snapshot,
          },
        })),
      sidebar_collapsed: false,
      toggle_sidebar: () =>
        set((state) => ({
          sidebar_collapsed: !state.sidebar_collapsed,
        })),
      set_sidebar_collapsed: (next) =>
        set({
          sidebar_collapsed: next,
        }),
    }),
    {
      name: UI_STORE_PERSIST_KEY,
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        onboarding_dismissed: state.onboarding_dismissed,
        production_last_seen: state.production_last_seen,
        sidebar_collapsed: state.sidebar_collapsed,
      }),
    },
  ),
)
