import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  RunSummary,
  ReviewItem,
  SceneDecisionRequest,
} from "../../contracts/runContracts";
import {
  ApiClientError,
  approveAllRemaining,
  approveBatchReview,
  dispatchSceneRegeneration,
  fetchBatchReviewItems,
  recordSceneDecision,
  undoLastDecision,
} from "../../lib/apiClient";
import { queryKeys } from "../../lib/queryKeys";
import { useUIStore } from "../../stores/useUIStore";
import { DetailPanel } from "../shared/DetailPanel";
import { InlineConfirmPanel } from "../shared/InlineConfirmPanel";
import { RejectComposer } from "../shared/RejectComposer";
import { SceneCard } from "../shared/SceneCard";

// Removed: useKeyboardShortcuts. The previous global shortcut surface
// (Enter / Esc / S / Ctrl+Z / Shift+Enter / J / K) was unreliable in
// practice — operators reach for the on-screen buttons. Inline shortcuts
// inside RejectComposer and InlineConfirmPanel still work because they
// own their own keydown handlers locally.

interface BatchReviewProps {
  run: RunSummary;
}

// Must match service.MaxSceneRegenAttempts (AC-4: 2 retries per scene per run).
const MAX_SCENE_REGEN_ATTEMPTS = 2;

function isActionable(item: ReviewItem) {
  return (
    item.review_status === "waiting_for_review" ||
    item.review_status === "pending"
  );
}

function sortReviewItems(items: ReviewItem[]) {
  // Operator preference: scene order beats high-leverage bucketing. The
  // high-leverage flag is still shown on each card via the badge, so the
  // operator can pick which to focus on without a forced reordering.
  return items.slice().sort((left, right) => left.scene_index - right.scene_index);
}

function getFallbackSelection(items: ReviewItem[]) {
  return items.find(isActionable)?.scene_index ?? null;
}

function buildSkipSnapshot(
  item: ReviewItem,
): NonNullable<SceneDecisionRequest["context_snapshot"]> {
  return {
    action_source: "batch_review",
    content_flags: item.content_flags ?? [],
    critic_score: item.critic_score ?? null,
    critic_sub: item.critic_breakdown ?? null,
    review_status_before: item.review_status,
    scene_index: item.scene_index,
  };
}

function buildRetryExhaustedSkipSnapshot(
  item: ReviewItem,
): NonNullable<SceneDecisionRequest["context_snapshot"]> {
  return {
    ...buildSkipSnapshot(item),
    flagged: true,
    flag_reason: "retry_exhausted",
  };
}

