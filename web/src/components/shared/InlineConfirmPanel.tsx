import { useEffect, useId, useRef, type KeyboardEvent } from "react";

interface InlineConfirmPanelProps {
  confirm_label: string;
  count: number;
  is_confirming?: boolean;
  on_cancel: () => void;
  on_confirm: () => void;
}

function focusableElements(root: HTMLElement | null) {
  if (!root) {
    return [];
  }
  return Array.from(
    root.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  );
}

export function InlineConfirmPanel({
  confirm_label,
  count,
  is_confirming = false,
  on_cancel,
  on_confirm,
}: InlineConfirmPanelProps) {
  const panel_ref = useRef<HTMLDivElement | null>(null);
  const cancel_button_ref = useRef<HTMLButtonElement | null>(null);
  const instance_id = useId();
  const title_id = `inline-confirm-title-${instance_id}`;
  const copy_id = `inline-confirm-copy-${instance_id}`;

  useEffect(() => {
    const first = focusableElements(panel_ref.current)[0];
    if (first) {
      first.focus();
      return;
    }
    // Fallback for the case where both buttons are disabled (e.g., the panel
    // mounts already in `is_confirming`): keep focus inside the panel root so
    // the trap still has a reference point.
    panel_ref.current?.focus();
  }, []);

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      if (is_confirming) {
        // Mutation is in flight; Esc must not race the in-flight request.
        return;
      }
      on_cancel();
      return;
    }

    if (event.key === "Enter") {
      event.preventDefault();
      if (is_confirming) {
        // Block double-submit when a confirmation request is already pending.
        return;
      }
      // Route through the focused button's native click so keyboard activation
      // matches mouse activation: Enter on [Esc] Cancel cancels, Enter on
      // [Enter] Confirm confirms. Falling back to on_confirm keeps the "press
      // Enter anywhere inside the panel" affordance for the initial mount
      // (primary action is the confirm button).
      const active = document.activeElement;
      if (active instanceof HTMLButtonElement && panel_ref.current?.contains(active)) {
        active.click();
        return;
      }
      on_confirm();
      return;
    }

    if (event.key !== "Tab") {
      return;
    }

    const targets = focusableElements(panel_ref.current);
    if (targets.length === 0) {
      // No focusable descendants (both buttons disabled during confirm).
      // Pin focus to the panel root so Tab does not leak to the underlying
      // review surface.
      event.preventDefault();
      panel_ref.current?.focus();
      return;
    }
    const active = document.activeElement as HTMLElement | null;
    const currentIndex = active ? targets.indexOf(active) : -1;
    const nextIndex = event.shiftKey
      ? currentIndex <= 0
        ? targets.length - 1
        : currentIndex - 1
      : currentIndex === -1 || currentIndex === targets.length - 1
        ? 0
        : currentIndex + 1;
    event.preventDefault();
    targets[nextIndex]?.focus();
  }

  return (
    <div
      ref={panel_ref}
      aria-describedby={copy_id}
      aria-labelledby={title_id}
      aria-modal="true"
      className="inline-confirm-panel"
      onKeyDown={handleKeyDown}
      role="alertdialog"
      tabIndex={-1}
    >
      <p className="inline-confirm-panel__eyebrow">Batch review</p>
      <h3 className="inline-confirm-panel__title" id={title_id}>
        Approve all remaining scenes?
      </h3>
      <p className="inline-confirm-panel__copy" id={copy_id}>
        {confirm_label} {count} remaining {count === 1 ? "scene" : "scenes"} in
        this run.
      </p>
      <div className="inline-confirm-panel__actions">
        <button
          type="button"
          className="batch-review__action-button"
          disabled={is_confirming}
          onClick={on_confirm}
        >
          [Enter] Confirm
        </button>
        <button
          ref={cancel_button_ref}
          type="button"
          className="batch-review__action-button"
          disabled={is_confirming}
          onClick={on_cancel}
        >
          [Esc] Cancel
        </button>
      </div>
    </div>
  );
}
