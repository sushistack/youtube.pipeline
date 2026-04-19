import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

export const UI_STORE_PERSIST_KEY = 'youtube-pipeline-ui'

interface UIState {
  sidebar_collapsed: boolean
  toggle_sidebar: () => void
  set_sidebar_collapsed: (next: boolean) => void
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
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
      partialize: (state) => ({ sidebar_collapsed: state.sidebar_collapsed }),
    },
  ),
)