export function BatchReview({ run }: BatchReviewProps) {
  const query_client = useQueryClient();
  const root_ref = useRef<HTMLElement | null>(null);
  const review_items_query = useQuery({
    queryFn: () => fetchBatchReviewItems(run.id),
    queryKey: queryKeys.runs.reviewItems(run.id),
    staleTime: 10_000,
  });
  const items = sortReviewItems(review_items_query.data ?? []);
  const actionable_count = items.filter(isActionable).length;
  const [selected_scene_index, set_selected_scene_index] = useState<
    number | null
  >(null);
  const item_refs = useRef(new Map<number, HTMLLIElement>());
  const effective_selected_scene_index = items.some(
    (item) => item.scene_index === selected_scene_index,
  )
    ? selected_scene_index
    : getFallbackSelection(items);
  const selected_item =
    items.find((item) => item.scene_index === effective_selected_scene_index) ??
    null;

  const [reject_composer_scene, set_reject_composer_scene] = useState<
    number | null
  >(null);
  const [approve_all_open, set_approve_all_open] = useState(false);
  const [regenerating_scenes, set_regenerating_scenes] = useState<Set<number>>(
    () => new Set(),
  );
  const approve_all_trigger_ref = useRef<HTMLButtonElement | null>(null);

  const push_undo_command = useUIStore((s) => s.push_undo_command);
  const pop_undo_command = useUIStore((s) => s.pop_undo_command);
  const raw_undo_stack = useUIStore((s) => s.undo_stacks[run.id]);
  const can_undo = (raw_undo_stack?.length ?? 0) > 0;

  const decision_mutation = useMutation({
    mutationFn: (payload: SceneDecisionRequest) =>
      recordSceneDecision(run.id, payload),
    onSuccess: (saved, variables) => {
      query_client.setQueryData<ReviewItem[] | undefined>(
        queryKeys.runs.reviewItems(run.id),
        (existing) =>
          existing?.map((item) => {
            if (item.scene_index !== saved.scene_index) {
              return item;
            }
            if (saved.decision_type === "approve") {
              return { ...item, review_status: "approved" };
            }
            if (saved.decision_type === "reject") {
              // AC-4: the UI reads retry_exhausted as "has the scene hit the
              // cap?" (am I done?), whereas the server response uses the
              // dispatch-gating semantic (can the client still dispatch the
              // current regen?). Those disagree at attempts == cap, so we
              // recompute here against the cap to keep the cache consistent
              // with /review-items (which also uses >= MaxSceneRegenAttempts).
              return {
                ...item,
                review_status: "rejected",
                regen_attempts: saved.regen_attempts,
                retry_exhausted:
                  saved.regen_attempts >= MAX_SCENE_REGEN_ATTEMPTS,
                prior_rejection: saved.prior_rejection ?? null,
              };
            }
            return item;
          }),
      );

      const kind =
        variables.decision_type === "skip_and_remember"
          ? "skip"
          : variables.decision_type;
      if (kind === "approve" || kind === "reject" || kind === "skip") {
        push_undo_command({
          command_id: `${run.id}-${saved.scene_index}-${saved.decision_type}-${Date.now()}`,
          run_id: run.id,
          kind,
          scene_index: variables.scene_index,
          focus_target: "scene-card",
          created_at: new Date().toISOString(),
        });
      }

      // Only snap selection forward if the user has not manually
      // navigated elsewhere while the mutation was in flight.
      set_selected_scene_index((current) =>
        current === variables.scene_index ? saved.next_scene_index : current,
      );
      root_ref.current?.focus();
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
    },
    onError: () => {
      // Canonical status/review-items stay trustworthy even when the
      // service returned an error after the decision tx committed
      // (e.g., UpsertSessionFromState failed post-commit).
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
    },
  });

  const approve_all_mutation = useMutation({
    mutationFn: (focus_scene_index: number) =>
      approveAllRemaining(run.id, focus_scene_index),
    onSuccess: (saved) => {
      query_client.setQueryData<ReviewItem[] | undefined>(
        queryKeys.runs.reviewItems(run.id),
        (existing) =>
          existing?.map((item) =>
            saved.approved_scene_indices.includes(item.scene_index)
              ? { ...item, review_status: "approved" }
              : item,
          ),
      );
      // Empty aggregate_command_id means the server found no target scenes
      // and no decision rows were written; pushing an undo entry here would
      // create a "phantom" Ctrl+Z that maps to nothing in the DB.
      if (saved.aggregate_command_id !== "" && saved.approved_count > 0) {
        push_undo_command({
          command_id: saved.aggregate_command_id,
          aggregate_command_id: saved.aggregate_command_id,
          run_id: run.id,
          kind: "approve_all_remaining",
          scene_index: saved.focus_scene_index,
          scene_indices: saved.approved_scene_indices,
          focus_target: "scene-card",
          created_at: new Date().toISOString(),
        });
      }
      set_approve_all_open(false);
      root_ref.current?.focus();
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
    },
    onError: () => {
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
    },
  });

  const regen_mutation = useMutation({
    mutationFn: (scene_index: number) =>
      dispatchSceneRegeneration(run.id, scene_index),
    onMutate: (scene_index) => {
      set_regenerating_scenes((prev) => {
        const next = new Set(prev);
        next.add(scene_index);
        return next;
      });
    },
    onSuccess: (_data, scene_index) => {
      set_regenerating_scenes((prev) => {
        const next = new Set(prev);
        next.delete(scene_index);
        return next;
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
    },
    onError: (_error, scene_index) => {
      set_regenerating_scenes((prev) => {
        const next = new Set(prev);
        next.delete(scene_index);
        return next;
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
    },
  });

  const finalize_mutation = useMutation({
    mutationFn: () => approveBatchReview(run.id),
    onSuccess: () => {
      // Status invalidation triggers ProductionShell to swap from BatchReview
      // to the assemble/waiting "Generate Video" gate.
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.list(),
      });
    },
  });

  const undo_mutation = useMutation({
    mutationFn: () => undoLastDecision(run.id),
    onSuccess: (result) => {
      pop_undo_command(run.id);
      // Only restore review_status for approve/reject undo. skip_and_remember
      // never changed review_status, so setting 'waiting_for_review' here
      // would incorrectly overwrite the scene's current cache state.
      if (result.undone_kind === "approve" || result.undone_kind === "reject") {
        query_client.setQueryData<ReviewItem[] | undefined>(
          queryKeys.runs.reviewItems(run.id),
          (existing) =>
            existing?.map((item) =>
              item.scene_index === result.undone_scene_index
                ? { ...item, review_status: "waiting_for_review" }
                : item,
            ),
        );
      }
      if (
        result.focus_target === "scene-card" &&
        result.undone_scene_index >= 0
      ) {
        set_selected_scene_index(result.undone_scene_index);
      }
      root_ref.current?.focus();
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
    },
    onError: (error) => {
      // A 409 means the server refused the undo because server-side state has
      // moved past the undoable window (e.g., /regen already flipped the
      // segment back to waiting_for_review). The command at the top of the
      // client's undo stack is now un-appliable — pop it so Ctrl+Z doesn't
      // re-fail forever on the same stale command. Other errors (500,
      // network) may be transient; keep the stack intact so retry is possible.
      if (error instanceof ApiClientError && error.status === 409) {
        pop_undo_command(run.id);
      }
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.reviewItems(run.id),
      });
      void query_client.invalidateQueries({
        queryKey: queryKeys.runs.status(run.id),
      });
    },
  });

  useEffect(() => {
    if (effective_selected_scene_index == null) {
      return;
    }
    const selected_node = item_refs.current.get(effective_selected_scene_index);
    if (!selected_node || typeof selected_node.scrollIntoView !== "function") {
      return;
    }
    selected_node.scrollIntoView({
      block: "nearest",
      behavior: "auto",
    });
  }, [effective_selected_scene_index]);

  function closeRejectComposer() {
    set_reject_composer_scene(null);
  }

  function closeApproveAllPanel() {
    set_approve_all_open(false);
    // Prefer the invoking trigger, but fall back to the batch-review root when
    // the trigger is unmounted (retry-exhausted surface) or disabled (zero
    // actionable scenes after a successful confirm). Keeps keyboard focus
    // inside the review context instead of dropping it to <body>.
    const trigger = approve_all_trigger_ref.current;
    if (trigger && !trigger.disabled && trigger.isConnected) {
      trigger.focus();
      return;
    }
    root_ref.current?.focus();
  }

  function openRejectComposer() {
    if (!selected_item) {
      return;
    }
    if (!isActionable(selected_item)) {
      return;
    }
    if (selected_item.retry_exhausted) {
      return;
    }
    set_reject_composer_scene(selected_item.scene_index);
  }

  async function submitRejectWithReason(reason: string) {
    if (!selected_item || decision_mutation.isPending) {
      return;
    }
    try {
      const saved = await decision_mutation.mutateAsync({
        scene_index: selected_item.scene_index,
        decision_type: "reject",
        note: reason,
      });
      closeRejectComposer();
      if (!saved.retry_exhausted) {
        regen_mutation.mutate(selected_item.scene_index);
      }
    } catch {
      // Mutation error is already surfaced via decision_mutation.isError.
      // Keep composer open so operator can retry without retyping.
    }
  }

  function approveSelectedScene() {
    if (!selected_item || decision_mutation.isPending) {
      return;
    }
    decision_mutation.mutate({
      scene_index: selected_item.scene_index,
      decision_type: "approve",
    });
  }

  function rejectSelectedScene() {
    // Story 8.4 AC-1: Reject opens the inline composer instead of firing an
    // immediate reject. Exhausted scenes route to the dedicated CTA surface
    // rendered below the action bar.
    openRejectComposer();
  }

  function skipAndFlagExhausted() {
    if (!selected_item || decision_mutation.isPending) {
      return;
    }
    decision_mutation.mutate({
      scene_index: selected_item.scene_index,
      decision_type: "skip_and_remember",
      context_snapshot: buildRetryExhaustedSkipSnapshot(selected_item),
      note: "retry exhausted — flagged for manual follow-up",
    });
  }

  function undoAction() {
    if (
      !can_undo ||
      undo_mutation.isPending ||
      decision_mutation.isPending ||
      approve_all_mutation.isPending
    ) {
      return;
    }
    undo_mutation.mutate();
  }

  function openApproveAllPanel() {
    if (
      actionable_count === 0 ||
      decision_mutation.isPending ||
      approve_all_mutation.isPending
    ) {
      return;
    }
    set_approve_all_open(true);
  }

  function confirmApproveAll() {
    if (approve_all_mutation.isPending) {
      return;
    }
    approve_all_mutation.mutate(selected_item?.scene_index ?? 0);
  }

  const composer_open_pre =
    reject_composer_scene != null &&
    selected_item?.scene_index === reject_composer_scene;

  if (review_items_query.isPending) {
    return (
      <div className="batch-review__loading" aria-busy="true">
        Loading batch review…
      </div>
    );
  }

  if (review_items_query.isError) {
    return (
      <div className="batch-review__error" role="alert">
        Failed to load batch review items. Try refreshing.
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="batch-review__empty">
        No review items are available for this run.
      </div>
    );
  }

  const is_selected_regenerating = selected_item
    ? regenerating_scenes.has(selected_item.scene_index)
    : false;
  const composer_open = composer_open_pre;
  const is_exhausted = selected_item?.retry_exhausted ?? false;

  return (
    <section
      ref={root_ref}
      className="batch-review"
      aria-label="Batch review layout"
      tabIndex={-1}
    >
      <aside className="batch-review__list-pane">
        <header className="batch-review__header">
          <p className="production-dashboard__eyebrow">Batch review</p>
          <h2 className="production-dashboard__section-title">
            {actionable_count === 0
              ? "All scenes reviewed"
              : `${actionable_count} scenes remaining`}
          </h2>
        </header>

        <ol
          className="batch-review__list"
          role="listbox"
          aria-label="Review scene queue"
        >
          {items.map((item) => (
            <li
              key={item.scene_index}
              ref={(node) => {
                if (node) {
                  item_refs.current.set(item.scene_index, node);
                } else {
                  item_refs.current.delete(item.scene_index);
                }
              }}
            >
              <SceneCard
                item={item}
                is_regenerating={regenerating_scenes.has(item.scene_index)}
                selected={item.scene_index === selected_item?.scene_index}
                on_select={() => set_selected_scene_index(item.scene_index)}
              />
            </li>
          ))}
        </ol>
      </aside>

      <div className="batch-review__detail-pane">
        {actionable_count === 0 ? (
          <div
            className="batch-review__finalize"
            role="status"
            aria-live="polite"
          >
            <p className="batch-review__finalize-title">All scenes reviewed</p>
            <div
              className="batch-review__actions"
              aria-label="Finalize batch review"
            >
              <button
                type="button"
                className="batch-review__action-button batch-review__action-button--primary"
                disabled={finalize_mutation.isPending}
                onClick={() => finalize_mutation.mutate()}
              >
                {finalize_mutation.isPending
                  ? "Finalizing…"
                  : "Continue to Assemble"}
              </button>
              <button
                type="button"
                className="batch-review__action-button"
                disabled={
                  !can_undo ||
                  undo_mutation.isPending ||
                  finalize_mutation.isPending
                }
                onClick={undoAction}
              >
                Undo
              </button>
            </div>
            {finalize_mutation.isError ? (
              <p className="batch-review__finalize-error" role="alert">
                Finalize failed:{" "}
                {finalize_mutation.error instanceof Error
                  ? finalize_mutation.error.message
                  : "Unknown error — try again."}
              </p>
            ) : null}
          </div>
        ) : null}

        {selected_item ? (
          <>
            <DetailPanel
              key={selected_item.scene_index}
              item={selected_item}
              is_regenerating={is_selected_regenerating}
            />

            {composer_open ? (
              <RejectComposer
                scene_index={selected_item.scene_index}
                prior_rejection={selected_item.prior_rejection ?? null}
                is_submitting={decision_mutation.isPending}
                on_submit={submitRejectWithReason}
                on_cancel={() => {
                  closeRejectComposer();
                  root_ref.current?.focus();
                }}
              />
            ) : null}

            {!composer_open && is_exhausted && isActionable(selected_item) ? (
              <div
                className="batch-review__exhausted"
                role="status"
                aria-live="polite"
              >
                <p className="batch-review__exhausted-title">
                  Retry limit reached
                </p>
                <p className="batch-review__exhausted-body">
                  This scene has been regenerated {selected_item.regen_attempts}{" "}
                  times. Use manual edit or skip &amp; flag.
                </p>
                <div
                  className="batch-review__actions"
                  aria-label="Retry-exhausted actions"
                >
                  <button
                    type="button"
                    className="batch-review__action-button"
                    disabled
                    title="Manual narration edits happen in Scenario Review."
                  >
                    Manual edit
                  </button>
                  <button
                    type="button"
                    className="batch-review__action-button"
                    disabled={decision_mutation.isPending}
                    onClick={skipAndFlagExhausted}
                  >
                    Skip &amp; flag
                  </button>
                </div>
              </div>
            ) : null}

            {!composer_open && approve_all_open ? (
              <InlineConfirmPanel
                confirm_label="This will approve"
                count={actionable_count}
                is_confirming={approve_all_mutation.isPending}
                on_cancel={closeApproveAllPanel}
                on_confirm={confirmApproveAll}
              />
            ) : null}

            {!composer_open && !is_exhausted && actionable_count > 0 ? (
              <div
                className="batch-review__actions"
                aria-label="Review actions"
              >
                <button
                  type="button"
                  className="batch-review__action-button"
                  disabled={
                    decision_mutation.isPending ||
                    is_selected_regenerating ||
                    approve_all_open
                  }
                  onClick={approveSelectedScene}
                >
                  Approve
                </button>
                <button
                  type="button"
                  className="batch-review__action-button"
                  disabled={
                    decision_mutation.isPending ||
                    is_selected_regenerating ||
                    approve_all_open
                  }
                  onClick={rejectSelectedScene}
                >
                  Reject
                </button>
                <button
                  ref={approve_all_trigger_ref}
                  type="button"
                  className="batch-review__action-button"
                  disabled={
                    decision_mutation.isPending ||
                    approve_all_mutation.isPending ||
                    is_selected_regenerating
                  }
                  onClick={openApproveAllPanel}
                >
                  Approve All Remaining
                </button>
                <button
                  type="button"
                  className="batch-review__action-button"
                  disabled={
                    !can_undo ||
                    undo_mutation.isPending ||
                    decision_mutation.isPending ||
                    approve_all_mutation.isPending
                  }
                  onClick={undoAction}
                >
                  Undo
                </button>
              </div>
            ) : null}

          </>
        ) : actionable_count === 0 ? null : (
          <div className="batch-review__empty">
            Select a scene to review.
          </div>
        )}
      </div>
    </section>
  );
}
