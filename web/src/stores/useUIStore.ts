import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { RunStage, RunStatus } from "../lib/formatters";

export const UI_STORE_PERSIST_KEY = "youtube-pipeline-ui";

export const UNDO_STACK_MAX_DEPTH = 10;

export type UndoFocusTarget = "scene-card" | "descriptor";

export interface UndoCommand {
  command_id: string;
  run_id: string;
  kind:
    | "approve"
    | "reject"
    | "skip"
    | "approve_all_remaining"
    | "descriptor_edit";
  aggregate_command_id?: string;
  scene_index?: number;
  scene_indices?: number[];
  focus_target: UndoFocusTarget;
  created_at: string;
}

export interface ProductionLastSeenSnapshot {
  run_id: string;
  updated_at: string;
  stage: RunStage;
  status: RunStatus;
}

interface UIState {
  onboarding_dismissed: boolean;
  dismiss_onboarding: () => void;
  production_last_seen: Record<string, ProductionLastSeenSnapshot>;
  set_production_last_seen: (snapshot: ProductionLastSeenSnapshot) => void;
  sidebar_collapsed: boolean;
  toggle_sidebar: () => void;
  set_sidebar_collapsed: (next: boolean) => void;
  undo_stacks: Record<string, UndoCommand[]>;
  push_undo_command: (command: UndoCommand) => void;
  pop_undo_command: (run_id: string) => UndoCommand | null;
  clear_undo_stack: (run_id: string) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
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
      undo_stacks: {},
      push_undo_command: (command) =>
        set((state) => {
          const existing = state.undo_stacks[command.run_id] ?? [];
          const updated = [...existing, command].slice(-UNDO_STACK_MAX_DEPTH);
          return {
            undo_stacks: {
              ...state.undo_stacks,
              [command.run_id]: updated,
            },
          };
        }),
      pop_undo_command: (run_id) => {
        const current = get().undo_stacks[run_id] ?? [];
        if (current.length === 0) {
          return null;
        }
        const command = current[current.length - 1];
        set((state) => {
          const latest = state.undo_stacks[run_id] ?? [];
          return {
            undo_stacks: {
              ...state.undo_stacks,
              [run_id]: latest.slice(0, -1),
            },
          };
        });
        return command;
      },
      clear_undo_stack: (run_id) =>
        set((state) => ({
          undo_stacks: {
            ...state.undo_stacks,
            [run_id]: [],
          },
        })),
    }),
    {
      name: UI_STORE_PERSIST_KEY,
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        onboarding_dismissed: state.onboarding_dismissed,
        production_last_seen: state.production_last_seen,
        sidebar_collapsed: state.sidebar_collapsed,
        // undo_stacks is intentionally NOT persisted — stacks are
        // session-scoped and stale across page reloads.
      }),
    },
  ),
);
